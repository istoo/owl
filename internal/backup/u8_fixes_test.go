package backup

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

// A backup without keys carries no proxy login, inline or not: an address
// typed as socks5://user:pass@host:port is a password the file would keep
// in clear (#70).
func TestCollectWithoutKeysStripsInlineProxyLogins(t *testing.T) {
	home(t)
	if err := settings.Save(settings.Settings{Proxies: []settings.NamedProxy{{ID: "px1234567890", Name: "Named", URL: "http://127.0.0.1:7890"}}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID: "p", Name: "P", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"},
		Key:   "sk-one",
		Keys:  []provider.KeyAccount{{Key: "sk-two"}},
		Proxy: "socks5://alice:secret@proxy.example:1080",
	}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("p")
	if err != nil {
		t.Fatal(err)
	}
	ids := p.KeyList()
	if len(ids) != 2 {
		t.Fatalf("keys: %+v", ids)
	}
	// each key's own proxy: one typed with a login, one naming a saved proxy
	p.AccountProxies = map[string]string{
		ids[0].ID: "http://alice:secret@proxy.example:1080",
		ids[1].ID: "@px1234567890",
	}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}

	for name, collect := range map[string]func() (Bundle, error){
		"without keys": func() (Bundle, error) { return Collect(false, "test") },
		"an automatic backup": func() (Bundle, error) {
			return autoCollect("test")
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := collect()
			if err != nil {
				t.Fatal(err)
			}
			if len(b.Providers) != 1 {
				t.Fatalf("providers: %+v", b.Providers)
			}
			p := b.Providers[0]
			if p.Proxy != "socks5://proxy.example:1080" {
				t.Errorf("proxy = %q, want the login out", p.Proxy)
			}
			if got := p.AccountProxies[ids[0].ID]; got != "http://proxy.example:1080" {
				t.Errorf("account proxy = %q, want the login out", got)
			}
			if got := p.AccountProxies[ids[1].ID]; got != "@px1234567890" {
				t.Errorf("named proxy = %q, want the reference kept", got)
			}
			raw, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("alice")) || bytes.Contains(raw, []byte("secret")) {
				t.Fatalf("the bundle still holds the proxy's login: %s", raw)
			}
			if !strings.Contains(string(raw), "@px1234567890") {
				t.Fatal("the named proxy was taken out too")
			}
		})
	}
}

// A named proxy is a reference, not an address with a login, and "direct"
// is neither.
func TestStripProxyCreds(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "",
		"direct":                "direct",
		"@px1234567890":         "@px1234567890",
		"http://127.0.0.1:7890": "http://127.0.0.1:7890",
		"socks5://alice:secret@proxy.example:1080": "socks5://proxy.example:1080",
		"http://alice@proxy.example:8080":          "http://proxy.example:8080",
		"alice:secret@proxy.example:8080":          "proxy.example:8080",
	} {
		if got := stripProxyCreds(in); got != want {
			t.Errorf("stripProxyCreds(%q) = %q, want %q", in, got, want)
		}
	}
}
