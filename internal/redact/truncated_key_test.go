package redact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// 这些是不含真实凭据的虚构 base64 行，只用来检查规则形状。
var (
	keyLineA = strings.Repeat("MIIEowIBAAKCAQEA", 4)  // 64 字符
	keyLineB = strings.Repeat("uJ8nQvXbLmYtRcWd", 4)  // 64 字符
	keyTail  = strings.Repeat("aB3dE6gH", 4) + "aB3d" // 36 字符
)

// pemBlock 是一个完整私钥块；truncatedPEM 是缺少 END 行的同一种文本，
// 像日志或工具输出被截断后留下的样子。
func pemBlock(kind string, lines ...string) string {
	return "-----BEGIN " + kind + "-----\n" + strings.Join(lines, "\n") + "\n-----END " + kind + "-----"
}

func truncatedPEM(kind string, lines ...string) string {
	return "-----BEGIN " + kind + "-----\n" + strings.Join(lines, "\n")
}

// 一个明确私钥头后跟像密钥数据的连续行，即使没有 END 行也必须整体遮蔽，
// 并且能逐字节换回；头本身不被单独当作密钥，普通文字、公钥和无头裸值不动。
func TestTruncatedPrivateKeyCases(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		secrets bool
		want    int
		gone    []string
		kept    []string
	}{
		{
			name: "one full line", in: truncatedPEM("PRIVATE KEY", keyLineA), secrets: true, want: 1,
			gone: []string{"BEGIN PRIVATE KEY", keyLineA},
		},
		{
			name: "rsa two lines", in: truncatedPEM("RSA PRIVATE KEY", keyLineA, keyLineB), secrets: true, want: 1,
			gone: []string{"BEGIN RSA PRIVATE KEY", keyLineA, keyLineB},
		},
		{
			name: "ec with short tail", in: truncatedPEM("EC PRIVATE KEY", keyLineA, keyTail), secrets: true, want: 1,
			gone: []string{"BEGIN EC PRIVATE KEY", keyLineA, keyTail},
		},
		{
			name: "openssh", in: truncatedPEM("OPENSSH PRIVATE KEY", keyLineA, keyLineB), secrets: true, want: 1,
			gone: []string{"BEGIN OPENSSH PRIVATE KEY", keyLineA},
		},
		{
			name: "pgp block", in: truncatedPEM("PGP PRIVATE KEY BLOCK", keyLineA, keyLineB), secrets: true, want: 1,
			gone: []string{"BEGIN PGP PRIVATE KEY BLOCK", keyLineA},
		},
		{
			name: "encrypted", in: truncatedPEM("ENCRYPTED PRIVATE KEY", keyLineA), secrets: true, want: 1,
			gone: []string{"BEGIN ENCRYPTED PRIVATE KEY", keyLineA},
		},
		{
			// 完整块的既有行为不变：它仍然是一个值，且优先于截断规则。
			name: "complete block kept whole", in: pemBlock("PRIVATE KEY", keyLineA, keyLineB), secrets: true, want: 1,
			gone: []string{"BEGIN PRIVATE KEY", "END PRIVATE KEY", keyLineA, keyLineB},
		},
		{
			name: "header only inline", in: "The PEM header is -----BEGIN PRIVATE KEY----- in this document.", secrets: true, want: 0,
			kept: []string{"BEGIN PRIVATE KEY"},
		},
		{
			name: "header then prose", in: "-----BEGIN PRIVATE KEY-----\nThis is how a key starts, not key data.\n", secrets: true, want: 0,
			kept: []string{"BEGIN PRIVATE KEY", "This is how a key starts"},
		},
		{
			name: "body only", in: keyLineA + "\n" + keyLineB, secrets: true, want: 0,
			kept: []string{keyLineA},
		},
		{
			name: "public key", in: "-----BEGIN PUBLIC KEY-----\n" + keyLineA + "\n" + keyLineB, secrets: true, want: 0,
			kept: []string{"BEGIN PUBLIC KEY", keyLineA},
		},
		{
			name: "first line truncated at end", in: "-----BEGIN PRIVATE KEY-----\n" + strings.Repeat("a", 20), secrets: true, want: 1,
			gone: []string{"BEGIN PRIVATE KEY", strings.Repeat("a", 20)},
		},
		{
			name: "too short a body", in: "-----BEGIN PRIVATE KEY-----\n" + strings.Repeat("a", 15), secrets: true, want: 0,
			kept: []string{"BEGIN PRIVATE KEY"},
		},
		{
			// 20 个 base64 字符后还有普通日志，无法确认它是密钥片段，
			// 宁可留给用户看，也不把日志吞掉。
			name: "short fragment then log", in: "-----BEGIN PRIVATE KEY-----\n" + strings.Repeat("a", 20) + "\n[INFO] started", secrets: true, want: 0,
			kept: []string{strings.Repeat("a", 20), "[INFO] started"},
		},
		{
			name: "secrets off", in: truncatedPEM("PRIVATE KEY", keyLineA, keyLineB), secrets: false, want: 0,
			kept: []string{"BEGIN PRIVATE KEY", keyLineA},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Forget()
			defer Forget()
			out, n := Mask(c.in, Options{Secrets: c.secrets})
			if n != c.want {
				t.Fatalf("%d masked, want %d: %q", n, c.want, out)
			}
			for _, g := range c.gone {
				if strings.Contains(unmarked(out), g) {
					t.Errorf("%q still in %q", g, out)
				}
			}
			for _, k := range c.kept {
				if !strings.Contains(out, k) {
					t.Errorf("%q gone from %q", k, out)
				}
			}
			if back := Restore(out, false); back != c.in {
				t.Errorf("round trip:\n%q\n→ %q\n→ %q", c.in, out, back)
			}
		})
	}
}

