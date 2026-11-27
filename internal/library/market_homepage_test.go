package library

import (
	"encoding/json"
	"testing"
)

// A registry entry's link is a web page or nothing: javascript:, data:, a
// file of the user's or an ftp address is dropped, and the repository's
// page stands in when it has one (#L11).
func TestRegistryHomepageIsAWebAddressOnly(t *testing.T) {
	mk := func(website string) registryServer {
		r := registryServer{Name: "io.github.acme/docs", WebsiteURL: website}
		r.Repository.URL = "https://github.com/acme/docs"
		// a remote, so the entry is one owl can serve
		if err := json.Unmarshal([]byte(`{"remotes":[{"type":"streamable-http","url":"https://acme.example/mcp"}]}`), &r); err != nil {
			panic(err)
		}
		return r
	}
	for _, u := range []string{"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "ftp://example.com/x", "//no-scheme"} {
		r := mk(u)
		m, ok := fromRegistry(&r)
		if !ok {
			t.Fatalf("%q: entry dropped", u)
		}
		if m.Homepage != "https://github.com/acme/docs" {
			t.Errorf("%q: homepage = %q", u, m.Homepage)
		}
	}
	r := mk("https://acme.example/docs")
	if m, _ := fromRegistry(&r); m.Homepage != "https://acme.example/docs" {
		t.Errorf("a web page was dropped: %q", m.Homepage)
	}
}
