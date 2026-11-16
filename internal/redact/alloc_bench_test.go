package redact

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// benchBody is a Claude Code-shaped Messages request of about target
// bytes: a long system prompt and tool schemas, turns with a tool call and
// its result, a thinking block with a signature, and one base64 image.
// The text is JSON-escaped (\n, \"), as an agent sends it, and some turns
// carry a secret and an address the rules mask.
func benchBody(target int) []byte {
	var b strings.Builder
	b.WriteString(`{"model":"claude-sonnet-4-5","max_tokens":32000,"stream":true,"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude. `)
	b.WriteString(strings.Repeat(`Read before you write. Check the caller. Keep it short.\n`, 900))
	b.WriteString(`"}],"tools":[`)
	for i := range 8 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"Tool%d","description":"Does a thing for the user, with care.\nIt checks first.","input_schema":{"type":"object","properties":{"path":{"type":"string","description":"The file to read, unmodified"},"limit":{"type":"integer"},"opts":{"type":"object","properties":{"deep":{"type":"boolean"},"tags":{"type":"array","items":{"type":"string"}}}}},"required":["path"]}}`, i)
	}
	b.WriteString(`],"messages":[{"role":"user","content":[{"type":"text","text":"look at this screenshot and the files"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`)
	b.WriteString(strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 4096)) // ~100 KB
	b.WriteString(`"}}]}`)
	page := strings.Repeat(`line of a file's contents, with "quotes" and a tab\there\n`, 40)
	turn := 0
	for b.Len() < target {
		fmt.Fprintf(&b, `,{"role":"assistant","content":[{"type":"thinking","thinking":"The user wants me to read the files. %s","signature":"EqQBCkgIBRABGAIiQ%s"},{"type":"text","text":"I'll read them now."},{"type":"tool_use","id":"toolu_%04d","name":"Tool0","input":{"path":"/Users/dev/project/file%d.go","limit":2000,"opts":{"deep":true,"tags":["a","b"]},"api_key":"%s"}}]}`,
			strings.Repeat("Let me check the caller first. ", 20), strings.Repeat("A1b2C3d4E5f6", 60), turn, turn, fixtureKey)
		fmt.Fprintf(&b, `,{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%04d","content":"%s"},{"type":"text","text":"the key is %s, mail bob%04d@example.org, call 13800138000"}]}`,
			turn, strings.ReplaceAll(page, `"`, `\"`), fixtureKey, turn)
		turn++
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

const fixtureKey = "sk-proj-abcdEFGH1234ijklMNOP5678qrst"

// A value masked in a string JSON escaped (\n in a tool call's arguments)
// is recalled in a sealed block as well: the walk hands a value in one
// buffer for all of them, so what the signed pass remembers has to be a
// copy of its own.
func TestMaskJSONRecallsAValueFromAnEscapedString(t *testing.T) {
	const secret = "Hr7jTm2qwXz94qLm4nZ8"
	o := Options{Secrets: true}
	body := `{"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"I set DB_PASSWORD=` + secret + `\nand ran it"},` +
		`{"type":"tool_use","id":"toolu_1","name":"run","input":{"cmd":"DB_PASSWORD=` + secret + `\nrun it"}}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"the password ` + secret + ` again\n","signature":"Eqk1"}]}]}`
	out, _ := MaskJSON([]byte(body), o)
	if strings.Contains(string(out), secret) {
		t.Fatalf("a value went out, or was not recalled in the sealed text:\n%s", out)
	}
	if !strings.Contains(string(out), "the password {{SECRET_") {
		t.Fatalf("the sealed text didn't get the placeholder the value went as:\n%s", out)
	}
	if !json.Valid(out) {
		t.Fatalf("not JSON: %s", out)
	}
}

// benchBodies are the three sizes in order: what masking remembers of a
// long text carries from one size to the next, so a walk over a map's keys
// measured a size after another that shared its system prompt.
func benchBodies(b *testing.B) []struct {
	name string
	body []byte
} {
	b.Helper()
	var out []struct {
		name string
		body []byte
	}
	for _, c := range []struct {
		name string
		n    int
	}{{"200KB", 200 << 10}, {"1MB", 1 << 20}, {"4MB", 4 << 20}} {
		body := benchBody(c.n)
		if !json.Valid(body) {
			var v any
			err := json.Unmarshal(body, &v)
			off := int64(0)
			if se, ok := err.(*json.SyntaxError); ok {
				off = se.Offset
			}
			lo := max(off-120, 0)
			hi := min(off+120, int64(len(body)))
			b.Fatalf("%s fixture is not JSON (%d bytes): %v at %d: %q", c.name, len(body), err, off, body[lo:hi])
		}
		out = append(out, struct {
			name string
			body []byte
		}{c.name, body})
	}
	return out
}

func benchMask(tb testing.TB, body []byte, o Options) {
	tb.Helper()
	out, n := MaskJSON(body, o)
	if len(out) < len(body)/2 || n == 0 {
		tb.Fatalf("masked %d of %d bytes, %d values", len(out), len(body), n)
	}
}

// BenchmarkMaskJSONRules measures a request masked by the rules alone, the
// model off.
func BenchmarkMaskJSONRules(b *testing.B) {
	for _, c := range benchBodies(b) {
		name, body := c.name, c.body
		o := Options{Secrets: true, Personal: true}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				benchMask(b, body, o)
			}
		})
	}
}

// BenchmarkMaskJSONModel measures a request with the local model on. The
// stub finds nothing: what it adds is the pass that hands every text to
// the model and keeps it for the pass that masks.
func BenchmarkMaskJSONModel(b *testing.B) {
	nothing := func(texts []string) [][]Found {
		out := make([][]Found, len(texts))
		return out
	}
	for _, c := range benchBodies(b) {
		name, body := c.name, c.body
		o := Options{Secrets: true, Personal: true, Model: nothing}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				benchMask(b, body, o)
			}
		})
	}
}

