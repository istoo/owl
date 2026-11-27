package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// copilot408 is Copilot's answer to a slow, multi-MB upload: the vendor
// asking, in so many words, for a smaller request.
const copilot408 = "Timed out reading request body. Try again, or use a smaller request size."

// copilotCapUp is Copilot's API for a key provider: a body over max is
// answered 408 with Copilot's words, and any other request is answered.
// Every request's key and body is kept.
type copilotCapUp struct {
	mu   sync.Mutex
	max  int
	sent []copilotAsk
}

func (u *copilotCapUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	u.mu.Lock()
	u.sent = append(u.sent, copilotAsk{key: key, body: b})
	max := u.max
	u.mu.Unlock()
	if len(b) > max {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestTimeout)
		io.WriteString(w, `{"error":{"message":"`+copilot408+`","type":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"id":"c1","model":"gemini-4-neon","choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`))
}

// bigImage is an Anthropic history image of n base64 characters.
func capImageJSON(n int) string {
	return `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + strings.Repeat("A", n) + `"}}`
}

func askAnthropic(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// Copilot's 408 that asks for a smaller request is read as a byte-cap
// refusal; a 408 that says nothing of the body stays what it was, and a
// 401 never counts.
func TestCopilotBodyCapWording(t *testing.T) {
	body := []byte(`{"error":{"message":"` + copilot408 + `"}}`)
	if !byteCapRefusal(http.StatusRequestTimeout, body) {
		t.Error("Copilot's 408 asking for a smaller request wasn't read as a byte cap")
	}
	if byteCapRefusal(http.StatusRequestTimeout, []byte(`{"error":{"message":"upstream timed out"}}`)) {
		t.Error("a plain 408 was read as a byte cap")
	}
	if byteCapRefusal(http.StatusUnauthorized, body) {
		t.Error("a 401 was read as a byte cap")
	}
}

// A request Copilot times out reading is fitted under the cap it teaches:
// its history image left out, and the same key asked again. The cap stays
// for later requests, which are fitted before they are sent; no other key
// of the provider is sent the same body.
func TestCopilotTimedOutBodyLearnsCapAndTrimsImage(t *testing.T) {
	fresh(t)
	up := &copilotCapUp{max: 200 << 10}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gemini-4-neon"},
		Keys: []provider.KeyAccount{{Key: "k2"}}, Chat: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	first := `{"model":"gh-copilot/gemini-4-neon","stream":false,"max_tokens":32000,"messages":[
	 {"role":"user","content":[` + capImageJSON(300000) + `,{"type":"text","text":"what is this?"}]},
	 {"role":"assistant","content":"a picture"},
	 {"role":"user","content":"now summarize"}]}`
	if code, body := askAnthropic(t, s, first); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	up.mu.Lock()
	sent := up.sent
	up.sent = nil
	up.mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("Copilot was asked %d times, want the refusal and the fitted retry", len(sent))
	}
	for _, a := range sent {
		if a.key != "k1" {
			t.Fatalf("the retry went to key %q, want the same one", a.key)
		}
	}
	if strings.Contains(string(sent[1].body), "base64") || !strings.Contains(string(sent[1].body), imageLeftOut) {
		t.Fatalf("the image wasn't left out: %s", sent[1].body)
	}
	cap := s.bodyCaps.of("gh-copilot")
	if cap <= 0 {
		t.Fatal("the 408 taught no cap")
	}

	// a later request near the cap is fitted before it is sent: one ask,
	// no image
	second := `{"model":"gh-copilot/gemini-4-neon","stream":false,"max_tokens":32000,"messages":[
	 {"role":"user","content":[` + capImageJSON(240000) + `,{"type":"text","text":"and this?"}]},
	 {"role":"assistant","content":"a picture"},
	 {"role":"user","content":"now summarize"}]}`
	if code, body := askAnthropic(t, s, second); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	up.mu.Lock()
	next := up.sent
	up.sent = nil
	up.mu.Unlock()
	if len(next) != 1 || next[0].key != "k1" {
		t.Fatalf("the later request was asked %d times: %+v", len(next), next)
	}
	if strings.Contains(string(next[0].body), "base64") {
		t.Fatalf("the later request's image wasn't fitted: %s", next[0].body)
	}
	if n := len(asksOf(sent, "k2")) + len(asksOf(next, "k2")); n != 0 {
		t.Fatalf("the provider's other key was sent the body %d times", n)
	}
}

// With no image to leave out, the size refusal goes to the agent as the
// 413 it is a cap of, so its compaction follows, and no other key of the
// provider is sent the same body.
func TestCopilotTimedOutBodyWithNothingToTrim(t *testing.T) {
	fresh(t)
	up := &copilotCapUp{max: 200 << 10}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gemini-4-neon"},
		Keys: []provider.KeyAccount{{Key: "k2"}}, Chat: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	req := `{"model":"gh-copilot/gemini-4-neon","stream":false,"max_tokens":32000,"messages":[
	 {"role":"user","content":"` + strings.Repeat("x", 300000) + `"},
	 {"role":"assistant","content":"ok"},
	 {"role":"user","content":"now summarize"}]}`
	code, body := askAnthropic(t, s, req)
	if code != http.StatusRequestEntityTooLarge || !strings.Contains(body, "request_too_large") {
		t.Fatalf("%d %s", code, body)
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.sent) != 1 || up.sent[0].key != "k1" {
		t.Fatalf("Copilot was asked %d times: %+v", len(up.sent), up.sent)
	}
}
