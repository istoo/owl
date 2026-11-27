package gateway

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// copilotItemRefusal is Copilot's answer to a request replaying a sealed
// item minted by another of its keys (the report's 401).
const copilotItemRefusal = "input item ID does not belong to this connection"

// copilotAsk is one request a Copilot fake was sent.
type copilotAsk struct {
	key   string
	seals []string // the key or account each reasoning item was sealed by
	body  []byte
}

// copilotItemUp is Copilot's API for key providers: it seals its reasoning
// as "sealed-by-<key>#<n>", reads only its own key's items, and answers
// another key's with 401 "input item ID does not belong to this
// connection", as the real vendor does. A key in down answers 502 instead,
// for a conversation to move on. acceptForeign answers anyone's items (an
// OpenAI-style key, whose reasoning is the organization's).
type copilotItemUp struct {
	acceptForeign bool
	// refuseStatus answers every request with Copilot's item refusal at
	// that status, for one that holds no item owl can take out.
	refuseStatus int
	mu           sync.Mutex
	n            int
	down         map[string]bool
	asks         []copilotAsk
}

func (u *copilotItemUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var q struct {
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(b, &q)
	var seals []string
	foreign := false
	for _, it := range q.Input {
		if it["type"] != "reasoning" {
			continue
		}
		enc, _ := it["encrypted_content"].(string)
		by, _, _ := strings.Cut(strings.TrimPrefix(enc, "sealed-by-"), "#")
		seals = append(seals, by)
		if by != key {
			foreign = true
		}
	}
	u.mu.Lock()
	u.n++
	n := u.n
	down := u.down[key]
	u.asks = append(u.asks, copilotAsk{key: key, seals: seals, body: b})
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if down {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":{"message":"Service Unavailable"}}`)
		return
	}
	if u.refuseStatus != 0 || foreign && !u.acceptForeign {
		status := cmp.Or(u.refuseStatus, http.StatusUnauthorized)
		w.WriteHeader(status)
		io.WriteString(w, `{"error":{"message":"`+copilotItemRefusal+`","type":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	call := fmt.Sprintf("call_%s_%d", key, n)
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"r`+call+`","model":"gpt-6.2-astra"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_`+call+`","summary":[],"encrypted_content":"sealed-by-`+key+`#`+call+`"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_`+call+`","call_id":"`+call+`","name":"shell","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_`+call+`","output_index":1,"delta":"{\"cmd\":\"cat a.txt\"}"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_`+call+`","call_id":"`+call+`","name":"shell","arguments":"{\"cmd\":\"cat a.txt\"}","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"r`+call+`","usage":{"input_tokens":7,"output_tokens":1}}}`))
}

// taken is what the fake was asked since the last call: each request's key
// and the sealer of each reasoning item it carried.
func (u *copilotItemUp) taken() []copilotAsk {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := u.asks
	u.asks = nil
	return out
}

// asksOf is taken's asks for key alone.
func asksOf(asks []copilotAsk, key string) []copilotAsk {
	var out []copilotAsk
	for _, a := range asks {
		if a.key == key {
			out = append(out, a)
		}
	}
	return out
}

// copilotClient is a Claude Code-shaped Messages client in a tool loop: it
// keeps the call ids it was given, as given, and sends them back with
// their results.
type copilotClient struct {
	calls []string
}

func (c *copilotClient) ask(t *testing.T, s *Server, model string) (int, string) {
	t.Helper()
	msgs := `{"role":"user","content":"Sum the numbers in a.txt"}`
	for _, id := range c.calls {
		msgs += `,{"role":"assistant","content":[{"type":"text","text":"Reading it."},{"type":"tool_use","id":"` + id + `","name":"shell","input":{"cmd":"cat a.txt"}}]}` +
			`,{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"1 2 3"}]}`
	}
	body := `{"model":"` + model + `","stream":false,"max_tokens":32000,"thinking":{"type":"adaptive"},` +
		`"tools":[{"name":"shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}}}}],` +
		`"messages":[` + msgs + `]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	if rec.Code == 200 {
		c.calls = append(c.calls, askedCallID(t, provider.Anthropic, rec.Body.Bytes(), false))
	}
	return rec.Code, rec.Body.String()
}

// copilotKey saves a key provider at Copilot's host, its requests going to
// up (no network): k1 first, k2 beside it.
func copilotKey(t *testing.T, id string, twoKeys bool) {
	t.Helper()
	p := provider.Provider{
		ID: id, Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gpt-6.2-astra"},
		Responses: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}
	if twoKeys {
		p.Keys = []provider.KeyAccount{{Key: "k2"}}
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}

// copilotTransport sends every request to up whatever host it names, so a
// provider at api.githubcopilot.com reaches the local fake.
func copilotTransport(t *testing.T, up *httptest.Server) *http.Client {
	t.Helper()
	to, _ := url.Parse(up.URL)
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = to.Scheme, to.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
}

// carriedKey is the store key of the reasoning an id carries.
func carriedKey(t *testing.T, id string) string {
	t.Helper()
	at := len(id) - reasonKeyLen - len(reasonTail)
	if at < 1 || id[at:at+len(reasonTail)] != reasonTail {
		t.Fatalf("id %q carries no reasoning", id)
	}
	return id[at+len(reasonTail):]
}

// The conversation moved to a key that doesn't hold its sealed item, and
// owl doesn't know who minted it (kept before minters were recorded): the
// 401 Copilot answers is the request's, not the key's — asked again on the
// same key without the item, nobody rests, no other key is sent the body,
// and the item stays out of that conversation's later turns there.
func TestCopilotForeignItemAskedAgainOnSameKey(t *testing.T) {
	fresh(t)
	up := &copilotItemUp{down: map[string]bool{}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	copilotKey(t, "gh-copilot", true)
	s := New()
	s.client = copilotTransport(t, srv)
	c := &copilotClient{}

	if code, body := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != 200 {
		t.Fatalf("first turn: %d %s", code, body)
	}
	id := c.calls[0]
	if _, items, from := unreasonedID(id); len(items) != 1 || from != "gh-copilot#"+provider.KeyID("k1") {
		t.Fatalf("the call's id %q carries %d items minted by %q", id, len(items), from)
	}
	// the item's minter is forgotten, as one kept before owl recorded them
	os.Remove(filepath.Join(sigDir(), carriedKey(t, id)+minterExt))
	up.taken()

	// the first key is turned off: the conversation goes to the second,
	// which doesn't hold the first's item
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k2", Models: []string{"gpt-6.2-astra"},
		Keys: []provider.KeyAccount{{Key: "k1", Off: true}}, Responses: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}); err != nil {
		t.Fatal(err)
	}
	if code, body := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != 200 {
		t.Fatalf("after the switch: %d %s", code, body)
	}
	asks := up.taken()
	if n := len(asksOf(asks, "k1")); n != 0 {
		t.Fatalf("the switched-away key was asked %d times", n)
	}
	k2 := asksOf(asks, "k2")
	if len(k2) != 2 {
		t.Fatalf("the second key was asked %d times, want the refusal and the retry: %+v", len(k2), k2)
	}
	if fmt.Sprint(k2[0].seals) != "[k1]" || fmt.Sprint(k2[1].seals) != "[]" {
		t.Fatalf("the second key carried %v, want the first key's item, then none", k2)
	}
	// the 401 didn't rest the key or count a failure, and kept it for the
	// conversation
	if _, resting := restOf("gh-copilot#" + provider.KeyID("k2")); resting {
		t.Fatal("the second key rested over the request's own item")
	}
	route := s.trace.routes[len(s.trace.routes)-1]
	var saw bool
	for _, tr := range route.Tries {
		if tr.Status == http.StatusUnauthorized {
			saw = true
			if tr.Fail != failForeign || tr.Rest != nil {
				t.Fatalf("the 401 was booked as fail=%q rest=%+v", tr.Fail, tr.Rest)
			}
		}
	}
	if !saw {
		t.Fatalf("no 401 in the route's tries: %+v", route.Tries)
	}

	// the next turn there leaves the refused item out up front, and the
	// key's own goes along
	if code, body := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != 200 {
		t.Fatalf("the turn after: %d %s", code, body)
	}
	next := asksOf(up.taken(), "k2")
	if len(next) != 1 || fmt.Sprint(next[0].seals) != "[k2]" {
		t.Fatalf("the next turn carried %v, want the second key's own reasoning alone", next)
	}
}

// Copilot's item refusal is the request's, not the key's: with nothing owl
// can take out, it rests nobody, counts no failure, and the provider's
// other keys aren't sent the same body (each would answer it alike), at
// either status the vendor uses.
func TestCopilotForeignWordsRestNoKey(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fresh(t)
			up := &copilotItemUp{refuseStatus: status, down: map[string]bool{}}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			copilotKey(t, "gh-copilot", true)
			s := New()
			s.client = copilotTransport(t, srv)
			c := &copilotClient{}
			if code, _ := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != status {
				t.Fatalf("the agent was told %d, want Copilot's %d", code, status)
			}
			asks := up.taken()
			if len(asks) != 1 || asks[0].key != "k1" {
				t.Fatalf("the provider was asked %d times, want the first key alone: %+v", len(asks), asks)
			}
			for _, k := range []string{"k1", "k2"} {
				if _, resting := restOf("gh-copilot#" + provider.KeyID(k)); resting {
					t.Fatalf("key %s rested over the request's own item", k)
				}
			}
			route := s.trace.routes[len(s.trace.routes)-1]
			for _, tr := range route.Tries {
				if tr.Fail == failAuth || tr.Rest != nil && tr.Rest.By != "off" {
					t.Fatalf("the refusal was booked as fail=%q rest=%+v", tr.Fail, tr.Rest)
				}
			}
			routed.Lock()
			counted := routed.failures["gh-copilot#"+provider.KeyID("k1")] + routed.failures["gh-copilot#"+provider.KeyID("k2")]
			routed.Unlock()
			if counted != 0 {
				t.Fatalf("the refusal counted %d failures", counted)
			}
		})
	}
}

// A Responses client's own items take the same path: the item another key
// sealed is left out and the same key asked again, and the refusal rests
// nobody.
func TestCopilotForeignItemResponsesClientAskedAgain(t *testing.T) {
	fresh(t)
	up := &copilotItemUp{down: map[string]bool{}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k2", Models: []string{"gpt-6.2-astra"},
		Keys: []provider.KeyAccount{{Key: "k1", Off: true}}, Responses: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	req := `{"model":"gh-copilot/gpt-6.2-astra","stream":true,"input":[
	 {"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"sealed-by-k1#old"},
	 {"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(req)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	asks := asksOf(up.taken(), "k2")
	if len(asks) != 2 {
		t.Fatalf("the second key was asked %d times, want the refusal and the retry: %+v", len(asks), asks)
	}
	if fmt.Sprint(asks[0].seals) != "[k1]" || fmt.Sprint(asks[1].seals) != "[]" {
		t.Fatalf("the second key carried %v, want the first key's item, then none", asks)
	}
	if _, resting := restOf("gh-copilot#" + provider.KeyID("k2")); resting {
		t.Fatal("the second key rested over the request's own item")
	}
}

// A Responses client's conversation that last answered on another key of
// the same Copilot-hosted provider leaves that key's sealed items out on
// the first ask, without a refusal.
func TestCopilotSwitchLeavesResponsesItemsOut(t *testing.T) {
	fresh(t)
	up := &copilotItemUp{down: map[string]bool{}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	first := provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k1", Models: []string{"gpt-6.2-astra"},
		Responses: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}
	if err := provider.Save(first); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.client = copilotTransport(t, srv)
	turn := `{"model":"gh-copilot/gpt-6.2-astra","stream":true,"input":[
	 %s
	 {"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	ask := func(input string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(turn, input))))
		return rec.Code, rec.Body.String()
	}
	if code, body := ask(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},`); code != 200 {
		t.Fatalf("first turn: %d %s", code, body)
	}
	up.taken()

	// the first key is turned off: the next turn goes to the second, which
	// can't read the item the first sealed
	if err := provider.Save(provider.Provider{
		ID: "gh-copilot", Name: "GitHub Copilot (key)", Key: "k2", Models: []string{"gpt-6.2-astra"},
		Keys: []provider.KeyAccount{{Key: "k1", Off: true}}, Responses: "https://api.githubcopilot.com", Routing: provider.Ordered,
	}); err != nil {
		t.Fatal(err)
	}
	if code, body := ask(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
	 {"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"sealed-by-k1#old"},`); code != 200 {
		t.Fatalf("after the switch: %d %s", code, body)
	}
	k2 := asksOf(up.taken(), "k2")
	if len(k2) != 1 || fmt.Sprint(k2[0].seals) != "[]" {
		t.Fatalf("the second key was asked %d times with %v, want one ask and nothing of the first's", len(k2), k2)
	}

	// the second key answered: its own item goes back to it
	if code, body := ask(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
	 {"type":"reasoning","id":"rs_2","summary":[],"encrypted_content":"sealed-by-k2#new"},`); code != 200 {
		t.Fatalf("the turn after: %d %s", code, body)
	}
	next := asksOf(up.taken(), "k2")
	if len(next) != 1 || fmt.Sprint(next[0].seals) != "[k2]" {
		t.Fatalf("the second key's own turn carried %v, want its own reasoning", next)
	}
}

// A conversation whose next request lands on another key of the same
// Copilot-hosted provider leaves the item the first key minted out before
// it is sent: no refused multi-MB upload per switch.
func TestCopilotSwitchLeavesAnotherKeysItemOut(t *testing.T) {
	fresh(t)
	up := &copilotItemUp{down: map[string]bool{}}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	copilotKey(t, "gh-copilot", true)
	s := New()
	s.client = copilotTransport(t, srv)
	c := &copilotClient{}

	if code, body := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != 200 {
		t.Fatalf("first turn: %d %s", code, body)
	}
	id := c.calls[0]
	if _, items, from := unreasonedID(id); len(items) != 1 || from != "gh-copilot#"+provider.KeyID("k1") {
		t.Fatalf("the call's id %q carries %d items minted by %q", id, len(items), from)
	}
	up.taken()

	// the first key fails a passing way, and the conversation moves to the
	// second
	up.mu.Lock()
	up.down["k1"] = true
	up.mu.Unlock()
	if code, body := c.ask(t, s, "gh-copilot/gpt-6.2-astra"); code != 200 {
		t.Fatalf("after the switch: %d %s", code, body)
	}
	k2 := asksOf(up.taken(), "k2")
	if len(k2) != 1 {
		t.Fatalf("the second key was asked %d times, want one with nothing of the first's: %+v", len(k2), k2)
	}
	if fmt.Sprint(k2[0].seals) != "[]" {
		t.Fatalf("the second key was sent the first key's reasoning %v before it was refused", k2[0].seals)
	}
}

// OpenAI's API seals reasoning to the organization, not to the key: a
// conversation moving between its keys carries the reasoning exactly as it
// did, byte for byte.
func TestOpenAIKeyProviderKeepsCarriedReasoning(t *testing.T) {
	var bodies [][]byte
	for _, c := range []struct {
		name    string
		twoKeys bool
	}{{"two keys", true}, {"one key", false}} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := &copilotItemUp{acceptForeign: true, down: map[string]bool{}}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			p := provider.Provider{
				ID: "openai-api", Name: "OpenAI", Key: "k1", Models: []string{"gpt-6.2-astra"},
				Responses: "https://api.openai.com", Routing: provider.Ordered,
			}
			if c.twoKeys {
				p.Keys = []provider.KeyAccount{{Key: "k2"}}
			}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			s := New()
			s.client = copilotTransport(t, srv)
			cl := &copilotClient{}
			if code, body := cl.ask(t, s, "openai-api/gpt-6.2-astra"); code != 200 {
				t.Fatalf("first turn: %d %s", code, body)
			}
			up.taken()
			if c.twoKeys {
				// the first key's 502 moves it to the second, which holds
				// the reasoning as before
				up.mu.Lock()
				up.down["k1"] = true
				up.mu.Unlock()
			}
			if code, body := cl.ask(t, s, "openai-api/gpt-6.2-astra"); code != 200 {
				t.Fatalf("after the switch: %d %s", code, body)
			}
			asks := up.taken()
			last := asks[len(asks)-1]
			if fmt.Sprint(last.seals) != "[k1]" {
				t.Fatalf("the OpenAI key carried %v, want the first's reasoning", last.seals)
			}
			bodies = append(bodies, last.body)
		})
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("the request another OpenAI key gets differs from the one key's:\n%s\n---\n%s", bodies[0], bodies[1])
	}
}

// The same on the wire: a request to an OpenAI key provider is unchanged by
// the minter, so a part another key minted stays in it.
func TestOpenAIRequestKeepsAnotherKeysSeal(t *testing.T) {
	fresh(t)
	p := provider.Provider{ID: "openai-api", Key: "k2", Responses: "https://api.openai.com", Models: []string{"gpt-6.2-astra"}}
	ctx := withSealScope(t.Context(), p.ID, "conversation", p.ID+"#"+provider.KeyID("k2"))
	item := json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"sealed-by-k1"}`)
	r := &Request{Messages: []Message{{Role: "assistant", Parts: []Part{
		{Kind: Thinking, Sealed: item, SealedBy: sealResponses, SealedFrom: "openai-api#" + provider.KeyID("k1")},
		{Kind: Text, Text: "hi"},
	}}}}
	withoutForeignMinted(ctx, p, provider.Responses, r)
	if got := r.Messages[0].Parts; len(got) != 2 || len(got[0].Sealed) == 0 {
		t.Fatalf("an OpenAI key provider's request lost its carried reasoning: %+v", got)
	}
	// and a Copilot-hosted key provider's is left out
	cp := provider.Provider{ID: "gh-copilot", Key: "k2", Responses: "https://api.githubcopilot.com", Models: []string{"gpt-6.2-astra"}}
	ctx = withSealScope(t.Context(), cp.ID, "conversation", cp.ID+"#"+provider.KeyID("k2"))
	withoutForeignMinted(ctx, cp, provider.Responses, r)
	if got := r.Messages[0].Parts; len(got) != 1 || got[0].Kind != Text {
		t.Fatalf("a Copilot key was sent another key's carried reasoning: %+v", got)
	}
}
