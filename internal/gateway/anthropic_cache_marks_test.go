package gateway

import (
	"encoding/json"
	"testing"
)

func anthropicBlocks(t *testing.T, r *Request) []map[string]any {
	t.Helper()
	var out struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(buildAnthropic(r, "m"), &out); err != nil {
		t.Fatal(err)
	}
	var blocks []map[string]any
	for _, m := range out.Messages {
		blocks = append(blocks, m.Content...)
	}
	return blocks
}

func markTTL(b map[string]any) string {
	cc, _ := b["cache_control"].(map[string]any)
	ttl, _ := cc["ttl"].(string)
	return ttl
}

// M8: a PDF document a client marked keeps its mark when the request is
// rebuilt, as its text does; so does the words a file that isn't a PDF is
// told by.
func TestAnthropicFileKeepsItsCacheMark(t *testing.T) {
	for _, c := range []struct{ name, media, data string }{
		{"pdf", "application/pdf", "JVBERi0xLjQK"},
		{"text", "text/plain", "aGVsbG8="},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[` +
				`{"type":"document","source":{"type":"base64","media_type":"` + c.media + `","data":"` + c.data + `"},"cache_control":{"type":"ephemeral","ttl":"1h"}},` +
				`{"type":"text","text":"read it"}]}]}`
			r, err := parseAnthropicRequest([]byte(body), true)
			if err != nil {
				t.Fatal(err)
			}
			marked := 0
			for _, b := range anthropicBlocks(t, r) {
				if ttl := markTTL(b); ttl != "" {
					marked++
					if ttl != "1h" {
						t.Fatalf("mark %v", b)
					}
				}
			}
			if marked != 1 {
				t.Fatalf("%d marks, want the file's: %s", marked, buildAnthropic(r, "m"))
			}
		})
	}
}

// M8: a client that marks only its tools keeps those breakpoints; the
// rebuilt tools carry them as they came.
func TestAnthropicToolsKeepTheirCacheMarks(t *testing.T) {
	body := `{"model":"m","max_tokens":64,` +
		`"tools":[{"name":"read","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral","ttl":"1h"}},` +
		`{"name":"write","input_schema":{"type":"object"}}],` +
		`"messages":[{"role":"user","content":"hi"}]}`
	r, err := parseAnthropicRequest([]byte(body), true)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(buildAnthropic(r, "m"), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("tools %v", out.Tools)
	}
	if ttl := markTTL(out.Tools[0]); ttl != "1h" {
		t.Fatalf("the tool's mark is gone: %v", out.Tools[0])
	}
	if _, ok := out.Tools[1]["cache_control"]; ok {
		t.Fatalf("a mark on a tool that had none: %v", out.Tools[1])
	}
}
