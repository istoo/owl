package redact

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A named secret in a tool call goes through the rules and the user's words
// first: a kind a rule knows keeps its own Action (an API_KEY set to block
// is blocked, though SECRET is set to warn), and a value the user allows
// still has the words they mask masked (H36).
func TestNamedSecretKeepsTheRulesAction(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"deploy","input":{"api_key":"AKIAIOSFODNN7EXAMPLE"}}]}]}`)
	var hits []Hit
	out, n := MaskJSON(body, Options{
		Secrets: true,
		Actions: map[string]string{"API_KEY": Block, "SECRET": Warn},
		Seen:    func(h Hit) { hits = append(hits, h) },
	})
	if strings.Contains(string(out), "AKIAIOSFODNN7EXAMPLE") || n != 1 {
		t.Fatalf("%d masked, the key left: %s", n, out)
	}
	if len(hits) != 1 || hits[0].Kind != "API_KEY" || hits[0].Action != Block {
		t.Fatalf("hits %+v: the rules' kind and action should govern, not SECRET's", hits)
	}
	if !json.Valid(out) {
		t.Fatalf("no JSON: %s", out)
	}
}

// The words the user masks win over Allow: a value on the allow list with
// one of their words in it is still masked by that word, and not masked a
// second time by the field's name (H36).
func TestNamedSecretWordsWinOverAllow(t *testing.T) {
	const value = "OtterKelvarn99"
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"deploy","input":{"password":"` + value + `"}}]}]}`)

	// allowed alone: as written
	if out, n := MaskJSON(body, Options{Secrets: true, Allow: []string{value}}); n != 0 || string(out) != string(body) {
		t.Fatalf("an allowed value went masked: %d %s", n, out)
	}
	// a word inside it is masked all the same, and only once
	var hits []Hit
	out, n := MaskJSON(body, Options{Secrets: true, Allow: []string{value}, Words: []string{"Kelvarn"}, Seen: func(h Hit) { hits = append(hits, h) }})
	if strings.Contains(string(out), "Kelvarn") || n != 1 || len(hits) != 1 || hits[0].Kind != "TERM" {
		t.Fatalf("%d masked, hits %+v: %s", n, hits, out)
	}
	if !json.Valid(out) {
		t.Fatalf("no JSON: %s", out)
	}
}

// A placeholder already in a named field is left alone: masked again by the
// name, it would nest one placeholder inside another (H36).
func TestNamedSecretPlaceholderNotMaskedAgain(t *testing.T) {
	const value = "Qm27wVb9nLx4tRk8Ps3d" // no rule knows it: masked by its field's name
	body := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"deploy","input":{"api_key":"` + value + `"}}]}]}`)
	first, n := MaskJSON(body, Options{Secrets: true})
	if n != 1 || strings.Contains(string(first), value) || !HasPlaceholder(string(first)) {
		t.Fatalf("first pass %d: %s", n, first)
	}
	again, n := MaskJSON(first, Options{Secrets: true})
	if n != 0 || string(again) != string(first) {
		t.Fatalf("replayed placeholder masked again: %d\n%s\n%s", n, first, again)
	}
}

// A \uXXXX in a tool call's arguments is read as the letter it stands for:
// a Chinese word written that way is masked, and so is a key whose & or p
// is spelled as an escape, while the arguments stay JSON and restore (H37).
func TestToolArgsUnicodeEscapesMasked(t *testing.T) {
	withCall := func(input string) []byte {
		return []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"deploy","input":` + input + `}]}]}`)
	}
	// 张三 written as 张三
	body := withCall(`{"note":"张三"}`)
	out, n := MaskJSON(body, Options{Words: []string{"张三"}})
	if n != 1 || strings.Contains(string(out), "张三") || !HasPlaceholder(string(out)) {
		t.Fatalf("Chinese word in escapes: %d %s", n, out)
	}
	if !json.Valid(out) {
		t.Fatalf("no JSON: %s", out)
	}
	if back := RestoreJSON(out); !strings.Contains(string(back), "张三") {
		t.Fatalf("not restored: %s", back)
	}

	// an sk- key with its & and a p written as \u escapes
	rule := []Rule{{Kind: "MYKEY", Regex: `sk-proj-[A-Za-z0-9&]{8,}`}}
	for _, input := range []string{
		`{"api_key":"sk-proj-AbC&dEf123456"}`,
		`{"api_key":"sk-proj-pQrS123456"}`,
	} {
		out, n := MaskJSON(withCall(input), Options{Secrets: true, Rules: rule})
		if n != 1 || strings.Contains(string(out), "sk-proj-") || !HasPlaceholder(string(out)) {
			t.Fatalf("%s: %d masked: %s", input, n, out)
		}
		if !json.Valid(out) {
			t.Fatalf("%s: no JSON: %s", input, out)
		}
	}
}

