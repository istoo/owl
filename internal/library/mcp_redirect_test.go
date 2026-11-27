package library

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The check follows a redirect only on the server's own origin, or its
// http address moving to https: a server cannot send the check's request,
// and the key it carries, to a host of its choosing (#H27).
func TestCheckRedirectStaysOnOrigin(t *testing.T) {
	mustURL := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	cases := []struct {
		from, to string
		ok       bool
	}{
		{"https://mcp.example.com/a", "https://mcp.example.com/b", true},
		{"https://mcp.example.com", "https://mcp.example.com:443/b", true}, // the default port
		{"https://mcp.example.com/a", "https://evil.example/b", false},
		{"https://mcp.example.com/a", "https://mcp.example.com:8443/b", false},
		{"https://mcp.example.com/a", "http://mcp.example.com/b", false}, // a downgrade
		{"http://mcp.example.com/a", "https://mcp.example.com/b", true},  // an upgrade
		{"https://mcp.example.com/a", "http://127.0.0.1:9999/b", false},
	}
	for _, c := range cases {
		req := &http.Request{URL: mustURL(c.to)}
		via := []*http.Request{{URL: mustURL(c.from)}}
		if err := checkRedirect(req, via); (err == nil) != c.ok {
			t.Errorf("%s -> %s: %v, want ok=%v", c.from, c.to, err, c.ok)
		}
	}
	// a chain of ten is where it stops
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = &http.Request{URL: mustURL("https://mcp.example.com/")}
	}
	if err := checkRedirect(&http.Request{URL: mustURL("https://mcp.example.com/")}, via); err == nil {
		t.Error("eleven hops were followed")
	}
}

// The client itself refuses a redirect to another origin, so a server
// cannot bounce the check to a second server's host and port (#H27).
func TestCheckClientRefusesAnotherOrigin(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()
	c := &http.Client{CheckRedirect: checkRedirect}
	res, err := c.Get(srv.URL + "/a")
	if err == nil {
		res.Body.Close()
		t.Fatal("the check followed a redirect to another server")
	}
}

// A redirect on the server's own origin is followed as before.
func TestCheckClientFollowsItsOwnOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			http.Redirect(w, r, "/b", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &http.Client{CheckRedirect: checkRedirect}
	res, err := c.Get(srv.URL + "/a")
	if err != nil {
		t.Fatalf("a redirect on its own origin was refused: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status %d", res.StatusCode)
	}
}
