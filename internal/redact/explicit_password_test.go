package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitPasswordText(t *testing.T) {
	o := Options{Secrets: true}
	for _, tc := range []struct {
		name, text, value string
	}{
		{"dotenv", "DB_PASSWORD=mockvioletfern", "mockvioletfern"},
		{"yaml", "  password: 'mock-cedar-willow'", "mock-cedar-willow"},
		{"dotenv shorthand", "DB_PASS=mockhazelgrove", "mockhazelgrove"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, n := Mask(tc.text, o)
			if n != 1 || strings.Contains(out, tc.value) || !strings.Contains(out, "{{SECRET_") {
				t.Errorf("明确标记的虚构口令应完整脱敏，实际命中 %d", n)
			}
			if Restore(out, false) != tc.text {
				t.Error("脱敏后应能完整还原原文")
			}
			b, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": tc.text}}})
			if err != nil {
				t.Fatal(err)
			}
			masked, n := MaskJSON(b, o)
			if n != 1 || strings.Contains(string(masked), tc.value) {
				t.Errorf("网关 JSON 的文本内容也应脱敏，实际命中 %d", n)
			}
		})
	}
}

func TestExplicitPasswordKeepsNonCredentials(t *testing.T) {
	for _, in := range []string{
		`DB_PASSWORD=${APP_PASSWORD}`,
		`DB_PASS = process.env.DB_PASS`,
		`PASSWORD=readPassword()`,
		`password_path=/srv/test/password-file`,
		`const passwordPolicy = requireLetters`,
		`token: sample-package`,
		`secret: example-module`,
		`PASSWORD= UI_ADDR=localhost`,
	} {
		out, n := Mask(in, Options{Secrets: true})
		if n != 0 || out != in {
			t.Errorf("非凭据不应被改写：%q（命中 %d）", in, n)
		}
	}
	const in = "DB_PASSWORD=mockplumtree"
	if out, n := Mask(in, Options{}); n != 0 || out != in {
		t.Error("关闭密钥脱敏时不应改写")
	}
}
