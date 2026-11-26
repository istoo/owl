package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

const namedSecretReply = `{"id":"msg_1","type":"message","role":"assistant","model":"m1",` +
	`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// A value no rule knows, under a field named for a secret in a tool
// call's arguments, is masked and told: the gateway keeps the placeholder
// instead of sending the body back as it came (#52).
func TestNamedSecretIsMaskedOnItsOwn(t *testing.T) {
	const secret = "my-secret-value"
	const body = `{"model":"fake/m1","max_tokens":10,"messages":[{"role":"assistant",` +
		`"content":[{"type":"tool_use","id":"toolu_1","name":"login","input":{"password":"` + secret + `"}}]}]}`
	req := func(t *testing.T) {
		t.Helper()
		f := &fake{ctype: "application/json", reply: namedSecretReply}
		setup(t, provider.Anthropic, f)
		if err := settings.Save(settings.Settings{Redact: true}); err != nil {
			t.Fatal(err)
		}
		code, out := post(t, "/v1/messages", body)
		if code != 200 {
			t.Fatalf("request: %d %s", code, out)
		}
		if f.calls != 1 {
			t.Fatalf("upstream calls: %d", f.calls)
		}
		if strings.Contains(string(f.got), secret) || !strings.Contains(string(f.got), "{{SECRET_") {
			t.Fatalf("the value named for a secret went upstream: %s", f.got)
		}
	}
	req(t)

	// the user's allow list lets it go as written
	t.Run("allow", func(t *testing.T) {
		f := &fake{ctype: "application/json", reply: namedSecretReply}
		setup(t, provider.Anthropic, f)
		if err := settings.Save(settings.Settings{Redact: true, RedactAllow: []string{secret}}); err != nil {
			t.Fatal(err)
		}
		if code, out := post(t, "/v1/messages", body); code != 200 {
			t.Fatalf("request: %d %s", code, out)
		}
		if !strings.Contains(string(f.got), secret) || strings.Contains(string(f.got), "{{SECRET_") {
			t.Fatalf("an allowed value was masked: %s", f.got)
		}
	})

	// and a kind set to block holds the request back untouched
	t.Run("block", func(t *testing.T) {
		f := &fake{ctype: "application/json", reply: namedSecretReply}
		setup(t, provider.Anthropic, f)
		if err := settings.Save(settings.Settings{Redact: true, RedactActions: map[string]string{"SECRET": "block"}}); err != nil {
			t.Fatal(err)
		}
		code, out := post(t, "/v1/messages", body)
		if code != 400 {
			t.Fatalf("a blocked request: %d %s", code, out)
		}
		if f.calls != 0 {
			t.Fatalf("a blocked request went upstream %d times", f.calls)
		}
	})
}

// A settings.json that can't be read is unknown, never "mask nothing": the
// masker fails closed with it and the request is refused before it goes
// anywhere (#53).
func TestUnreadableSettingsSendNothing(t *testing.T) {
	f := &fake{ctype: "application/json", reply: namedSecretReply}
	setup(t, provider.Anthropic, f)
	dir := filepath.Dir(settings.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := post(t, "/v1/messages",
		`{"model":"fake/m1","max_tokens":10,"messages":[{"role":"user","content":"write to bob@example.com"}]}`)
	if code != 503 {
		t.Fatalf("an unreadable settings.json answered %d %s, want 503", code, body)
	}
	if f.calls != 0 {
		t.Fatalf("the request went upstream %d times with settings unreadable", f.calls)
	}
}