// 截断的正文之后若还有普通日志或源码，规则只取可确认的密钥行，
// 不把后面的行一起吞掉；短尾行之后的下一个普通行也保留。
func TestTruncatedPrivateKeyStopsAtLogOrSource(t *testing.T) {
	Forget()
	defer Forget()
	cases := []struct {
		name string
		in   string
	}{
		{
			name: "log after key",
			in: "read key:\n" + truncatedPEM("RSA PRIVATE KEY", keyLineA, keyLineB) +
				"\n[INFO] gateway started\n2026-11-18T12:00:00Z loaded\n",
		},
		{
			name: "short tail then log",
			in: truncatedPEM("EC PRIVATE KEY", keyLineA, keyTail) +
				"\n[WARN] rotated\n",
		},
		{
			name: "source after key",
			in: "const manifest = `\n" + truncatedPEM("OPENSSH PRIVATE KEY", keyLineA) +
				"\nfunc main() {\n\tprintln(\"ok\")\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Forget()
			defer Forget()
			out, n := Mask(c.in, Options{Secrets: true})
			if n != 1 {
				t.Fatalf("%d masked, want 1: %q", n, out)
			}
			for _, marker := range []string{"[INFO] gateway started", "2026-11-18T12:00:00Z loaded", "[WARN] rotated", "func main()", "\tprintln(\"ok\")"} {
				if strings.Contains(c.in, marker) && !strings.Contains(out, marker) {
					t.Errorf("%q swallowed: %q", marker, out)
				}
			}
			if back := Restore(out, false); back != c.in {
				t.Errorf("round trip changed text: %q", back)
			}
		})
	}
}

