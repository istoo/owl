package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// 第三份集合：写法与前两份都不重合，规则写完后才第一次跑，结果不再拿来
// 调规则。值都是随机生成的虚构值。

func freshCases() []leakCase {
	g := leakGen{rand.New(rand.NewPCG(424242, 99))}
	var cs []leakCase
	add := func(kind, shape, text string, vals ...string) {
		cs = append(cs, leakCase{kind, shape, text, vals})
	}
	for i := 0; i < 12; i++ {
		// ---- 私钥 ----
		k := g.pem("PRIVATE KEY", 8, false)
		add("private_key", "tool-json", `{"type":"tool_result","content":"`+strings.ReplaceAll(k, "\n", `\n`)+`"}`, strings.Split(k, "\n")[2])
		k = g.pem("EC PRIVATE KEY", 3, false)
		add("private_key", "crlf", strings.ReplaceAll(k, "\n", "\r\n"), strings.Split(k, "\n")[1])
		k = g.pem("PRIVATE KEY", 6, false)
		add("private_key", "firebase-env", `FIREBASE_PRIVATE_KEY="`+strings.ReplaceAll(k, "\n", `\n`)+`"`, strings.Split(k, "\n")[3])
		k = g.pem("PGP PRIVATE KEY BLOCK", 10, true)
		add("private_key", "pgp-trunc", "gpg --export-secret-keys 的输出：\n"+k, strings.Split(k, "\n")[4])

		// ---- 云 AK ----
		akid := "AS" + "IA" + g.s(lkUpper+lkDigits, 16)
		sec := g.s(lkAlnum+"+/", 40)
		tok := g.s(lkB64, 120)
		add("cloud", "aws-session", "[prod]\naws_access_key_id="+akid+"\naws_secret_access_key="+sec+"\naws_session_token="+tok, akid, sec, tok)
		ali := "LT" + "AI" + g.mixed(20)
		aliSec := g.mixed(30)
		add("cloud", "aliyun-java", "DefaultProfile profile = DefaultProfile.getProfile(\"cn-hangzhou\", \""+ali+"\", \""+aliSec+"\");", ali, aliSec)
		hwAK := g.s(lkUpper+lkDigits, 20)
		hwSK := g.mixed(40)
		add("cloud", "huawei-env", "HUAWEICLOUD_SDK_AK="+hwAK+"\nHUAWEICLOUD_SDK_SK="+hwSK, hwAK, hwSK)
		sig := g.s(lkB64, 43) + "="
		add("cloud", "azure-sas", "https://acct.blob.core.windows.net/c/f.zip?sv=2022-11-02&ss=b&srt=o&sp=r&se=2026-12-01&sig="+userinfoPW(sig), userinfoPW(sig))
		mk, ms := g.mixed(20), g.mixed(40)
		add("cloud", "minio-mc", "mc alias set myminio http://"+g.host()+":9000 "+mk+" "+ms, mk, ms)
		pw := g.password()
		add("cloud", "minio-env", "MINIO_ROOT_USER=admin\nMINIO_ROOT_PASSWORD="+pw, pw)

		// ---- 数据库管理员 ----
		pw = g.password()
		add("db_admin", "redis-cli", "redis-cli -h "+g.host()+" -p 6379 -a "+pw+" info", pw)
		pw = g.password()
		add("db_admin", "mongo-cli", "mongosh --host "+g.host()+" --username admin --password "+pw+" --authenticationDatabase admin", pw)
		pw = g.password()
		add("db_admin", "my-cnf", "[client]\nuser=root\npassword="+pw, pw)
		pw = g.password()
		add("db_admin", "mssql-conn", "\"ConnectionStrings\": { \"Default\": \"Server="+g.host()+";Database=app;User Id=sa;Password="+pw+";\" }", pw)
		pw = g.password()
		add("db_admin", "zh-slash", "数据库账号密码 root/"+pw, pw)
		pw = g.password()
		add("db_admin", "influx", "influx -host "+g.host()+" -username admin -password "+pw, pw)
		pw = userinfoPW(g.password())
		add("db_admin", "yaml-url", "datasource:\n  url: \"mysql://root:"+pw+"@"+g.host()+":3306/app\"", pw)

		// ---- 口令 ----
		pw = g.password()
		add("password", "sudo-S", "echo '"+pw+"' | sudo -S systemctl restart nginx", pw)
		pw = g.password()
		add("password", "chpasswd", "echo \"deploy:"+pw+"\" | chpasswd", pw)
		pw = g.password()
		add("password", "htpasswd", "htpasswd -b -c /etc/nginx/.htpasswd admin "+pw, pw)
		pw = g.password()
		add("password", "json-pass", `{"user": "admin", "pass": "`+pw+`"}`, pw)
		pw = g.password()
		add("password", "yaml-admin", "grafana:\n  admin_password: "+pw, pw)
		pw = g.password()
		add("password", "zh-login", "登录密码："+pw, pw)
		pw = g.password()
		add("password", "zh-nosep", "账号admin 密码"+pw+" 你登录看看", pw)
		pw = g.password()
		add("password", "expect", "expect \"password:\"\nsend \""+pw+"\\r\"", pw)
		pw = g.password()
		add("password", "docker-login", "docker login -u admin -p "+pw+" registry.example.com", pw)

		// ---- Token ----
		ghs := "gh" + "s_" + g.mixed(36)
		add("token", "git-remote", "git remote set-url origin https://x-access-token:"+ghs+"@github.com/org/repo.git", ghs)
		hf := "h" + "f_" + g.mixed(34)
		add("token", "hf-env", "HF_TOKEN="+hf, hf)
		oa := "sk-" + "proj-" + g.s(lkB64url, 100)
		add("token", "openai-env", "OPENAI_API_KEY="+oa, oa)
		ck := g.s(lkB64url, 48)
		add("token", "cookie", "Cookie: session="+ck+"; theme=dark", ck)
		xt := g.mixed(40)
		add("token", "x-auth", "X-Auth-Token: "+xt, xt)
		gk := "AI" + "za" + "Sy" + g.s(lkB64url, 33)
		add("token", "maps-query", "https://maps.googleapis.com/maps/api/geocode/json?address=x&key="+gk, gk)
		sh := g.mixed(24)
		add("token", "slack-hook", "https://hooks.slack.com/services/T"+g.s(lkUpper+lkDigits, 8)+"/B"+g.s(lkUpper+lkDigits, 10)+"/"+sh, sh)
		fs := g.s(lkHexa, 8) + "-" + g.s(lkHexa, 4) + "-4" + g.s(lkHexa, 3) + "-8" + g.s(lkHexa, 3) + "-" + g.s(lkHexa, 12)
		add("token", "feishu-hook", "飞书机器人 webhook：https://open.feishu.cn/open-apis/bot/v2/hook/"+fs, fs)
		dt := g.s(lkHexa, 64)
		add("token", "dingtalk", "https://oapi.dingtalk.com/robot/send?access_token="+dt, dt)
		dp := g.mixed(30)
		add("token", "docker-pat", "echo dckr_pat_"+dp+" | docker login -u me --password-stdin", dp)

		// ---- 其他 Secret ----
		v := g.mixed(32)
		add("secret", "yaml-client", "oauth:\n  client_id: app-"+g.s(lkDigits, 6)+"\n  client_secret: "+v, v)
		v = g.mixed(32)
		add("secret", "json-app", `{"app_id": "cli_`+g.s(lkHexa, 16)+`", "app_secret": "`+v+`"}`, v)
		v = "base64:" + g.s(lkB64, 43) + "="
		add("secret", "laravel", "APP_KEY="+v, v[7:])
		v = g.mixed(32)
		add("secret", "wxpay", "wechat:\n  mch_id: 1600000000\n  api_v3_key: "+v, v)
		v = g.mixed(32)
		add("secret", "encrypt", "ENCRYPT_KEY="+v, v)

		// ---- 不该遮的正常内容 ----
		add("keep", "docker-port", "docker run -p 8080:80 -p 443:443 nginx", "8080:80")
		add("keep", "ssh-port", "ssh -p 2222 deploy@"+g.host(), "2222")
		add("keep", "pw-stdin", "cat pw.txt | docker login -u me --password-stdin", "--password-stdin")
		add("keep", "secret-ref", "valueFrom:\n  secretKeyRef:\n    name: db\n    key: password", "secretKeyRef")
		add("keep", "token-count", "prompt_tokens: 1234\ncompletion_tokens: 567\ntotal_tokens: 1801", "1801")
		add("keep", "css", ".btn { color: #3b82f6; background: #0f172a; }", "#3b82f6")
		add("keep", "image", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ", "iVBORw0KGgo")
		add("keep", "pass-test", "--- PASS: TestLogin (0.01s)\nPASS\nok  \tgithub.com/acme/app\t0.42s", "TestLogin")
		add("keep", "zh-discuss", "密码要至少 12 位，包含大小写和数字；token 有效期 7 天。", "12 位")
		add("keep", "authorization-doc", "Authorization header is required for every request.", "Authorization header is required")
		add("keep", "en-is", "the token is expired and the password is incorrect", "expired", "incorrect")
		add("keep", "redis-cli-plain", "redis-cli -h localhost -p 6379 ping", "ping")
	}
	return cs
}

func TestLeakFresh(t *testing.T) {
	runLeakEval(t, freshCases())
}
