package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

// allocBenchSecret and allocBenchMail are a key and an address a tool
// output carries, as an agent's file read does: with redaction on, the
// masker walks every string of the body for them, and the body every
// reader decodes below is the masked one, which is not the body the
// request was read as.
const (
	allocBenchSecret = "sk-proj-allocbench0123456789"
	allocBenchMail   = "someone@example.org"
)

var allocBenchImage = func() string {
	var b bytes.Buffer
	enc := base64.NewEncoder(base64.StdEncoding, &b)
	png := []byte("\x89PNG\r\n\x1a\n")
	for len(png) < 24<<10 {
		png = append(png, byte(len(png)), byte(len(png)>>8), byte(len(png)>>16), 0x7f)
	}
	enc.Write(png)
	enc.Close()
	return b.String()
}()

// allocConversation is a conversation as an agent sends it, in proto, of
// about target bytes: a system prompt and tools, then turns of a user's
// words (with an inline picture every fourth turn), the assistant's
// thinking and a tool call, and a tool result of a file's contents. The
// shapes are the ones each API is written in.
func allocConversation(proto provider.Protocol, model string, target int, stream bool) []byte {
	para := strings.Repeat("The function reads the file, splits it on newlines and returns the lines that match. ", 8)
	system := "You are a coding agent. " + strings.Repeat("Work carefully and read before you write. ", 80)
	result := strings.Repeat("line of code with \"quotes\" and unicode é 漢字\n", 40)
	type schema = map[string]any
	var tools []schema
	for i := range 40 {
		props := schema{"path": schema{"type": "string", "description": "Absolute path to file " + fmt.Sprint(i)}, "limit": schema{"type": "integer"}}
		switch proto {
		case provider.Anthropic:
			tools = append(tools, schema{"name": fmt.Sprintf("Tool%d", i), "description": para[:240], "input_schema": schema{"type": "object", "properties": props, "required": []string{"path"}}})
		default:
			tools = append(tools, schema{"type": "function", "name": fmt.Sprintf("Tool%d", i), "description": para[:240], "parameters": schema{"type": "object", "properties": props, "required": []string{"path"}}})
		}
	}
	var msgs []any
	n := len(system)
	for turn := 0; n < target; turn++ {
		id := fmt.Sprintf("call_%04d", turn)
		args := fmt.Sprintf(`{"path":"/src/pkg/file%d.go","limit":200,"api_key":"%s"}`, turn, allocBenchSecret)
		withImage := turn%4 == 0
		// what the turn adds to the marshalled body: the result's newlines
		// are escaped, so it is a little longer than itself, and an image is
		// its base64 every fourth turn
		added := len(result) + len(result)/16 + len(para)*2 + 400
		if withImage {
			added += len(allocBenchImage)
		}
		switch proto {
		case provider.Anthropic:
			uc := []any{schema{"type": "text", "text": fmt.Sprintf("read file %d, then tell me what it does", turn)}}
			if withImage {
				uc = append(uc, schema{"type": "image", "source": schema{"type": "base64", "media_type": "image/png", "data": allocBenchImage}})
			}
			msgs = append(msgs,
				schema{"role": "user", "content": uc},
				schema{"role": "assistant", "content": []any{
					schema{"type": "thinking", "thinking": para, "signature": fmt.Sprintf("sig-%04d-allocbench", turn)},
					schema{"type": "text", "text": para},
					schema{"type": "tool_use", "id": id, "name": "Tool1", "input": json.RawMessage(args)},
				}},
				schema{"role": "user", "content": []any{
					schema{"type": "tool_result", "tool_use_id": id, "content": result},
					schema{"type": "text", "text": "the key is " + allocBenchSecret + ", mail " + allocBenchMail},
				}})
			n += added
		case provider.Chat:
			uc := any(fmt.Sprintf("read file %d, then tell me what it does", turn))
			if withImage {
				uc = []any{
					schema{"type": "text", "text": fmt.Sprintf("read file %d, then tell me what it does", turn)},
					schema{"type": "image_url", "image_url": schema{"url": "data:image/png;base64," + allocBenchImage}},
				}
			}
			msgs = append(msgs,
				schema{"role": "user", "content": uc},
				schema{"role": "assistant", "content": para, "reasoning_content": para,
					"tool_calls": []any{schema{"id": id, "type": "function", "function": schema{"name": "Tool1", "arguments": args}}}},
				schema{"role": "tool", "tool_call_id": id, "content": result},
				schema{"role": "user", "content": "the key is " + allocBenchSecret + ", mail " + allocBenchMail})
			n += added
		default:
			items := []any{
				schema{"type": "message", "role": "user", "content": []any{schema{"type": "input_text", "text": fmt.Sprintf("read file %d, then tell me what it does", turn)}}},
				schema{"type": "reasoning", "summary": []any{}, "encrypted_content": fmt.Sprintf("gAAAAA-allocbench-%04d", turn)},
				schema{"type": "message", "role": "assistant", "content": []any{schema{"type": "output_text", "text": para}}},
				schema{"type": "function_call", "call_id": id, "name": "Tool1", "arguments": args},
				schema{"type": "function_call_output", "call_id": id, "output": result},
				schema{"type": "message", "role": "user", "content": []any{schema{"type": "input_text", "text": "the key is " + allocBenchSecret + ", mail " + allocBenchMail}}},
			}
			if withImage {
				items[0] = schema{"type": "message", "role": "user", "content": []any{
					schema{"type": "input_text", "text": fmt.Sprintf("read file %d, then tell me what it does", turn)},
					schema{"type": "input_image", "image_url": "data:image/png;base64," + allocBenchImage},
				}}
			}
			msgs = append(msgs, items...)
			n += added
		}
	}
	body := schema{"model": model, "stream": stream, "tools": tools}
	switch proto {
	case provider.Anthropic:
		body["max_tokens"] = 32000
		body["system"] = []any{schema{"type": "text", "text": system, "cache_control": schema{"type": "ephemeral"}}}
		body["messages"] = msgs
	case provider.Chat:
		body["max_tokens"] = 32000
		body["messages"] = append([]any{schema{"role": "system", "content": system}}, msgs...)
	default:
		body["instructions"] = system
		body["input"] = msgs
	}
	b, _ := json.Marshal(body)
	return b
}