// 工具输出来自 JSON 请求时，私钥正文里的换行在请求体中是 \n 两个字符。
// 遮蔽后请求仍是合法 JSON，换回后命令逐字节恢复。
func TestTruncatedPrivateKeyInRequestJSON(t *testing.T) {
	Forget()
	defer Forget()
	key := truncatedPEM("RSA PRIVATE KEY", keyLineA, keyLineB)
	req, err := json.Marshal(map[string]any{
		"model": "test",
		"messages": []any{
			map[string]any{"role": "user", "content": "tool output:\n" + key},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, n := MaskJSON(req, Options{Secrets: true})
	if n != 1 {
		t.Fatalf("%d masked, want 1: %s", n, out)
	}
	if bytes.Contains(out, []byte("BEGIN RSA PRIVATE KEY")) || bytes.Contains(out, []byte(keyLineA)) {
		t.Fatalf("key went through: %s", out)
	}
	if !bytes.Contains(out, []byte("tool output:")) {
		t.Fatalf("surrounding text gone: %s", out)
	}
	back := RestoreJSON(out)
	var want, got struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(req, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(back, &got); err != nil {
		t.Fatalf("restored body no longer JSON: %v: %s", err, back)
	}
	if got.Messages[0].Content != want.Messages[0].Content {
		t.Fatalf("restored:\n%q\nwant:\n%q", got.Messages[0].Content, want.Messages[0].Content)
	}
}

// 工具调用参数里的私钥也一样：占位符不破坏参数 JSON，换回后原命令不变。
func TestTruncatedPrivateKeyInToolArguments(t *testing.T) {
	Forget()
	defer Forget()
	key := truncatedPEM("OPENSSH PRIVATE KEY", keyLineA, keyLineB)
	command := "cat > id_ed25519 <<'EOF'\n" + key + "\nEOF\ngit status"
	inner, _ := json.Marshal(map[string]string{"command": command})
	in := chatWithCall(string(inner))
	out, n := MaskJSON(in, Options{Secrets: true})
	if n != 1 {
		t.Fatalf("%d masked, want 1: %s", n, out)
	}
	sent := argsOf(t, out)[0]
	if !json.Valid([]byte(sent)) {
		t.Fatalf("arguments no longer JSON: %s", sent)
	}
	if strings.Contains(sent, "BEGIN OPENSSH") || strings.Contains(sent, keyLineA) {
		t.Fatalf("key went through: %s", sent)
	}
	back := argsOf(t, RestoreJSON(out))[0]
	var v struct{ Command string }
	if err := json.Unmarshal([]byte(back), &v); err != nil {
		t.Fatalf("restored arguments no longer JSON: %v: %s", err, back)
	}
	if v.Command != command {
		t.Fatalf("restored:\n%q\nwant:\n%q", v.Command, command)
	}
}

// 请求归档用 Scrub 永久抹掉密钥，不保留换回用的值；截断块同样覆盖。
func TestScrubTruncatedPrivateKey(t *testing.T) {
	Forget()
	defer Forget()
	in := truncatedPEM("PRIVATE KEY", keyLineA, keyTail)
	out := Scrub(in)
	if strings.Contains(out, "BEGIN PRIVATE KEY") || strings.Contains(out, keyLineA) || strings.Contains(out, keyTail) {
		t.Fatalf("key left in scrubbed text: %q", out)
	}
	if !strings.Contains(out, scrubbed("PRIVATE_KEY", "")) {
		t.Fatalf("no redaction marker: %q", out)
	}
}

// CRLF 与同文多个截断块都覆盖；换回保持原字节。
func TestTruncatedPrivateKeyCRLFAndSeveral(t *testing.T) {
	Forget()
	defer Forget()
	crlf := strings.ReplaceAll(truncatedPEM("PRIVATE KEY", keyLineA, keyTail), "\n", "\r\n")
	two := truncatedPEM("PRIVATE KEY", keyLineA) + "\nbetween\n" + truncatedPEM("RSA PRIVATE KEY", keyLineB, keyTail)
	for _, in := range []string{crlf, two} {
		Forget()
		out, n := Mask(in, Options{Secrets: true})
		if n != 2 && in == two || n != 1 && in == crlf {
			t.Fatalf("%d masked: %q", n, out)
		}
		if strings.Contains(out, keyLineA) || strings.Contains(out, keyLineB) || strings.Contains(out, keyTail) {
			t.Fatalf("key body still there: %q", out)
		}
		if back := Restore(out, false); back != in {
			t.Fatalf("round trip:\n%q\n→ %q", in, back)
		}
	}
}
