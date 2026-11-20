package redact

import (
	"strings"

	"github.com/istoo/owl/internal/lazyre"
)

// credRules 补的是 AI 写代码聊天里凭据最常见、而字段名规则看不到的写法：
// 命令行参数、curl 的 Bearer 头、厂商有固定前缀的云 AK、连接串和 XML
// 属性、.pgpass 行。每条只认明确的上下文，值仍过变量、路径的排除。
//
// debt: 这些都是按上下文写的规则，覆盖不到无名、无前缀、无上下文的裸值；
// 要再往上提召回，得靠本地模型（Options.Model）而不是继续放宽正则。
var credRules = []rule{
	// curl -H "Authorization: Bearer …"、代码里的 "Bearer " + 值：行首的
	// Authorization 头规则看不到写在命令和字符串里的这种
	{kind: "TOKEN", re: lazyre.New(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=:-]{16,})`), fold: []string{"bearer"}, back: 8, ahead: 2048, ok: secretValue},
	// 代码里的头：{'Authorization': 'token …'}、Authorization: `Bearer …`、
	// setHeader("Authorization", "Basic …")。行首头规则看不到写在代码里的这种
	{kind: "TOKEN", re: lazyre.New(`(?i)authorization\\?["'` + "`" + `]?[ \t]*[:=,][ \t]*\\?["'` + "`" + `]?(?:(?:bearer|token|basic)[ \t]+)?([A-Za-z0-9._~+/=:-]{16,})`),
		fold: []string{"authorization"}, back: 8, ahead: 2048, ok: authValue},
	// 口令写在名字和 is / was 后面，像英文消息里的 the password is …、
	// my api key is …。值要像凭据（字母加数字或符号），the password is wrong 不算
	{kind: "SECRET", re: lazyre.New(`(?i)\b(?:password|passwd|passcode|api[ _-]?key|access[ _-]?key|secret[ _-]?key|secret)[ \t]+(?:is|was)[ \t]*:?[ \t]+\\?["'` + "`" + `]?([A-Za-z0-9_\-./+=~!@#%^&*$]{6,})`),
		fold: []string{" is", " was"}, back: 40, ahead: 200, fix: proseSecretFix},
	// token 后面同样：the token is …。但全文大写的字母数字串是编号（0X7FFE0300、
	// 十六进制或 base32 的 id），和十六进制数一样照原样发（TestHexNumberGoesAsWritten）。
	{kind: "SECRET", re: lazyre.New(`(?i)\btoken[ \t]+(?:is|was)[ \t]*:?[ \t]+\\?["'` + "`" + `]?([A-Za-z0-9_\-./+=~!@#%^&*$]{6,})`),
		fold: []string{" is", " was"}, back: 40, ahead: 200, fix: proseTokenFix},
	// 中文消息里口令名后跟 是、为、：：pwd是…、password：…。口令可以只有字母
	{kind: "PASSWORD", re: lazyre.New(`(?i)\b(?:password|passwd|pwd|pass)` + quote + `[ \t]*(?:[为為是][ \t]*[:：]?|：)[ \t]*(?:\\?["'“「])?(` + secretCh + `{3,}[A-Za-z0-9_\-/+=~!@#%^&*$])`),
		fold: cjkCopulas, back: 24, ahead: 120, ok: passValue},
	// ini 和 .env 里的 pass = …、pwd = …。只认等号：pass: … 在英文里是普通的话，
	// --- PASS: TestX 是测试输出
	{kind: "PASSWORD", re: lazyre.New(`(?:^|[^A-Za-z0-9_.-])(?:pass|pwd|PWD)` + quote + `[ \t]*=[ \t]*` + quote + `(` + secretCh + `{8,})`),
		markers: []string{"pass", "pwd", "PWD"}, ok: passValue, at: fieldAt, field: true},
	// JSON 里带引号的键："pass": "…"
	{kind: "PASSWORD", re: lazyre.New(`\\?"(?:pass|pwd)\\?"[ \t]*:[ \t]*\\?"(` + secretCh + `{8,})`),
		markers: []string{"pass\"", "pwd\"", "pass\\\"", "pwd\\\""}, ok: passValue, at: fieldAt, field: true},
	// Ansible 的连接口令：ansible_ssh_pass、ansible_become_pass、ansible_password
	{kind: "PASSWORD", re: lazyre.New(`ansible_(?:ssh_|become_|sudo_)?pass(?:word)?` + quote + `[ \t]*[:=][ \t]*` + quote + `(` + secretCh + `{4,})`),
		markers: []string{"ansible_"}, ok: passValue, at: fieldAt, field: true},
	// curl -u 用户:口令、--user 用户:口令
	{kind: "PASSWORD", re: lazyre.New(`curl\b[^\n]*?[ \t](?:-u|--user)[ \t]*["']?[^\s:'"]+:([^\s'"]{4,})`), markers: []string{"curl"}, ok: passValue},
	// redis.conf：requirepass 口令、masterauth 口令
	{kind: "PASSWORD", re: lazyre.New(`(?m)^[ \t]*(?:requirepass|masterauth)[ \t]+["']?([^\s"']{4,})`), markers: []string{"requirepass", "masterauth"}, ok: notAVariable},
	// aws configure set aws_secret_access_key 值
	{kind: "SECRET", re: lazyre.New(`aws[ \t]+configure[ \t]+set[ \t]+(?:[a-z0-9_.-]*\.)?(?:aws_secret_access_key|aws_session_token)[ \t]+([^\s'"]{16,})`), markers: []string{"configure"}, ok: notAVariable},
	// ~/.netrc：machine 主机 login 用户 password 口令
	{kind: "PASSWORD", re: lazyre.New(`(?:^|\s)machine[ \t]+\S+(?:\s+login[ \t]+\S+)?\s+password[ \t]+(\S{4,})`), markers: []string{"machine"}, ok: notAVariable},
	// Docker Hub 的个人访问令牌
	{kind: "API_KEY", re: lazyre.New(`dckr_pat_[A-Za-z0-9_-]{20,}`), markers: []string{"dckr_pat_"}, bound: tokenCh},
	// 机器人 webhook 的地址本身就是凭据：Slack、Discord、飞书/Lark、企业微信
	{kind: "TOKEN", re: lazyre.New(`(?:hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/([A-Za-z0-9]{16,})|discord(?:app)?\.com/api/webhooks/\d+/([A-Za-z0-9_-]{30,})|open\.(?:feishu\.cn|larksuite\.com)/open-apis/bot/v2/hook/([A-Za-z0-9-]{16,})|qyapi\.weixin\.qq\.com/cgi-bin/webhook/send\?key=([A-Za-z0-9-]{16,}))`),
		markers: []string{"hooks.slack.com", "/api/webhooks/", "/bot/v2/hook/", "qyapi.weixin.qq.com"}, anyGroup: true},
	// Azure 的 SAS 地址：sv=… 之后的 sig= 是签名
	{kind: "TOKEN", re: lazyre.New(`[?&]sv=[^\s&]+(?:&[^\s&=]+=[^\s&]*)*?&sig=([A-Za-z0-9%+/=]{20,})`), markers: []string{"sig="}},
	// Laravel 的 APP_KEY=base64:…，值在 base64: 后面
	{kind: "SECRET", re: lazyre.New(`(?:^|[^A-Za-z0-9_])[A-Z0-9_]*_KEY[ \t]*[:=][ \t]*\\?["']?base64:([A-Za-z0-9+/=]{16,})`), markers: []string{"base64:"}},
	// 名字就说明是密钥的小写字段：api_v3_key、mch_key、app_key、sign_key、
	// encrypt_key 等。通用的 *_key 仍不认（sort_key、cache_key 是代码）
	{kind: "SECRET", re: lazyre.New(`(?i)(?:^|[^A-Za-z0-9_.-])(?:api_?v3_?key|mch_?key|app_?key|sign(?:ing)?_?key|encrypt(?:ion)?_?key|aes_?key|hmac_?key|master_?key)` + quote + `[ \t]*[:=][ \t]*` + quote + `(` + secretCh + `{16,})`),
		fold: []string{"key"}, back: 40, ahead: 200, ok: secretValue, at: fieldAt, field: true},
	// MinIO 客户端：mc alias set 名字 地址 AccessKey SecretKey
	{kind: "SECRET", re: lazyre.New(`\bmc[ \t]+(?:alias[ \t]+set|config[ \t]+host[ \t]+add)[ \t]+\S+[ \t]+\S+[ \t]+([^\s'"]{8,})[ \t]+([^\s'"]{8,})`),
		markers: []string{"mc "}, groups: []string{"SECRET", "SECRET"}, groupOK: []func(string) bool{cliSecret, cliSecret}},
	// echo 口令 | sudo -S …；echo user:口令 | chpasswd
	{kind: "PASSWORD", re: lazyre.New(`echo[ \t]+(?:'([^'\n]{4,128})'|"([^"\n]{4,128})"|([^\s'"|]{4,128}))[ \t]*\|[ \t]*sudo\b[^\n|]*?[ \t]-S`), markers: []string{"sudo"}, anyGroup: true, ok: cliSecret},
	{kind: "PASSWORD", re: lazyre.New(`echo[ \t]+["']?[A-Za-z0-9_.-]+:([^\s'":]{4,128})["']?[ \t]*\|[ \t]*(?:sudo[ \t]+)?chpasswd`), markers: []string{"chpasswd"}, ok: cliSecret},
	// htpasswd -b 文件 用户 口令（-b 是从命令行取口令）
	{kind: "PASSWORD", re: lazyre.New(`htpasswd[ \t]+(?:-[A-Za-z]+[ \t]+)*?-[A-Za-z]*b[A-Za-z]*[ \t]+(?:-[A-Za-z]+[ \t]+)*\S+[ \t]+\S+[ \t]+` + cliValue), markers: []string{"htpasswd"}, anyGroup: true, ok: cliSecret},
	// expect 脚本：等到 password: 提示后 send 的那一行
	{kind: "PASSWORD", re: lazyre.New(`(?i)expect[ \t]+["'][^"'\n]*(?:password|passphrase)[^"'\n]*["'][^\n]*\n[ \t]*send[ \t]+["']([^"'\n\\]{4,})(?:\\r|\\n)?["']`),
		fold: []string{"expect"}, back: 8, ahead: 400, ok: notAVariable},
	// Telegram 机器人：数字 id、冒号、AA 开头的 35 个字符
	{kind: "API_KEY", re: lazyre.New(`\d{8,10}:AA[A-Za-z0-9_-]{32,34}`), markers: []string{":AA"}, bound: tokenCh},
	// 阿里云 LTAI、腾讯云 AKID、火山引擎 AKLT 开头的 AccessKey ID
	{kind: "API_KEY", re: lazyre.New(`(?:LTAI[A-Za-z0-9]{12,28}|AKID[A-Za-z0-9]{13,40}|AKLT[A-Za-z0-9_-]{20,60})`), markers: []string{"LTAI", "AKID", "AKLT"}, bound: tokenCh, ok: secretValue},
	// AcsClient('LTAI…', '…')：紧跟在 AccessKey ID 后面的下一个参数是它的 Secret
	{kind: "SECRET", re: lazyre.New(`(?:LTAI|AKID|AKLT|AKIA|ASIA)[A-Za-z0-9_-]{12,60}\\?["'][ \t]*,[ \t]*\\?["']([A-Za-z0-9+/=_-]{20,100})\\?["']`),
		markers: []string{"LTAI", "AKID", "AKLT", "AKIA", "ASIA"}, ok: secretValue},
	// Azure 存储、服务总线连接串里的 AccountKey=…; SharedAccessKey=…
	{kind: "SECRET", re: lazyre.New(`(?:AccountKey|SharedAccessKey)=([A-Za-z0-9+/=]{20,})`), markers: []string{"AccountKey=", "SharedAccessKey="}},
	// 命令行里的口令：mysql -p口令（紧跟，空格后是库名）、sshpass -p、
	// sqlcmd -P、redis-cli -a，以及任何命令的 --password / --token 值
	{kind: "PASSWORD", re: lazyre.New(`(?:mysql|mysqldump|mysqladmin|mariadb|mariadb-dump)\b[^\n]*?[ \t]-p` + cliValue),
		markers: []string{"mysql", "mariadb"}, anyGroup: true, ok: cliSecret},
	{kind: "PASSWORD", re: lazyre.New(`(?:sshpass[^\n]*?[ \t]-p|sqlcmd[^\n]*?[ \t]-P|bcp[ \t][^\n]*?[ \t]-P|redis-cli[^\n]*?[ \t]-a|docker[ \t]+login[^\n]*?[ \t]-p)[ \t]?` + cliValue),
		markers: []string{"sshpass", "sqlcmd", "bcp ", "redis-cli", "docker"}, anyGroup: true, ok: cliSecret},
	// 单横线的长选项：influx -password …、keytool -storepass …
	{kind: "PASSWORD", re: lazyre.New(`(?:^|[ \t])-(?:password|passwd|storepass|keypass|srcstorepass|deststorepass)[ \t]+` + cliValue),
		fold: []string{"pass"}, back: 24, ahead: 160, anyGroup: true, ok: cliStrong},
	{kind: "SECRET", re: lazyre.New(`(?:^|[ \t])--(?:password|passwd|pass|token|api-key|apikey|secret|client-secret|access-token|auth-token|access-key-secret|secret-key|secret-access-key)[ \t]+` + cliValue),
		markers: []string{"--"}, anyGroup: true, ok: cliStrong},
	// XML 配置：<property name="password" value="…"/>、<password>…</password>
	{kind: "SECRET", re: lazyre.New(`(?i)(?:name|key)=\\?"(?:[A-Za-z0-9_.-]*[_.-])?(?:password|passwd|secret|token|api[_-]?key)\\?"[ \t]+value=\\?"([^"\\\s]{4,})\\?"`),
		fold: []string{"value="}, back: 160, ahead: 400, ok: literalSecretValue},
	{kind: "SECRET", re: lazyre.New(`(?i)<(?:[a-z0-9_.-]*[_.-])?(?:password|passwd|secret|token|api[_-]?key)>([^<\s]{4,})</`),
		fold: []string{"</"}, back: 400, ahead: 40, ok: literalSecretValue},
	// 引号里的值可以带任何符号：SECRET_KEY = '(ytfo…=r@B7…'。字段规则的字
	// 符集在 ( 处停住，只遮前半截；这里取到配对的引号为止
	{kind: "SECRET", re: lazyre.New(`(?i)(?:^|[^A-Za-z0-9_.-])[A-Za-z0-9_.-]*(?:secret|password|passwd|token|api[_-]?key)[A-Za-z0-9_.-]*[ \t]*[:=][ \t]*(?:'([^'\s\\:]{8,})'|"([^"\s\\:]{8,})")`),
		markers: secretNames.markers(), anyGroup: true, ok: secretValue, at: fieldAt, field: true},
	// ~/.pgpass 的一行：host:port:db:user:password。只在文本提到 pgpass 时认，
	// 免得把 file.go:12:3:warning:… 这种编译输出当成口令
	{kind: "PASSWORD", re: lazyre.New(`(?m)^[^\s:#]+:(?:\d{1,5}|\*):[^\s:]+:[^\s:]+:([^\s]{4,})$`), fold: []string{"pgpass"}, back: 64, ahead: 4096, ok: notAVariable},
}

// authValue 是 Authorization 头里的凭据，不是签名算法名（AWS4-HMAC-SHA256 Credential=…）。
func authValue(v string) bool {
	return secretValue(v) && !strings.HasPrefix(v, "AWS4-")
}

// cliStrong 是任何命令都可能有的 --password、--token 后面的值：除了 cliSecret，
// 还要有数字、符号或词中的大写字母，免得把帮助文本里的 --token string、
// --password Password 当成口令。专门工具的 -p（mysql、sshpass）不受这条限制。
func cliStrong(v string) bool {
	return cliSecret(v) && (strings.ContainsAny(v, digits+"+=~!@#%^&*$_") ||
		len(v) > 1 && strings.IndexFunc(v[1:], func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0)
}

// proseSecret 是消息里 is 后面的值：字面值，且有数字或符号，不是一个英文词。
func proseSecret(v string) bool {
	return len(v) >= 6 && literalSecretValue(v) && !pathValue(v) && strings.ContainsAny(v, digits+"+=~!@#%^&*$")
}

// proseSecretFix 去掉句末的标点再判断：the password is Abcd1234. 的句号不是口令。
func proseSecretFix(s string, a, b int) (int, int, bool) {
	for b > a && strings.ContainsRune(".!?", rune(s[b-1])) {
		b--
	}
	return a, b, proseSecret(s[a:b])
}

// proseTokenFix 是 token is … 的那一半，判断同 proseSecretFix。
func proseTokenFix(s string, a, b int) (int, int, bool) {
	for b > a && strings.ContainsRune(".!?", rune(s[b-1])) {
		b--
	}
	return a, b, proseTokenSecret(s[a:b])
}

// proseTokenSecret 说 token is 后面的值是不是一个 token。全文大写的字母数字串
// 是编号：0X7FFE0300、十六进制或 base32 的 id，和十六进制数一样照原样发
// （TestHexNumberGoesAsWritten）。值里有小写字母或符号才是 token。
func proseTokenSecret(v string) bool {
	if !proseSecret(v) {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z':
			return true
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return true // 符号，只有凭据会有
		}
	}
	return false
}

// cliValue 是命令行参数的值：单引号、双引号里的，或不带引号的一段
const cliValue = `(?:'([^'\n]{4,128})'|"([^"\n]{4,128})"|([^\s'"\\-][^\s'"\\]{3,127}))`

// cliSecret 排除命令行里代替口令的东西：$VAR、$(cat …)、文件路径、另一个参数。
func cliSecret(v string) bool {
	return literalSecretValue(v) && !pathValue(v) && !strings.HasPrefix(v, "-") && !strings.HasPrefix(v, "`")
}