// allocReply is the stream or whole reply of a Chat upstream, which every
// client protocol is translated to in this benchmark; its size is the
// reply's, so what a request allocates is the request's.
func allocReply(stream bool) (string, string) {
	if stream {
		return "text/event-stream", sse(
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
			`data: [DONE]`)
	}
	return "application/json", `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`
}

// BenchmarkGatewayAlloc is what one request through the gateway allocates
// for a conversation of an agent's own shape and size, in each client
// protocol, streamed or whole, masked or not: a masked request is read
// from a body the masker built, which no hold covers, so each of its
// readers has decoded it into copies of its own (fillJSONIn).
func BenchmarkGatewayAlloc(b *testing.B) {
	const (
		small = 100 << 10
		mid   = 1 << 20
		big   = 4 << 20
	)
	for _, tc := range []struct {
		name, path, model string
		proto             provider.Protocol
	}{
		{"anthropic", "/v1/messages", "fake/m1", provider.Anthropic},
		{"chat", "/v1/chat/completions", "fake/m1", provider.Chat},
		{"responses", "/v1/responses", "fake/m1", provider.Responses},
	} {
		for _, size := range []struct {
			name string
			n    int
		}{{"100k", small}, {"1m", mid}, {"4m", big}} {
			for _, stream := range []bool{false, true} {
				for _, masked := range []bool{false, true} {
					name := fmt.Sprintf("%s/%s/%s", tc.name, size.name, map[bool]string{false: "whole", true: "stream"}[stream])
					if masked {
						name += "/masked"
					}
					b.Run(name, func(b *testing.B) {
						home := b.TempDir()
						b.Setenv("HOME", home)
						b.Setenv("XDG_CONFIG_HOME", home)
						b.Setenv("XDG_CACHE_HOME", b.TempDir())
						if masked {
							if err := settings.Save(settings.Settings{Redact: true}); err != nil {
								b.Fatal(err)
							}
						}
						// a translated request (Anthropic or Responses to
						// Chat) is always streamed upstream; a Chat client's
						// own protocol relays whole when it asks whole
						ctype, reply := allocReply(true)
						if tc.proto == provider.Chat && !stream {
							ctype, reply = allocReply(false)
						}
						up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							io.Copy(io.Discard, r.Body)
							w.Header().Set("Content-Type", ctype)
							io.WriteString(w, reply)
						}))
						defer up.Close()
						p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}, Chat: up.URL + "/v1"}
						if err := provider.Save(p); err != nil {
							b.Fatal(err)
						}
						body := allocConversation(tc.proto, tc.model, size.n, stream)
						s := New()
						b.ReportAllocs()
						b.SetBytes(int64(len(body)))
						b.ResetTimer()
						for range b.N {
							ctx, done := context.WithCancel(context.Background())
							rec := httptest.NewRecorder()
							s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", tc.path, bytes.NewReader(body)).WithContext(ctx))
							done()
							if rec.Code != 200 {
								b.Fatalf("%d %s", rec.Code, rec.Body.String())
							}
						}
					})
				}
			}
		}
	}
}
