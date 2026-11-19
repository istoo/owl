package redact

import (
	"regexp"
	"strings"
)

// 日志或工具输出被截断时，私钥往往只剩明确头加若干行密钥正文，没有
// END 行。完整块规则不匹配这种文本，密钥就原样去了供应商。这里补一个
// 窄规则：头必须是标准 PEM 私钥头，正文必须紧跟着像 base64 的连续行，
// 只取能确认的正文，不吞后续普通日志或源码。
// debt: 依赖常见正文行宽与明确的头；无上下文的裸字符串不判为密钥。若需
// 识别带装甲头字段的格式，应按对应格式解析，不放宽通用字符匹配。

var privateKeyHeader = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)

const (
	// 标准 PEM 正文每行 64 字符，OpenSSH 的 70 和少数工具的 76 也收。
	keyFullLineMin = 48
	keyFullLineMax = 76
	// 末行通常短于完整宽度；太短的行无法与普通日志词区分，不收。
	keyTailLineMin = 16
	// 上限覆盖真实的 RSA、EC、OpenSSH 和 PGP 私钥（通常几十行）。
	keyMaxLines = 1024
)

// truncatedKeySpans 找出没有 END 行的私钥块。s 可能是普通文本，也可能是
// 工具参数里把换行写成 \n 的 JSON 文本；后者在 unescapedView 里换行是
// 真换行，前面还留着转义用的反斜杠，keyLineAt 会把行尾的反斜杠去掉再
// 判断，位置仍与原始文本一一对应。
func truncatedKeySpans(s string) []span {
	if !strings.Contains(s, "PRIVATE KEY") {
		return nil
	}
	var found []span
	for _, h := range privateKeyHeader.FindAllStringIndex(s, -1) {
		body, ok := keyLineBreak(s, h[1])
		if !ok {
			continue
		}
		end, ok := truncatedKeyBody(s, body)
		if !ok {
			continue
		}
		found = append(found, span{h[0], end, "PRIVATE_KEY", ByRule, 0, false})
	}
	return found
}

// keyLineBreak 是头这一行之后正文开始的位置。普通文本是 \n 或 \r\n；
// 工具参数的 unescapedView 里，换行前还留着一个反斜杠。
func keyLineBreak(s string, i int) (int, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch s[i] {
	case '\n':
		return i + 1, true
	case '\r':
		if i+1 < len(s) && s[i+1] == '\n' {
			return i + 2, true
		}
	case '\\':
		j := i + 1
		if j < len(s) && s[j] == '\r' {
			j++
		}
		if j < len(s) && s[j] == '\\' {
			j++
		}
		if j < len(s) && s[j] == '\n' {
			return j + 1, true
		}
	}
	return 0, false
}

// keyLineAt 读 pos 开始的一行：data 是去掉行尾换行、回车和转义留下的
// 反斜杠后的正文，next 是下一行的起点。
func keyLineAt(s string, pos int) (data string, next int, ok bool) {
	if pos >= len(s) {
		return "", 0, false
	}
	rawEnd := len(s)
	if nl := strings.IndexByte(s[pos:], '\n'); nl >= 0 {
		rawEnd = pos + nl
	}
	end := rawEnd
	for end > pos {
		if c := s[end-1]; c == '\r' || c == '\\' {
			end--
			continue
		}
		break
	}
	if rawEnd < len(s) {
		rawEnd++ // 跳过行尾的 \n；没有 \n 时 next 停在文本末尾
	}
	return s[pos:end], rawEnd, true
}

// keyBodyLine 说 line 是否是私钥正文可能的一行：base64 字母表，末尾最多
// 两个 =。不含空格的连续 base64 才是线；普通句子在这里停住。
func keyBodyLine(line string) bool {
	if len(line) < 8 {
		return false
	}
	n := len(line)
	pads := 0
	for n > 0 && line[n-1] == '=' {
		n--
		pads++
		if pads > 2 {
			return false
		}
	}
	if n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		c := line[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' {
			continue
		}
		return false
	}
	return true
}

// truncatedKeyBody 从正文第一行读到能确认的最后一行，返回遮蔽终点。
// 至少要有完整宽度的一行；只有截断落在第一行且后面别无内容时，才收
// 一个短片段。短末行只在下一行不再像密钥行时才收，不吞后面的日志词。
func truncatedKeyBody(s string, pos int) (int, bool) {
	full := 0
	end := 0
	confirmed := false
body:
	for lines := 0; lines < keyMaxLines; lines++ {
		line, next, ok := keyLineAt(s, pos)
		if !ok || line == "" || !keyBodyLine(line) {
			break
		}
		n := len(line)
		switch {
		case n >= keyFullLineMin && n <= keyFullLineMax:
			full++
			confirmed = true
			end = pos + n
			pos = next
		case n >= keyTailLineMin && n < keyFullLineMin && full > 0:
			// 末行：下一行不再像密钥行时才收。
			if !nextIsKeyLine(s, next) {
				confirmed = true
				end = pos + n
			}
			break body
		case n >= keyTailLineMin && n < keyFullLineMin:
			// 截断落在第一行正文里，只剩一个片段。只有它后面确实
			// 没有别的内容时才收，免得把普通日志词当成密钥。
			if restBlank(s, next) {
				confirmed = true
				end = pos + n
			}
			break body
		default:
			break body
		}
	}
	if !confirmed || end == 0 {
		return 0, false
	}
	return end, true
}

// restBlank 说 pos 之后只剩空白或转义留下的反斜杠。
func restBlank(s string, pos int) bool {
	for ; pos < len(s); pos++ {
		switch s[pos] {
		case ' ', '\t', '\r', '\n', '\\':
		default:
			return false
		}
	}
	return true
}

// nextIsKeyLine 说 pos 开始的一行是否还可能属于私钥正文。是的话，上一
// 个短行就不是末行，宁可少收一个短行，也不把普通日志当成正文吞掉。
func nextIsKeyLine(s string, pos int) bool {
	line, _, ok := keyLineAt(s, pos)
	if !ok {
		return false
	}
	n := len(line)
	return n >= keyTailLineMin && n <= keyFullLineMax && keyBodyLine(line)
}
