package redact

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// 这份评测按 AI 写代码聊天里泄露凭据的六类组织：私钥、云 AK、数据库管理
// 员凭据、口令、Token、其他 Secret。所有值都是用固定种子随机生成的虚构值，
// 只保留厂商的形状和聊天里常见的写法（dotenv、YAML、JSON、命令行、代码、
// 连接串、中文对话）；没有一个来自真实泄露。
//
// 成功只算“完整遮蔽”：值的任何 6 个字符以上的片段都不能留在发出的文本里，
// 且换回后逐字节等于原文。另有一组不该遮的正常内容，单独报误遮。

type leakCase struct {
	kind  string   // 六类之一，或 "keep"
	shape string   // 写法，用于定位漏检
	text  string   // 发出的整段文本
	vals  []string // 要遮蔽的值（kind=keep 时是要保留的值）
}

// leakWeights 是待评测数据里各类的条数，用来算按真实分布加权的总召回。
var leakWeights = map[string]int{
	"private_key": 13, "cloud": 113, "db_admin": 11,
	"password": 2370, "token": 11234, "secret": 1625,
}

type leakGen struct{ r *rand.Rand }

const (
	lkLower  = "abcdefghijklmnopqrstuvwxyz"
	lkUpper  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lkDigits = "0123456789"
	lkHexa   = "0123456789abcdef"
	lkAlnum  = lkLower + lkUpper + lkDigits
	lkB64    = lkAlnum + "+/"
	lkB64url = lkAlnum + "-_"
)

func (g leakGen) s(set string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = set[g.r.IntN(len(set))]
	}
	return string(b)
}

// mixed 至少含一个大写、小写和数字，像真实随机 token。
func (g leakGen) mixed(n int) string {
	for {
		v := g.s(lkAlnum, n)
		if strings.ContainsAny(v, lkUpper) && strings.ContainsAny(v, lkLower) && strings.ContainsAny(v, lkDigits) {
			return v
		}
	}
}

func (g leakGen) pick(xs ...string) string { return xs[g.r.IntN(len(xs))] }

func (g leakGen) host() string {
	return g.pick("db", "pg", "mysql-prod", "rds", "10.0.3.17", "192.168.1.20") + "." + g.pick("internal", "corp.example.com", "example.cn", "svc.cluster.local")
}

// password 生成人会设置的各种口令：纯字母、字母加数字、带符号、拼音加年份。
func (g leakGen) password() string {
	switch g.r.IntN(6) {
	case 0:
		return g.s(lkLower+lkUpper, 8+g.r.IntN(6))
	case 1:
		return strings.ToUpper(g.s(lkLower, 1)) + g.s(lkLower, 5+g.r.IntN(4)) + g.s(lkDigits, 3+g.r.IntN(3))
	case 2:
		return g.s(lkUpper, 1) + g.s(lkLower, 4) + g.pick("@", "#", "!", "$", "%", "&", "*") + g.s(lkDigits, 4) + g.pick("!", "?", ".", "")
	case 3:
		return g.pick("zhang", "wang", "liu", "chen", "admin", "root") + g.s(lkLower, 2) + g.pick("2023", "2024", "2025", "2026", "123", "888")
	case 4:
		return g.s(lkAlnum, 12+g.r.IntN(10))
	default:
		return g.s(lkAlnum+"!@#%^*-_=+", 16+g.r.IntN(8))
	}
}

func (g leakGen) pem(kind string, lines int, truncated bool) string {
	var b strings.Builder
	b.WriteString("-----BEGIN " + kind + "-----\n")
	for i := 0; i < lines; i++ {
		b.WriteString(g.s(lkB64, 64) + "\n")
	}
	if truncated {
		return strings.TrimSuffix(b.String(), "\n")
	}
	b.WriteString(g.s(lkB64, 20) + "==\n-----END " + kind + "-----")
	return b.String()
}

// userinfoPW 是口令写进 URL 的样子：特殊字符按 userinfo 的规则百分号编码。
func userinfoPW(pw string) string {
	u := url.UserPassword("u", pw).String()
	return u[2:]
}

