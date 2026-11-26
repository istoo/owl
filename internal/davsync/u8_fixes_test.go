package davsync

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/istoo/owl/internal/backup"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

// The proxies kept by name travel with the providers: a sync that brings
// the providers in brings them too, so a provider naming one as "@px…"
// reaches its server here (#69).
func TestBringProvidersRestoresNamedProxies(t *testing.T) {
	a := newComputer(t)
	a.use(t)
	if err := settings.Save(settings.Settings{Proxies: []settings.NamedProxy{{ID: "px1234567890", Name: "P", URL: "http://127.0.0.1:7890", User: "u", Pass: "p"}}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "p", Name: "P", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"m"}, Proxy: "@px1234567890"}); err != nil {
		t.Fatal(err)
	}
	bundle, err := backup.Collect(true, "test")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Proxies == nil || len(*bundle.Proxies) != 1 {
		t.Fatalf("the bundle holds no proxy: %+v", bundle.Proxies)
	}

	b := newComputer(t)
	b.use(t)
	if err := bring(bundle, "providers"); err != nil {
		t.Fatal(err)
	}
	if addr, ok := settings.ResolveProxy("@px1234567890"); !ok || addr == "" {
		t.Fatalf("the named proxy is not here: %q, %v", addr, ok)
	}
	p, err := provider.Find("p")
	if err != nil {
		t.Fatal(err)
	}
	if p.Proxy != "@px1234567890" {
		t.Fatalf("the provider's proxy = %q, want the reference brought in", p.Proxy)
	}
}

// A preview asks with the saved password for the same account, another
// prefix in the same server included: the form's URL differs, the account
// doesn't (#75).
func TestPreviewWithKeepsTheSavedPasswordForAnotherPrefix(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	c := newComputer(t)
	c.use(t)
	if err := Configure(Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	// another prefix, no password typed: the server still asks for me/pw
	// (fakeDAV answers 401 to anything else)
	if _, err := PreviewWith(context.Background(), Config{URL: srv.URL + "/dav2/", User: "me"}); err != nil {
		t.Fatalf("preview with the saved password: %v", err)
	}
}
