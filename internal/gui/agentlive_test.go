package gui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/usage"
)

// The Agents page's row says where the agent's last request through owl
// went, from the gateway's calls (agentlive.go): the state carries it, in
// both of its shapes, and reading the state does not read the whole usage
// summary. The call is the usage record's, by the request id it names.
func TestAgentLiveFromTheLastCall(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"recent/m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "recent", Name: "Recent Vendor", Chat: up.URL, Key: "fixture", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	s := gateway.New()
	was := served.Load()
	served.Store(s)
	t.Cleanup(func() { served.Store(was) })
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"recent/m","messages":[{"role":"user","content":"hi"}]}`))
	r.Header.Set("User-Agent", "codex/0.1")
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("gateway: %d %s", w.Code, w.Body)
	}

	lives := agentLives()
	l := lives["codex"]
	if l == nil || l.Provider != "Recent Vendor" || l.Model != "m" || l.At.IsZero() || l.Route == 0 {
		t.Fatalf("the codex's last call: %+v", l)
	}
	if l.Account != "" {
		t.Fatalf("a key provider has an account: %q", l.Account)
	}
	// the request id is the usage record's, and the time is the same instant
	recs := usage.Load(time.Now().Add(-time.Minute))
	var rec *usage.Record
	for i := range recs {
		if recs[i].Agent == "codex" && recs[i].RouteID == l.Route {
			rec = &recs[i]
			break
		}
	}
	if rec == nil || !rec.Time.Equal(l.At) || rec.Provider != "recent" || rec.Model != "m" {
		t.Fatalf("the record the line names: %+v (line %+v)", rec, l)
	}

	// the state never reads the whole summary: its agents' place comes from
	// the ring and from what a summary read elsewhere already published
	before := usage.SummaryReads()
	st := state()
	if after := usage.SummaryReads(); after != before {
		t.Fatalf("state() read %d summaries", after-before)
	}
	var live *agentLiveJSON
	for _, a := range st.Agents {
		if a.ID == "codex" {
			live = a.Live
		}
	}
	if live == nil || live.Provider != "Recent Vendor" || live.Route != l.Route {
		t.Fatalf("the state's codex: %+v", live)
	}
	// the shared shape carries it too: the page that asks for options once
	shared := shareOptions(st)
	for _, a := range shared.Agents {
		if a.ID != "codex" {
			continue
		}
		b, _ := json.Marshal(a)
		if !strings.Contains(string(b), `"live":{"at":`) || !strings.Contains(string(b), `"provider":"Recent Vendor"`) || !strings.Contains(string(b), `"route":`) {
			t.Fatalf("the shared shape's codex: %s", b)
		}
	}

	// a restart empties the ring; the summary ReadAhead works out at start
	// keeps today's call, so the line is not lost to it
	usage.SummarizeTop(usage.Today, 0)
	served.Store(gateway.New())
	again := agentLives()["codex"]
	if again == nil || again.Route != l.Route || again.Provider != "Recent Vendor" || !again.At.Equal(l.At) {
		t.Fatalf("after a restart: %+v", again)
	}
}

// A reroute's reason lives only in the gateway's ring: the summary read
// after the call names the same request, at the same instant, without it.
// The row keeps the reroute, from the ring, for as long as the ring has
// the call.
func TestAgentLiveKeepsTheRerouteAfterTheSummary(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"main/m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"rate limited"}}`)
	}))
	defer limited.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer good.Close()
	for _, p := range []provider.Provider{
		{ID: "main", Name: "Main Vendor", Chat: limited.URL, Key: "k1", Models: []string{"m"}, Fallback: []string{"spare/m"}},
		{ID: "spare", Name: "Spare Vendor", Chat: good.URL, Key: "k2", Models: []string{"m"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := gateway.New()
	was := served.Load()
	served.Store(s)
	t.Cleanup(func() { served.Store(was) })
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"main/m","messages":[{"role":"user","content":"hi"}]}`))
	r.Header.Set("User-Agent", "codex/0.1")
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("gateway: %d %s", w.Code, w.Body)
	}
	live := agentLives()["codex"]
	if live == nil || live.Provider != "Spare Vendor" || live.Rerouted == nil || !strings.Contains(live.Rerouted.From, "Main Vendor") || live.Rerouted.Why == "" {
		t.Fatalf("the rerouted call: %+v", live)
	}

	// the page reads today's usage after the call: the summary names this
	// very request (same route, same instant) and carries no fallback
	usage.SummarizeTop(usage.Today, 0)
	var seeded usage.LastCall
	found := false
	usage.VisitLastCalls(func(id string, c usage.LastCall) {
		if id == "codex" {
			seeded, found = c, true
		}
	})
	if !found || seeded.Route != live.Route || !seeded.At.Equal(live.At) {
		t.Fatalf("the summary's call: %+v (live %+v)", seeded, live)
	}
	again := agentLives()["codex"]
	if again == nil || again.Route != live.Route || again.Provider != "Spare Vendor" || again.Rerouted == nil || again.Rerouted.From != live.Rerouted.From || again.Rerouted.Why != live.Rerouted.Why {
		t.Fatalf("after the summary: %+v (was %+v)", again, live)
	}
}

// lastCallHome gives the test a home of its own with an empty ring, so its
// cases read what the summary published alone, and the start of today.
func lastCallHome(t *testing.T) time.Time {
	t.Helper()
	sandboxHome(t)
	was := served.Load()
	served.Store(gateway.New()) // an empty ring: the summary alone
	t.Cleanup(func() { served.Store(was) })
	return usage.Today.Since(usage.Clock())
}

// The line says the newest request owl carried to a provider: one the
// agent's app reported it made itself, a search owl ran with its own key,
// owl's own title call, and a request owl refused before asking anyone
// never say a provider, though they are newer. A request that reached a
// provider and failed did reach it, so it is the one shown.
func TestAgentLiveSkipsWhatOwlDidNotCarry(t *testing.T) {
	start := lastCallHome(t)
	at := func(d time.Duration) time.Time { return start.Add(time.Minute + d) }
	usage.Append(usage.Record{Time: at(0), Agent: "codex", Provider: "relay", Model: "m", RouteID: 1, Input: 10, Status: 200})
	usage.Append(usage.Record{Time: at(time.Minute), Agent: "codex", Provider: "searchvendor", Model: "deep", Source: usage.SourceSearch, RouteID: 2, Input: 10, Status: 200})
	usage.Append(usage.Record{Time: at(2 * time.Minute), Agent: "codex", Provider: "reported", Model: "m", Source: usage.SourceReported, RouteID: 3, Input: 10, Status: 200})
	usage.Append(usage.Record{Time: at(3 * time.Minute), Agent: "codex", Provider: "owl", Model: "gpt-title", RouteID: 4, Input: 10, Status: 200})
	usage.SummarizeTop(usage.Today, 0)
	l := agentLives()["codex"]
	if l == nil || l.Provider != "relay" || l.Route != 1 || !l.At.Equal(at(0)) {
		t.Fatalf("what owl did not carry: %+v", l)
	}

	// refused before it reached anyone: no provider, no place
	usage.Append(usage.Record{Time: at(4 * time.Minute), Agent: "codex", Model: "gone", RouteID: 5, Status: 400, Rejected: true})
	usage.SummarizeTop(usage.Today, 0)
	if l := agentLives()["codex"]; l == nil || l.Route != 1 {
		t.Fatalf("a refused request set a place: %+v", l)
	}

	// it reached the provider and was refused there: that provider answered
	usage.Append(usage.Record{Time: at(5 * time.Minute), Agent: "codex", Provider: "relay", Model: "m", RouteID: 6, Input: 10, Status: 429, Error: "rate limited"})
	usage.SummarizeTop(usage.Today, 0)
	l = agentLives()["codex"]
	if l == nil || l.Route != 6 || l.Provider != "relay" || l.Account != "" {
		t.Fatalf("a request the provider refused: %+v", l)
	}
}

// The ring's newest call is the line, even before a summary read; a call
// older than what the summary says does not move it back.
func TestAgentLiveRingWinsOverTheSummary(t *testing.T) {
	start := lastCallHome(t)
	usage.Append(usage.Record{Time: start, Agent: "codex", Provider: "relay", Model: "m", RouteID: 1, Input: 10, Status: 200})
	usage.SummarizeTop(usage.Today, 0)

	s := gateway.New()
	served.Store(s)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "ring", Chat: up.URL, Key: "fixture", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"ring/m","messages":[{"role":"user","content":"hi"}]}`))
	r.Header.Set("User-Agent", "codex/0.1")
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("gateway: %d %s", w.Code, w.Body)
	}
	l := agentLives()["codex"]
	if l == nil || l.Provider != "ring" || l.Route == 0 || l.Route == 1 {
		t.Fatalf("the ring's call: %+v", l)
	}
	// a newer call in the log than the ring's is not taken back by it
	usage.Append(usage.Record{Time: time.Now().Add(time.Minute), Agent: "codex", Provider: "newer", Model: "m", RouteID: 99, Input: 10, Status: 200})
	usage.SummarizeTop(usage.Today, 0)
	if got := agentLives()["codex"]; got == nil || got.Route != 99 || got.Provider != "newer" {
		t.Fatalf("a newer summary lost to the ring: %+v", got)
	}
	// a rerouted call says where it was passed over from
	names := map[string]string{"claude": "Claude"}
	if got := rerouteOf("claude (me@example.com): usage limit reached: every account is spent. Ask again later; x", names); got == nil || got.From != "Claude (me@example.com)" || got.Why != "usage limit reached: every account is spent" {
		t.Fatalf("a reroute: %+v", got)
	}
}

// A fallback's first entry names where the request was to go and why it
// didn't: provider ids become the names the page shows.
func TestRerouteOf(t *testing.T) {
	names := map[string]string{"claude": "Claude"}
	for _, c := range []struct {
		fallback string
		want     *agentRerouteJSON
	}{
		{"", nil},
		{"claude (me@example.com): usage limit reached: every account is spent. Ask again later; x", &agentRerouteJSON{From: "Claude (me@example.com)", Why: "usage limit reached: every account is spent"}},
		{"codex: 401 unauthorized; claude: 429", &agentRerouteJSON{From: "Codex", Why: "401 unauthorized"}},
	} {
		if got := rerouteOf(c.fallback, names); got == nil && c.want == nil {
			continue
		} else if got == nil || c.want == nil || *got != *c.want {
			t.Fatalf("rerouteOf(%q) = %+v, want %+v", c.fallback, got, c.want)
		}
	}
}
