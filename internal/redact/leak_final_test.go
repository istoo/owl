package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// 第四份集合：规则全部写完后才写、才跑，结果只报告，不再拿来调规则。
// 写法与前三份都不重合；值都是随机生成的虚构值。

func finalCases() []leakCase {
	g := leakGen{rand.New(rand.NewPCG(31337, 2026))}
	var cs []leakCase
	add := func(kind, shape, text string, vals ...string) {
		cs = append(cs, leakCase{kind, shape, text, vals})
	}
	for i := 0; i < 12; i++ {
		// ---- 私钥 ----
		k := g.pem(g.pick("RSA PRIVATE KEY", "OPENSSH PRIVATE KEY"), 5+g.r.IntN(8), false)
		add("private_key", "markdown", "我把私钥贴在这：\n```\n"+k+"\n```", strings.Split(k, "\n")[1])
		k = g.pem("PRIVATE KEY", 4, false)
		add("private_key", "python-str", "PRIVATE_KEY = \"\"\""+k+"\"\"\"", strings.Split(k, "\n")[2])
		k = g.pem("ENCRYPTED PRIVATE KEY", 4+g.r.IntN(4), true)
		add("private_key", "log-trunc", "2026-11-19 10:00:01 DEBUG loaded key\n"+k, strings.Split(k, "\n")[1])

		// ---- 云 AK ----
		akid := "AK" + "IA" + g.s(lkUpper+lkDigits, 16)
		sec := g.s(lkAlnum+"+/", 40)
		add("cloud", "aws-cli-config", "aws configure set aws_access_key_id "+akid+"\naws configure set aws_secret_access_key "+sec, akid, sec)
		add("cloud", "aws-json", `{"AccessKeyId": "`+akid+`", "SecretAccessKey": "`+sec+`", "Expiration": "2026-11-19T12:00:00Z"}`, akid, sec)
		ali := "LT" + "AI" + g.mixed(18)
		aliSec := g.mixed(30)
		add("cloud", "aliyun-cli", "aliyun configure set --profile prod --mode AK --access-key-id "+ali+" --access-key-secret "+aliSec+" --region cn-shanghai", ali, aliSec)
		add("cloud", "aliyun-go", "client, _ := oss.New(\"oss-cn-beijing.aliyuncs.com\", \""+ali+"\", \""+aliSec+"\")", ali, aliSec)
		tc := "AK" + "ID" + g.mixed(32)
		tcSec := g.mixed(32)
		add("cloud", "tencent-cos", "secret_id = '"+tc+"'\nsecret_key = '"+tcSec+"'", tc, tcSec)
		gcpKey := "AI" + "za" + "Sy" + g.s(lkB64url, 33)
		add("cloud", "gcp-zh", "谷歌地图的 key 是 "+gcpKey, gcpKey)
		volc := "AK" + "LT" + g.mixed(40)
		volcSec := g.s(lkB64, 60)
		add("cloud", "volc-env", "VOLC_ACCESSKEY="+volc+"\nVOLC_SECRETKEY="+volcSec, volc, volcSec)

		// ---- 数据库管理员 ----
		pw := userinfoPW(g.password())
		add("db_admin", "prisma", "DATABASE_URL=\"postgresql://postgres:"+pw+"@"+g.host()+":5432/prod?schema=public\"", pw)
		pw = g.password()
		add("db_admin", "django-db", "DATABASES = {'default': {'ENGINE': 'django.db.backends.mysql', 'USER': 'root', 'PASSWORD': '"+pw+"', 'HOST': '"+g.host()+"'}}", pw)
		pw = g.password()
		add("db_admin", "spring-yaml", "spring:\n  datasource:\n    username: root\n    password: "+pw, pw)
		pw = g.password()
		add("db_admin", "mongo-env", "MONGO_INITDB_ROOT_USERNAME=admin\nMONGO_INITDB_ROOT_PASSWORD="+pw, pw)
		pw = g.password()
		add("db_admin", "psql-cli", "PGPASSWORD='"+pw+"' psql -h "+g.host()+" -U postgres -c 'select 1'", pw)
		pw = g.password()
		add("db_admin", "zh-sa", "SQL Server 的 sa 密码："+pw+"，端口 1433", pw)
		pw = g.password()
		add("db_admin", "redis-conf", "# redis.conf\nrequirepass "+pw, pw)

		// ---- 口令 ----
		pw = g.password()
		add("password", "toml", "[smtp]\nusername = \"bot\"\npassword = \""+pw+"\"", pw)
		pw = g.password()
		add("password", "ansible", "ansible_ssh_pass: "+pw+"\nansible_user: root", pw)
		pw = g.password()
		add("password", "wifi", "WiFi 名称 office-5G 密码 "+pw+"", pw)
		pw = g.password()
		add("password", "rclone", "rclone config create mydav webdav url https://dav.example.com user me pass "+pw, pw)
		pw = g.password()
		add("password", "curl-u", "curl -u admin:"+pw+" https://jenkins.example.com/api/json", pw)
		pw = g.password()
		add("password", "java-props", "jdbc.password="+pw+"\njdbc.username=app", pw)
		pw = g.password()
		add("password", "en-chat", "Login with admin / "+pw+" and tell me what you see", pw)
		pw = g.password()
		add("password", "zh-admin", "后台地址 https://"+g.host()+"/admin 账号 admin 密码 "+pw+"", pw)

		// ---- Token ----
		for _, t := range []struct{ shape, v string }{
			{"anthropic", "sk-" + "ant-" + "api03-" + g.s(lkB64url, 93) + "AA"},
			{"deepseek", "sk-" + g.s(lkHexa, 32)},
			{"gitlab", "gl" + "pat-" + g.s(lkB64url, 20)},
			{"hex40", g.s(lkHexa, 40)},
			{"random48", g.mixed(48)},
		} {
			switch g.r.IntN(6) {
			case 0:
				add("token", t.shape+"/claude-env", "ANTHROPIC_AUTH_TOKEN="+t.v, t.v)
			case 1:
				add("token", t.shape+"/yaml", "llm:\n  api_key: "+t.v+"\n  base_url: https://api.example.com", t.v)
			case 2:
				add("token", t.shape+"/ts", "const openai = new OpenAI({ apiKey: '"+t.v+"' });", t.v)
			case 3:
				add("token", t.shape+"/zh", "我的 key："+t.v+"，帮我看看为什么报 401", t.v)
			case 4:
				add("token", t.shape+"/curl-x", "curl -H 'x-api-key: "+t.v+"' https://api.example.com/v1/messages", t.v)
			default:
				add("token", t.shape+"/settings-json", `{"env": {"ANTHROPIC_API_KEY": "`+t.v+`"}}`, t.v)
			}
		}

		// ---- 其他 Secret ----
		v := g.mixed(32)
		add("secret", "github-oauth", "GITHUB_CLIENT_SECRET="+v, v)
		v = g.s(lkB64, 64)
		add("secret", "jwt-env", "JWT_SECRET_KEY="+v, v)
		v = g.mixed(40)
		add("secret", "dingtalk-sec", "钉钉机器人加签 secret："+"SEC"+v, "SEC"+v)
		v = g.mixed(32)
		add("secret", "wx-mp", "WECHAT_APPSECRET="+v, v)
		v = g.s(lkHexa, 64)
		add("secret", "cookie-secret", "COOKIE_SECRET="+v, v)

		// ---- 不该遮的正常内容 ----
		add("keep", "go-test", "--- PASS: TestPasswordReset (0.02s)\n--- FAIL: TestTokenRefresh (0.10s)", "TestPasswordReset", "TestTokenRefresh")
		add("keep", "help", "  --password string   Password for the registry\n  --token string      Access token to use", "Password for the registry", "Access token to use")
		add("keep", "i18n", "export default { password: \"Password\", token: \"Token\", secret: \"Secret\" }", "\"Password\"", "\"Token\"")
		add("keep", "jsdoc", "/** @param {string} password - the user's password */", "@param {string}")
		add("keep", "env-ref", "DB_PASSWORD=${DB_PASSWORD:-}\nAPI_KEY=$(cat /run/secrets/api_key)", "${DB_PASSWORD:-}", "$(cat /run/secrets/api_key)")
		add("keep", "k8s-ref", "- name: PASSWORD\n  valueFrom:\n    secretKeyRef: {name: db, key: password}", "secretKeyRef")
		add("keep", "hash-pin", "go.sum: github.com/x/y v1.2.3 h1:"+g.s(lkB64, 43)+"=", "github.com/x/y v1.2.3")
		add("keep", "git-log", "commit "+g.s(lkHexa, 40)+"\n    fix: rotate token on 401", "fix: rotate token on 401")
		add("keep", "zh-howto", "怎么在 .env 里配置 API_KEY？密码需要加引号吗？", "怎么在 .env 里配置 API_KEY")
		add("keep", "url-noauth", "git clone https://github.com/istoo/owl.git && cd owl", "https://github.com/istoo/owl.git")
	}
	return cs
}

func TestLeakFinal(t *testing.T) {
	runLeakEval(t, finalCases())
}
