package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// copilotChatUp is Copilot's chat API for a key provider: it records every
// body and answers 400 "invalid request body" when refuse says the body is
// one it turns away.
type copilotChatUp struct {
	mu     sync.Mutex
	sent   []string
	refuse func(body string) bool
}

func (u *copilotChatUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.sent = append(u.sent, string(b))
	refuse := u.refuse
	u.mu.Unlock()
	if refuse != nil && refuse(string(b)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":null,"message":"invalid request body","param":null,"type":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"id":"c1","model":"gemini-4-neon","choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`))
}

// toolSearchHistory is Claude Code's ToolSearch call and answer: the tool
// the search loaded is asked for with defer_loading, and the answer names
// it in a tool_reference block and nothing else.
const toolSearchHistory = `"messages":[
 {"role":"user","content":"Message the reviewer."},
 {"role":"assistant","content":[
   {"type":"tool_use","id":"toolu_ts1","name":"ToolSearch","input":{"query":"select:SendMessage"}}]},
 {"role":"user","content":[
   {"type":"tool_result","tool_use_id":"toolu_ts1","content":[{"type":"tool_reference","tool_name":"SendMessage"}]}]}]`

// toolSearchTools asks for ToolSearch and the deferred tool it loads.
const toolSearchTools = `"tools":[
 {"name":"ToolSearch","input_schema":{"type":"object","properties":{"query":{"type":"string"}}}},
 {"name":"SendMessage","description":"Send a message.","defer_loading":true,
  "input_schema":{"type":"object","properties":{"text":{"type":"string"}}}}]`

// A tool result that names tools loaded and says nothing else reaches a
// Chat upstream with content, and the loaded tool goes with the request:
// an empty tool message is one an upstream may turn away. (Claude Code's
// own ToolSearch result is already turned into the loaded-tool text by
// the Messages reader, toolOutputIn; this is the builder's own guarantee
// for a request that reaches it with the names alone, as a Responses
// client's does.)
func TestToolSearchEmptyResultReachesChat(t *testing.T) {
	req := &Request{
		Tools: []Tool{
			{Name: "SendMessage", Description: "Send a message.", Deferred: true,
				Schema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
		},
		Messages: []Message{
			{Role: "assistant", Parts: []Part{{Kind: ToolCall, ID: "call_1", Name: "ToolSearch", Args: json.RawMessage(`{"query":"select:SendMessage"}`)}}},
			{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "call_1", Loads: []string{"SendMessage"}}}},
		},
	}
	body := string(buildChat(req, "gemini-4-neon", "api.githubcopilot.com", false))
	if !strings.Contains(body, `"content":"Loaded tool: SendMessage."`) {
		t.Errorf("the tool result went as empty: %s", body)
	}
	if !strings.Contains(body, `"name":"SendMessage"`) {
		t.Errorf("the loaded tool wasn't offered: %s", body)
	}
}

// Claude Code's own ToolSearch history reaches Copilot's chat with the
// turn answered, the text the Messages reader gives its references, and
// the tool they load offered.
func TestToolSearchHistoryReachesChat(t *testing.T) {
	fresh(t)
	up := &copilotChatUp{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gemini-4-neon"},
		Chat: "https://api.githubcopilot.com",
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	req := `{"model":"gh-copilot/gemini-4-neon","stream":false,"max_tokens":32000,` + toolSearchTools + `,` + toolSearchHistory + `}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(req)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.sent) != 1 {
		t.Fatalf("Copilot was asked %d times", len(up.sent))
	}
	if !strings.Contains(up.sent[0], `"content":"Tool SendMessage is loaded and can be called now."`) {
		t.Errorf("the tool result carried no loaded-tool text: %s", up.sent[0])
	}
	if !strings.Contains(up.sent[0], `"name":"SendMessage"`) {
		t.Errorf("the loaded tool wasn't offered: %s", up.sent[0])
	}
}

// The same gap in the Gemini builder: a ToolSearch result that names tools
// answers with them, not with an empty output.
func TestToolSearchResultReachesGemini(t *testing.T) {
	req := &Request{
		System: "be brief",
		Tools: []Tool{
			{Name: "ToolSearch", Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)},
			{Name: "SendMessage", Description: "Send a message.", Deferred: true, Schema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
		},
		Messages: []Message{
			{Role: "assistant", Parts: []Part{{Kind: ToolCall, ID: "toolu_ts1", Name: "ToolSearch", Args: json.RawMessage(`{"query":"select:SendMessage"}`)}}},
			{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "toolu_ts1", Loads: []string{"SendMessage"}}}},
		},
	}
	body, err := buildGemini(req, "gemini-4-neon")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Loaded tool: SendMessage.") {
		t.Errorf("the tool result went as an empty output: %s", body)
	}
	if !strings.Contains(string(body), `"name":"SendMessage"`) {
		t.Errorf("the loaded tool wasn't offered: %s", body)
	}
}

// Copilot's Gemini turns away 400 "invalid request body" for a required
// entry that names no property. That is the vendor's under any provider id
// pointed at its host, not only its built-in account.
func TestCopilotKeyProviderGetsGeminiToolsFit(t *testing.T) {
	fresh(t)
	up := &copilotChatUp{refuse: func(body string) bool {
		return strings.Contains(body, `"required":["ghost"`)
	}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gemini-4-neon"},
		Chat: "https://api.githubcopilot.com",
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	req := `{"model":"gh-copilot/gemini-4-neon","stream":false,"max_tokens":32000,
	 "tools":[{"name":"SendMessage","input_schema":{"type":"object","properties":{"text":{"type":"string"}},"required":["ghost","text"]}}],
	 "messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(req)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.sent) != 1 || !strings.Contains(up.sent[0], `"required":["text"]`) {
		t.Fatalf("the schema wasn't fitted: %s", up.sent)
	}
}
