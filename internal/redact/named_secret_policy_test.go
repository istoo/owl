package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// A value masked because its field is named for a secret goes through the
// same policy as one a rule finds: Seen is told of it (so the gateway
// keeps the masked body instead of sending the original), Block holds the
// request back, and the user's Allow lets it go as written (#52).
func TestMaskJSONNamedSecretFollowsThePolicy(t *testing.T) {
	const secret = "my-secret-value"
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"login","input":{"password":"` + secret + `"}}]}]}`)

	var hits []Hit
	out, n := MaskJSON(body, Options{Secrets: true, Seen: func(h Hit) { hits = append(hits, h) }})
	if n != 1 || len(hits) != 1 || hits[0].Kind != "SECRET" || hits[0].Action != "" {
		t.Fatalf("masked %d, hits %+v", n, hits)
	}
	if strings.Contains(string(out), secret) || !strings.Contains(string(out), "{{SECRET_") || !json.Valid(out) {
		t.Fatalf("a value named for a secret went out: %s", out)
	}

	// the gateway holds the request back on a Block, and the value is told
	hits = nil
	out, n = MaskJSON(body, Options{Secrets: true, Actions: map[string]string{"SECRET": Block},
		Seen: func(h Hit) { hits = append(hits, h) }})
	if n != 1 || len(hits) != 1 || hits[0].Action != Block {
		t.Fatalf("block: masked %d, hits %+v", n, hits)
	}
	if strings.Contains(string(out), secret) || !json.Valid(out) {
		t.Fatalf("block sent the value: %s", out)
	}

	// the user's Allow wins, and nothing is told
	hits = nil
	out, n = MaskJSON(body, Options{Secrets: true, Allow: []string{secret},
		Seen: func(h Hit) { hits = append(hits, h) }})
	if n != 0 || len(hits) != 0 || string(out) != string(body) {
		t.Fatalf("allow: masked %d, hits %+v, out %s", n, hits, out)
	}
}

// A value masked before is recalled where the arguments hold it JSON
// escaped: a newline, a quote and a backslash in it read as they are
// written, and knownSpans reading the unescaped view misses all three.
// The user's Allow reads the same unescaped value, so it matches too
// (#58).
func TestMaskJSONRecallsEscapedKnownSecret(t *testing.T) {
	Forget()
	t.Cleanup(Forget)
	const secret = "aa\"bb\ncc\\dd" // a quote, a newline and a backslash
	want := placeholder("SECRET", secret)
	inner, err := json.Marshal(map[string]string{"cmd": "DB_PASSWORD=" + secret})
	if err != nil {
		t.Fatal(err)
	}
	body := chatWithCall(string(inner))

	out, n := MaskJSON(body, Options{Secrets: true})
	if n != 1 {
		t.Fatalf("masked %d values, want 1: %s", n, out)
	}
	args := argsOf(t, out)[0]
	if !json.Valid([]byte(args)) {
		t.Fatalf("arguments no longer JSON: %s", args)
	}
	if !strings.Contains(args, want) {
		t.Fatalf("the escaped value wasn't recalled as its placeholder: %s", args)
	}
	back := RestoreJSON(out)
	var got struct{ Cmd string }
	if err := json.Unmarshal([]byte(argsOf(t, back)[0]), &got); err != nil {
		t.Fatalf("restored arguments: %v: %s", err, argsOf(t, back)[0])
	}
	if got.Cmd != "DB_PASSWORD="+secret {
		t.Fatalf("restored %q, want %q", got.Cmd, "DB_PASSWORD="+secret)
	}

	// the allow list names the value itself, not the way the arguments
	// write it escaped
	out, n = MaskJSON(body, Options{Secrets: true, Allow: []string{secret}})
	if n != 0 || string(out) != string(body) {
		t.Fatalf("allow: masked %d, out %s", n, out)
	}
}