// BenchmarkMaskJSONStages breaks down where a masked request's allocations
// go: the structural scan, a walk that reads and unescapes every string
// without changing one, and the whole mask.
func BenchmarkMaskJSONStages(b *testing.B) {
	body := benchBody(1 << 20)
	stages := map[string]func(){
		"scan": func() { scan(body) },
		"walk-noop": func() {
			walk(body, func(path []byte, key, s string) string { return s })
		},
		"walk-roots-none": func() {
			walkRoots(body, []string{"tools"}, func(path []byte, key, s string) string { return s })
		},
		"mask": func() {
			benchMask(b, body, Options{Secrets: true, Personal: true})
		},
	}
	for name, f := range stages {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				f()
			}
		})
	}
}

// BenchmarkWalkMaskedStrings is the walk's own cost over the strings of a
// request that are all changed, so every value writes to the output.
func BenchmarkWalkMaskedStrings(b *testing.B) {
	var s strings.Builder
	s.WriteString(`[`)
	for i := range 4000 {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteString(`"a line of text, with \"quotes\" and\na break"`)
	}
	s.WriteString(`]`)
	doc := []byte(s.String())
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for b.Loop() {
		out, ok := walk(doc, func(path []byte, key, s string) string {
			if strings.Contains(s, "line") {
				return strings.ReplaceAll(s, "line", "row")
			}
			return s
		})
		if !ok || len(out) == len(doc) {
			b.Fatalf("walk: %v %d", ok, len(out))
		}
	}
}

// BenchmarkMaskJSONManyStrings measures a request of many short escaped
// strings with nothing to mask: what the walk's one buffer saves.
func BenchmarkMaskJSONManyStrings(b *testing.B) {
	body := manyStringsBody(4000)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		out, _ := MaskJSON(body, Options{Secrets: true, Personal: true})
		if len(out) < len(body)/2 {
			b.Fatal("short")
		}
	}
}