// Pieces of two tool calls in one stream are never one text: a placeholder
// split across the two is left split, not put back in the second call
// (H38).
func TestStreamParallelToolCallsKeepTheirTails(t *testing.T) {
	const phone = "13922223333"
	p := phonePlaceholder(t, phone)
	chunk := func(index int, args string) string {
		// choices[0].index comes before the call's own, as OpenAI writes
		// it: reading only the first index read 0 for every call.
		a, _ := json.Marshal(args)
		return `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":` +
			strconv.Itoa(index) + `,"id":"call_` + string(rune('a'+index)) + `","function":{"name":"deploy","arguments":` + string(a) + `}}]}}]}` + "\n\n"
	}
	// the first call ends just before the placeholder's last two braces, so
	// its tail may be the start of one; the second call begins with them.
	// Joined across the calls they are a whole placeholder, and its value
	// would be put back in the second call's arguments.
	events := []string{
		chunk(0, `{"a":"`+p[:len(p)-2]),
		chunk(1, `}}`+`"}`),
		"data: [DONE]\n\n",
	}
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/event-stream")
	w := NewWriter(rec)
	w.WriteHeader(200)
	for _, e := range events {
		if _, err := w.Write([]byte(e)); err != nil {
			t.Fatal(err)
		}
	}
	w.Finish()
	// the pieces of each call, put together
	args := map[int]string{}
	for _, ev := range strings.Split(rec.Body.String(), "\n\n") {
		d, ok := strings.CutPrefix(ev, "data: ")
		if !ok || d == "[DONE]" {
			continue
		}
		var e struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index    int `json:"index"`
						Function struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(d), &e); err != nil {
			t.Fatalf("event %q: %v", d, err)
		}
		for _, c := range e.Choices[0].Delta.ToolCalls {
			args[c.Index] += c.Function.Arguments
		}
	}
	// tool 0's body without its braces is still put back, alone; tool 1's
	// two braces stay its own, and its value never comes from tool 0
	if want := `{"a":"` + phone; args[0] != want {
		t.Fatalf("tool 0 arguments %q, want %q", args[0], want)
	}
	if want := `}}"}`; args[1] != want {
		t.Fatalf("tool 1 arguments %q, want %q: tool 0's tail was carried into it", args[1], want)
	}
}

// A named secret's whole value is covered: a rule's match in part of it
// doesn't leave the rest in clear, and a placeholder already in it is left
// whole instead of being nested in another placeholder (H36).
func TestNamedSecretCoversTheWholeValue(t *testing.T) {
	withCall := func(value string) []byte {
		return []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"deploy","input":{"password":"` + value + `"}}]}]}`)
	}

	// a rule finds the key; the rest still goes by the field's name
	key := "sk-ant-api03-" + strings.Repeat("a", 40)
	out, n := MaskJSON(withCall("acct "+key+" then hunter2Secret99"), Options{Secrets: true})
	if strings.Contains(string(out), "hunter2Secret99") || strings.Contains(string(out), "sk-ant-api03-") {
		t.Fatalf("the rest of a named field went as written: %s", out)
	}
	if n != 2 || !json.Valid(out) {
		t.Fatalf("%d masked, JSON %v: %s", n, json.Valid(out), out)
	}

	// a placeholder in the value stays whole, and the secret beside it is
	// masked: no placeholder inside another
	const ph = "{{SECRET_abcdabcd}}"
	out, n = MaskJSON(withCall("keep-"+ph+"-hunter2Secret99"), Options{Secrets: true})
	if strings.Count(string(out), ph) != 1 || strings.Contains(string(out), "hunter2Secret99") {
		t.Fatalf("a placeholder in a named field was nested or the secret left: %s", out)
	}
	if n != 1 || !json.Valid(out) {
		t.Fatalf("%d masked, JSON %v: %s", n, json.Valid(out), out)
	}
}

// In text the vendor signed, a user's whole-word value is replaced only as
// a whole word: the same letters inside a longer one are not it, and
// replacing them broke the signature (M40d).
func TestSignedRemaskHonoursWordsWhole(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"ACME ships"},{"role":"assistant","content":[{"type":"thinking","thinking":"XACMEY and ACME here","signature":"Eqk1"}]}]}`)
	for _, whole := range []bool{true, false} {
		out, n := MaskJSON(body, Options{Words: []string{"ACME"}, WordsWhole: whole})
		if !json.Valid(out) {
			t.Fatalf("whole %v: no JSON: %s", whole, out)
		}
		gotLonger := strings.Contains(string(out), "XACMEY")
		if whole && !gotLonger {
			t.Fatalf("whole words on: a word inside a longer one was masked: %s", out)
		}
		if !whole && gotLonger {
			t.Fatalf("whole words off: a word inside a longer one was left: %s", out)
		}
		if !strings.Contains(string(out), "{{TERM_") {
			t.Fatalf("whole %v: the standalone word wasn't masked: %s", whole, out)
		}
		if n == 0 {
			t.Fatalf("whole %v: nothing counted: %s", whole, out)
		}
	}
}