func leakCases() []leakCase {
	g := leakGen{rand.New(rand.NewPCG(20261119, 7))}
	var cs []leakCase
	add := func(kind, shape, text string, vals ...string) {
		cs = append(cs, leakCase{kind, shape, text, vals})
	}
	const rounds = 12
	for i := 0; i < rounds; i++ {
		// ---- 私钥 ----
		for _, k := range []string{"RSA PRIVATE KEY", "PRIVATE KEY", "OPENSSH PRIVATE KEY", "EC PRIVATE KEY"} {
			full := g.pem(k, 4+g.r.IntN(20), false)
			add("private_key", "pem-full", "这是我的密钥文件内容:\n"+full+"\n帮我看看为什么 ssh 连不上", full)
			trunc := g.pem(k, 2+g.r.IntN(6), true)
			add("private_key", "pem-truncated", "$ cat ~/.ssh/id_rsa\n"+trunc, trunc)
		}
		sa := g.pem("PRIVATE KEY", 10, false)
		saJSON := strings.ReplaceAll(sa, "\n", `\n`)
		add("private_key", "gcp-sa-json", `{"type": "service_account", "project_id": "demo-`+g.s(lkLower, 6)+`", "private_key_id": "`+g.s(lkHexa, 40)+`", "private_key": "`+saJSON+`\n"}`, saJSON)

		// ---- 云 AK ----
		akid := "AK" + "IA" + g.s(lkUpper+lkDigits, 16)
		aksec := g.s(lkAlnum+"+/", 40)
		add("cloud", "aws-credentials", "[default]\naws_access_key_id = "+akid+"\naws_secret_access_key = "+aksec+"\nregion = us-east-1", akid, aksec)
		add("cloud", "aws-env", "export AWS_ACCESS_KEY_ID="+akid+"\nexport AWS_SECRET_ACCESS_KEY="+aksec, akid, aksec)
		ali := "LT" + "AI" + g.mixed(20)
		aliSec := g.mixed(30)
		add("cloud", "aliyun-yaml", "aliyun:\n  access_key_id: "+ali+"\n  access_key_secret: "+aliSec+"\n  endpoint: oss-cn-hangzhou.aliyuncs.com", ali, aliSec)
		add("cloud", "aliyun-code", "client = AcsClient('"+ali+"', '"+aliSec+"', 'cn-hangzhou')", ali, aliSec)
		tc := "AK" + "ID" + g.mixed(32)
		tcSec := g.mixed(32)
		add("cloud", "tencent-env", "TENCENTCLOUD_SECRET_ID="+tc+"\nTENCENTCLOUD_SECRET_KEY="+tcSec, tc, tcSec)
		hw := g.s(lkUpper+lkDigits, 20)
		hwSec := g.mixed(40)
		add("cloud", "huawei-ak-sk", "华为云的 AK 是 "+hw+"，SK 是 "+hwSec+"，帮我写个 obs 上传脚本", hw, hwSec)
		azKey := g.s(lkB64, 86) + "=="
		add("cloud", "azure-conn", "AZURE_STORAGE_CONNECTION_STRING=DefaultEndpointsProtocol=https;AccountName=demo"+g.s(lkLower, 6)+";AccountKey="+azKey+";EndpointSuffix=core.windows.net", azKey)
		volc := "AK" + "LT" + g.mixed(40)
		volcSec := g.s(lkB64, 60)
		add("cloud", "volc-json", `{"access_key": "`+volc+`", "secret_key": "`+volcSec+`"}`, volc, volcSec)
		gcpKey := "AI" + "za" + "Sy" + g.s(lkB64url, 33)
		add("cloud", "gcp-apikey", `const firebaseConfig = { apiKey: "`+gcpKey+`", authDomain: "demo.firebaseapp.com" };`, gcpKey)

		// ---- 数据库管理员 ----
		pw := userinfoPW(g.password())
		add("db_admin", "pg-url", "DATABASE_URL=postgres://postgres:"+pw+"@"+g.host()+":5432/app", pw)
		pw = g.password()
		add("db_admin", "mysql-cli", "mysql -h "+g.host()+" -uroot -p"+pw+" -e 'show databases'", pw)
		pw = g.password()
		add("db_admin", "mysql-cli-space", "mysql -u root --password="+pw+" app < dump.sql", pw)
		pw = userinfoPW(g.password())
		add("db_admin", "mongo-srv", "mongodb+srv://admin:"+pw+"@cluster0."+g.s(lkLower, 5)+".mongodb.net/?retryWrites=true", pw)
		pw = userinfoPW(g.password())
		add("db_admin", "redis-url", "REDIS_URL=redis://default:"+pw+"@"+g.host()+":6379/0", pw)
		pw = g.password()
		add("db_admin", "pgpass", "~/.pgpass:\n"+g.host()+":5432:*:postgres:"+pw, pw)
		pw = g.password()
		add("db_admin", "jdbc", "spring.datasource.url=jdbc:mysql://"+g.host()+":3306/app?user=root&password="+pw+"&useSSL=false", pw)
		pw = g.password()
		add("db_admin", "docker-root", "docker run -d -e MYSQL_ROOT_PASSWORD="+pw+" -p 3306:3306 mysql:8", pw)
		pw = g.password()
		add("db_admin", "compose", "services:\n  db:\n    image: postgres:16\n    environment:\n      POSTGRES_USER: postgres\n      POSTGRES_PASSWORD: "+pw, pw)
		pw = g.password()
		add("db_admin", "sqlcmd", "sqlcmd -S "+g.host()+" -U sa -P '"+pw+"' -Q \"SELECT 1\"", pw)
		pw = g.password()
		add("db_admin", "zh-chat", "数据库地址 "+g.host()+"，用户名 root，密码 "+pw+"，帮我连一下看看表结构", pw)

		// ---- 口令 ----
		pw = g.password()
		add("password", "dotenv", "ADMIN_PASSWORD="+pw, pw)
		pw = g.password()
		add("password", "yaml", "smtp:\n  user: noreply@example.com\n  password: "+pw, pw)
		pw = g.password()
		add("password", "yaml-quoted", "auth:\n  password: \""+pw+"\"", pw)
		pw = g.password()
		add("password", "json", `{"username": "admin", "password": "`+pw+`"}`, pw)
		pw = g.password()
		add("password", "python", "conn = connect(host='"+g.host()+"', user='admin', password='"+pw+"')", pw)
		pw = g.password()
		add("password", "sshpass", "sshpass -p '"+pw+"' ssh ubuntu@"+g.host(), pw)
		pw = g.password()
		add("password", "cli-flag", "./deploy.sh --user admin --password "+pw, pw)
		pw = userinfoPW(g.password())
		add("password", "url-userinfo", "git clone https://deploy:"+pw+"@git.example.com/team/app.git", pw)
		pw = g.password()
		add("password", "zh-colon", "服务器 "+g.host()+" 账号 ubuntu 密码："+pw, pw)
		pw = g.password()
		add("password", "zh-is", "我的密码是 "+pw+" 帮我登录试试", pw)
		pw = g.password()
		add("password", "pwd-key", "PWD="+pw+"\nUSER=admin", pw)
		pw = g.password()
		add("password", "ini", "[smtp]\nhost = smtp.example.com\npass = "+pw, pw)
		pw = g.password()
		add("password", "xml", "<property name=\"password\" value=\""+pw+"\"/>", pw)

		// ---- Token ----
		tokens := []struct{ shape, v string }{
			{"openai", "sk-" + "proj-" + g.s(lkB64url, 48) + "T3Blbk" + "FJ" + g.s(lkB64url, 48)},
			{"anthropic", "sk-" + "ant-" + "api03-" + g.s(lkB64url, 93) + "AA"},
			{"github-classic", "gh" + "p_" + g.mixed(36)},
			{"github-fine", "github" + "_pat_" + g.mixed(22) + "_" + g.mixed(59)},
			{"gitlab", "gl" + "pat-" + g.s(lkB64url, 20)},
			{"slack-bot", "xo" + "xb-" + g.s(lkDigits, 12) + "-" + g.s(lkDigits, 13) + "-" + g.mixed(24)},
			{"stripe", "sk" + "_live_" + g.mixed(99)},
			{"huggingface", "h" + "f_" + g.mixed(34)},
			{"npm", "np" + "m_" + g.mixed(36)},
			{"deepseek", "sk-" + g.s(lkHexa, 32)},
			{"groq", "gs" + "k_" + g.mixed(52)},
			{"telegram", g.s(lkDigits, 10) + ":AA" + g.s(lkB64url, 33)},
			{"jwt", "ey" + "J" + g.s(lkB64url, 35) + ".ey" + "J" + g.s(lkB64url, 80) + "." + g.s(lkB64url, 43)},
			{"hex40", g.s(lkHexa, 40)},
			{"hex64", g.s(lkHexa, 64)},
			{"random32", g.mixed(32)},
			{"zhipu", g.s(lkHexa, 32) + "." + g.mixed(16)},
			{"uuid", g.s(lkHexa, 8) + "-" + g.s(lkHexa, 4) + "-4" + g.s(lkHexa, 3) + "-a" + g.s(lkHexa, 3) + "-" + g.s(lkHexa, 12)},
		}
		for _, t := range tokens {
			ctx := g.r.IntN(7)
			v := t.v
			switch ctx {
			case 0:
				add("token", t.shape+"/env", "API_TOKEN="+v, v)
			case 1:
				add("token", t.shape+"/bearer", "curl -H \"Authorization: Bearer "+v+"\" https://api.example.com/v1/me", v)
			case 2:
				add("token", t.shape+"/json", `{"api_key": "`+v+`", "model": "x"}`, v)
			case 3:
				add("token", t.shape+"/code", "client = Client(api_key=\""+v+"\")", v)
			case 4:
				add("token", t.shape+"/header", "headers = {\"X-API-Key\": \""+v+"\"}", v)
			case 5:
				add("token", t.shape+"/query", "https://api.example.com/hook?access_token="+v+"&id=3", v)
			default:
				add("token", t.shape+"/zh", "这是我的 token："+v+" 你帮我调一下接口", v)
			}
		}

		// ---- 其他 Secret ----
		sec := []struct{ shape, text, v string }{}
		v := g.s(lkAlnum+"!@#$%^&*(-_=+)", 50)
		sec = append(sec, struct{ shape, text, v string }{"django", "SECRET_KEY = '" + v + "'", v})
		v = g.mixed(32)
		sec = append(sec, struct{ shape, text, v string }{"client-secret", "OAUTH_CLIENT_SECRET=" + v, v})
		v = g.s(lkB64, 43) + "="
		sec = append(sec, struct{ shape, text, v string }{"jwt-secret", "jwt:\n  secret: " + v, v})
		v = "wh" + "sec_" + g.s(lkB64, 32)
		sec = append(sec, struct{ shape, text, v string }{"webhook", "STRIPE_WEBHOOK_SECRET=" + v, v})
		v = g.s(lkHexa, 32)
		sec = append(sec, struct{ shape, text, v string }{"wechat-appsecret", "{\"appid\": \"wx" + g.s(lkHexa, 16) + "\", \"appsecret\": \"" + v + "\"}", v})
		v = g.s(lkB64, 44)
		sec = append(sec, struct{ shape, text, v string }{"enc-key", "ENCRYPTION_KEY=" + v, v})
		v = g.mixed(24)
		sec = append(sec, struct{ shape, text, v string }{"session", "app.use(session({ secret: '" + v + "', resave: false }))", v})
		v = g.mixed(40)
		sec = append(sec, struct{ shape, text, v string }{"zh-secret", "飞书应用的 App Secret 是 " + v, v})
		v = g.s(lkAlnum+"~.-_", 40)
		sec = append(sec, struct{ shape, text, v string }{"azure-client", "AZURE_CLIENT_SECRET=" + v, v})
		for _, s := range sec {
			add("secret", s.shape, s.text, s.v)
		}

		// ---- 不该遮的正常内容 ----
		sha := g.s(lkHexa, 40)
		add("keep", "git-sha", "git cherry-pick "+sha+"\n修复了 commit "+sha[:9]+" 的问题", sha)
		add("keep", "var-ref", "password: ${DB_PASSWORD}\ntoken: process.env.API_TOKEN", "${DB_PASSWORD}", "process.env.API_TOKEN")
		add("keep", "placeholder", "API_KEY=your-api-key-here\nPASSWORD=changeme", "your-api-key-here")
		add("keep", "integrity", `"integrity": "sha512-`+g.s(lkB64, 86)+`=="`, "sha512-")
		add("keep", "docker-digest", "image: nginx@sha256:"+g.s(lkHexa, 64), "nginx@sha256")
		add("keep", "code", "func validatePassword(password string) error {\n\tif len(password) < 8 {\n\t\treturn errTooShort\n\t}\n\treturn nil\n}", "validatePassword", "errTooShort")
		add("keep", "pkg", "npm install @tanstack/react-query jsonwebtoken bcryptjs", "@tanstack/react-query", "jsonwebtoken", "bcryptjs")
		add("keep", "pubkey", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI"+g.s(lkB64, 43)+" dev@laptop", "ssh-ed25519")
		add("keep", "log", "2026-11-19T10:21:33Z INFO request_id=req_01HZX3 user_id=42 status=200 latency=12ms", "request_id", "latency=12ms")
		add("keep", "zh-prose", "密码字段至少 8 位，token 过期后需要重新登录，secret 不要提交到仓库。", "密码字段至少 8 位", "token 过期后需要重新登录")
	}
	return cs
}

// leftover 说 out 里是否还留着 v 的 6 字符以上片段（占位符先去掉）。
func leftover(out, v string) bool {
	o := unmarked(out)
	const k = 6
	if len(v) <= k {
		return strings.Contains(o, v)
	}
	for i := 0; i+k <= len(v); i++ {
		if strings.Contains(o, v[i:i+k]) {
			return true
		}
	}
	return false
}

func TestLeakEval(t *testing.T) {
	runLeakEval(t, leakCases())
}

func runLeakEval(t *testing.T, cases []leakCase) {
	type tally struct{ ok, n int }
	byKind := map[string]*tally{}
	byShape := map[string]*tally{}
	var misses []string
	for _, c := range cases {
		Forget()
		out, _ := Mask(c.text, Options{Secrets: true})
		if back := Restore(out, false); back != c.text {
			t.Errorf("%s/%s round trip changed text:\n%q\n→ %q", c.kind, c.shape, c.text, back)
		}
		key := c.kind + "/" + c.shape
		if byKind[c.kind] == nil {
			byKind[c.kind] = &tally{}
		}
		if byShape[key] == nil {
			byShape[key] = &tally{}
		}
		for _, v := range c.vals {
			var right bool
			if c.kind == "keep" {
				right = strings.Contains(out, v)
			} else {
				right = !leftover(out, v)
			}
			byKind[c.kind].n++
			byShape[key].n++
			if right {
				byKind[c.kind].ok++
				byShape[key].ok++
			} else if len(misses) < 400 {
				misses = append(misses, fmt.Sprintf("%s: %q → %q", key, c.text, out))
			}
		}
	}
	kinds := []string{"private_key", "cloud", "db_admin", "password", "token", "secret", "keep"}
	var wsum, wok float64
	var report strings.Builder
	for _, k := range kinds {
		tl := byKind[k]
		rate := float64(tl.ok) / float64(tl.n)
		fmt.Fprintf(&report, "%-12s %4d/%-4d %6.1f%%\n", k, tl.ok, tl.n, 100*rate)
		if w, ok := leakWeights[k]; ok {
			wsum += float64(w)
			wok += float64(w) * rate
		}
	}
	fmt.Fprintf(&report, "按待评测分布加权的完整遮蔽率 %.1f%%\n", 100*wok/wsum)
	var shapes []string
	for s, tl := range byShape {
		if tl.ok < tl.n {
			shapes = append(shapes, fmt.Sprintf("  %-32s %3d/%-3d", s, tl.ok, tl.n))
		}
	}
	sort.Strings(shapes)
	report.WriteString("未全对的写法:\n" + strings.Join(shapes, "\n") + "\n")
	t.Log("\n" + report.String())
	if testing.Verbose() {
		for _, m := range misses {
			t.Log(m)
		}
	}
}
