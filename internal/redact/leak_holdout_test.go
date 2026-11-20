package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// 留出集：写法与 leak_eval_test.go 不重合，调规则时没有看过它的结果。
// 种子不同，值同样是随机生成的虚构值。

func holdoutCases() []leakCase {
	g := leakGen{rand.New(rand.NewPCG(7, 20261119))}
	var cs []leakCase
	add := func(kind, shape, text string, vals ...string) {
		cs = append(cs, leakCase{kind, shape, text, vals})
	}
	for i := 0; i < 12; i++ {
		// ---- 私钥 ----
		k := g.pem(g.pick("RSA PRIVATE KEY", "PRIVATE KEY", "OPENSSH PRIVATE KEY"), 6+g.r.IntN(10), false)
		add("private_key", "k8s-secret", "apiVersion: v1\nkind: Secret\nstringData:\n  tls.key: |\n    "+strings.ReplaceAll(k, "\n", "\n    "), strings.Split(k, "\n")[1])
		k = g.pem("PRIVATE KEY", 6, false)
		add("private_key", "go-const", "const key = `"+k+"`", k)
		k = g.pem("RSA PRIVATE KEY", 3+g.r.IntN(4), true)
		add("private_key", "trunc-in-error", "Error: invalid key format\n"+k+"\n...", strings.Split(k, "\n")[1])

		// ---- 云 AK ----
		akid := "AK" + "IA" + g.s(lkUpper+lkDigits, 16)
		sec := g.s(lkAlnum+"+/", 40)
		add("cloud", "terraform", "provider \"aws\" {\n  region     = \"us-west-2\"\n  access_key = \""+akid+"\"\n  secret_key = \""+sec+"\"\n}", akid, sec)
		add("cloud", "boto3", "s3 = boto3.client('s3', aws_access_key_id='"+akid+"', aws_secret_access_key='"+sec+"')", akid, sec)
		ali := "LT" + "AI" + g.mixed(16)
		aliSec := g.mixed(30)
		add("cloud", "aliyun-zh", "AccessKeyId: "+ali+"\nAccessKeySecret: "+aliSec, ali, aliSec)
		tc := "AK" + "ID" + g.mixed(32)
		tcSec := g.mixed(32)
		add("cloud", "tencent-code", "cred = credential.Credential(\""+tc+"\", \""+tcSec+"\")", tc, tcSec)
		add("cloud", "tencent-zh", "SecretId 是 "+tc+"，SecretKey 是 "+tcSec, tc, tcSec)
		qn := g.s(lkAlnum+"-_", 40)
		add("cloud", "qiniu", "QINIU_ACCESS_KEY="+qn, qn)

		// ---- 数据库管理员 ----
		pw := g.password()
		add("db_admin", "psql-dsn", "psql \"host="+g.host()+" port=5432 user=postgres password="+pw+" dbname=app\"", pw)
		pw = g.password()
		add("db_admin", "pgpassword", "PGPASSWORD="+pw+" pg_dump -h "+g.host()+" -U postgres app > app.sql", pw)
		pw = g.password()
		add("db_admin", "kubectl", "kubectl create secret generic db --from-literal=username=root --from-literal=password="+pw, pw)
		pw = g.password()
		add("db_admin", "mysqldump", "mysqldump -uroot -p'"+pw+"' --all-databases > all.sql", pw)
		pw = userinfoPW(g.password())
		add("db_admin", "sqlalchemy", "engine = create_engine(\"mysql+pymysql://root:"+pw+"@"+g.host()+"/app\")", pw)
		pw = g.password()
		add("db_admin", "zh-root", "root 密码 "+pw+" ，库在 "+g.host(), pw)

		// ---- 口令 ----
		pw = g.password()
		add("password", "netrc", "machine api.example.com\n  login deploy\n  password "+pw, pw)
		pw = g.password()
		add("password", "powershell", "$env:ADMIN_PASSWORD = \""+pw+"\"", pw)
		pw = g.password()
		add("password", "compose-list", "    environment:\n      - ADMIN_PASSWORD="+pw, pw)
		pw = g.password()
		add("password", "en-prose", "the password is "+pw+" for the staging box", pw)
		pw = g.password()
		add("password", "zh-pwd", "pwd是"+pw, pw)
		pw = g.password()
		add("password", "zh-kouling", "口令为 "+pw+"。", pw)
		pw = g.password()
		add("password", "js-object", "const cfg = { user: 'admin', password: '"+pw+"' };", pw)
		pw = g.password()
		add("password", "go-struct", "cfg := Config{User: \"admin\", Password: \""+pw+"\"}", pw)

		// ---- Token ----
		for _, t := range []struct{ shape, v string }{
			{"gh", "gh" + "o_" + g.mixed(36)},
			{"openai", "sk-" + "proj-" + g.s(lkB64url, 80)},
			{"hex32", g.s(lkHexa, 32)},
			{"random40", g.mixed(40)},
			{"jwt", "ey" + "J" + g.s(lkB64url, 30) + ".ey" + "J" + g.s(lkB64url, 60) + "." + g.s(lkB64url, 43)},
		} {
			switch g.r.IntN(6) {
			case 0:
				add("token", t.shape+"/npmrc", "//registry.npmjs.org/:_authToken="+t.v, t.v)
			case 1:
				add("token", t.shape+"/fetch", "fetch(url, { headers: { Authorization: `Bearer "+t.v+"` } })", t.v)
			case 2:
				add("token", t.shape+"/requests", "requests.get(url, headers={'Authorization': 'token "+t.v+"'})", t.v)
			case 3:
				add("token", t.shape+"/en-prose", "use this api key: "+t.v, t.v)
			case 4:
				add("token", t.shape+"/gh-actions", "env:\n  GH_TOKEN: "+t.v, t.v)
			default:
				add("token", t.shape+"/zh-lingpai", "令牌："+t.v, t.v)
			}
		}

		// ---- 其他 Secret ----
		v := g.mixed(32)
		add("secret", "nextauth", "NEXTAUTH_SECRET="+v, v)
		v = g.s(lkB64, 44)
		add("secret", "rails", "secret_key_base: "+v, v)
		v = g.mixed(40)
		add("secret", "zh-appsecret", "AppSecret："+v, v)
		v = g.s(lkHexa, 64)
		add("secret", "signing", "signingKey := []byte(\""+v+"\")", v)

		// ---- 不该遮的正常内容 ----
		add("keep", "mysql-prompt", "mysql -u root -p -h "+g.host()+" app", "app")
		add("keep", "token-var", "curl -H \"Authorization: Bearer $GITHUB_TOKEN\" https://api.github.com", "$GITHUB_TOKEN")
		add("keep", "bearer-prose", "Use Bearer authentication with an access token from the login endpoint.", "Bearer authentication")
		add("keep", "sshpass-var", "sshpass -p \"$SSHPASS\" ssh host", "$SSHPASS")
		add("keep", "token-file", "gh auth login --with-token < ~/.config/gh/token.txt\n./run --token-file ./token.txt", "--token-file")
		add("keep", "pgpass-doc", "Each line of .pgpass has the format hostname:port:database:username:password", "hostname:port:database:username")
		add("keep", "compile", "main.go:12:3: undefined: Config\npkg/db.go:40:9: missing return", "main.go:12:3")
		add("keep", "pass-python", "for x in xs:\n    pass\npass_rate = 0.95", "pass_rate")
		add("keep", "xml-var", "<password>${DB_PASSWORD}</password>", "${DB_PASSWORD}")
		add("keep", "ak-question", "AK 是什么意思？SK 又是什么", "AK 是什么意思")
		add("keep", "pwd-dir", "PWD=/home/ubuntu/project\nOLDPWD=/home/ubuntu", "/home/ubuntu/project")
		add("keep", "sha-commit", "commit "+g.s(lkHexa, 40)+"\nAuthor: dev", "commit ")
		add("keep", "uuid-row", "SELECT * FROM orders WHERE id = '"+g.s(lkHexa, 8)+"-"+g.s(lkHexa, 4)+"-4"+g.s(lkHexa, 3)+"-a"+g.s(lkHexa, 3)+"-"+g.s(lkHexa, 12)+"';", "SELECT * FROM orders WHERE id = '")
		add("keep", "flags", "./server --port 8080 --password-file /run/secrets/pw --verbose", "/run/secrets/pw")
	}
	return cs
}

func TestLeakHoldout(t *testing.T) {
	runLeakEval(t, holdoutCases())
}
