package library

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/istoo/owl/internal/mcpauth"
	"github.com/istoo/owl/internal/mcpauth/mcpauthtest"
)

// barrier holds the first two POSTs that carry want (the revoked token)
// until both have arrived, so two requests meet the same 401 together.
type barrier struct {
	base    http.RoundTripper
	want    string
	mu      sync.Mutex
	n       int
	release chan struct{}
}

func (b *barrier) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost && r.Header.Get("Authorization") == b.want {
		b.mu.Lock()
		b.n++
		if b.n == 2 {
			close(b.release)
		}
		b.mu.Unlock()
		<-b.release
	}
	return b.base.RoundTrip(r)
}

// Two requests that meet the same stale token's 401 renew it once: the
// second asks again with the token the first renewed and never marks it
// dead (#36).
func TestCheckConcurrent401RenewsOnce(t *testing.T) {
	sandbox(t)
	checked.Lock()
	checked.m = map[string]checkedServer{}
	checked.Unlock()
	f := mcpauthtest.New(t)
	srv := &Server{Name: "neon", Transport: "http", URL: f.URL}
	ok(t)(SaveServer("", Server{Name: "neon", Transport: "http", URL: f.URL, Agents: []string{}}))
	f.SignIn(t, "neon")

	r, _ := mcpauth.Get("neon")
	f.Revoke() // the server ends the token early

	old := checkClient
	b := &barrier{base: old.Transport, want: "Bearer " + r.Access, release: make(chan struct{})}
	checkClient = &http.Client{Transport: b}
	t.Cleanup(func() { checkClient = old })

	c, bad := newHTTPSession(context.Background(), srv)
	if bad != nil {
		t.Fatalf("session: %+v", bad)
	}
	var wg sync.WaitGroup
	errs := make([]*ServerHealth, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = c.initialize(context.Background(), initializeReq())
		}(i)
	}
	wg.Wait()
	for i, h := range errs {
		if h != nil {
			t.Fatalf("request %d: %+v", i, h)
		}
	}
	if f.Refreshed != 1 {
		t.Fatalf("renewed %d times, want once", f.Refreshed)
	}
	r2, _ := mcpauth.Get("neon")
	if r2.Access == r.Access {
		t.Fatal("the token wasn't renewed")
	}
	if st := mcpauth.StatusOf("neon", f.URL); st.Dead {
		t.Fatalf("the renewed token was marked dead: %+v", st)
	}
}
