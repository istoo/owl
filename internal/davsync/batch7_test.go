package davsync

import (
	"crypto/md5"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Digest challenge is kept by host: a request to one server never
// answers with another's realm and nonce (#M41).
func TestDigestChallengesAreKeptByHost(t *testing.T) {
	a := &fakeDigest{alg: "MD5", newHash: md5.New, seenNC: map[string]bool{}, realm: "realm a", nonce: "nonce a"}
	b := &fakeDigest{alg: "MD5", newHash: md5.New, seenNC: map[string]bool{}, realm: "realm b", nonce: "nonce b"}
	sa := httptest.NewServer(a)
	defer sa.Close()
	sb := httptest.NewServer(b)
	defer sb.Close()

	tr := &digestTransport{next: http.DefaultTransport}
	send := func(u string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("me", "pw")
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: HTTP %d", u, res.StatusCode)
		}
	}
	send(sa.URL)
	send(sb.URL)
	send(sa.URL) // warm: this must not be sent b's challenge
	if a.unauthorized != 1 {
		t.Errorf("server a answered %d 401s, want 1 (the first, basic one): its challenge was forgotten", a.unauthorized)
	}
	if b.unauthorized != 1 {
		t.Errorf("server b answered %d 401s, want 1 (the first, basic one)", b.unauthorized)
	}
}

// A WebDAV redirect may not take the login from HTTPS down to HTTP, nor to
// another host (#M41).
func TestDAVRedirectGuards(t *testing.T) {
	secure, _ := http.NewRequest(http.MethodGet, "https://dav.example/a", nil)
	plain, _ := http.NewRequest(http.MethodGet, "http://dav.example/a", nil)
	if err := checkDAVRedirect(plain, []*http.Request{secure}); err == nil {
		t.Fatal("HTTPS to HTTP was allowed")
	}

	other, _ := http.NewRequest(http.MethodGet, "https://storage.example/a", nil)
	other.Header.Set("Authorization", "Basic c2VjcmV0")
	other.Header.Set("Cookie", "session=1")
	if err := checkDAVRedirect(other, []*http.Request{secure}); err != nil {
		t.Fatal(err)
	}
	if other.Header.Get("Authorization") != "" || other.Header.Get("Cookie") != "" {
		t.Fatal("the login was forwarded to another host")
	}

	same, _ := http.NewRequest(http.MethodGet, "https://dav.example/b", nil)
	same.Header.Set("Authorization", "Basic c2VjcmV0")
	if err := checkDAVRedirect(same, []*http.Request{secure}); err != nil {
		t.Fatal(err)
	}
	if same.Header.Get("Authorization") == "" {
		t.Fatal("a same-host redirect lost the login")
	}
}

// A conflict's kept-aside copy gets a name of its own: milliseconds tell
// two kept in one second apart, and a name already taken is never written
// over (#L14).
func TestAsideFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 10, 11, 12, 345_000_000, time.UTC)
	first := asideFile(dir, "server-damaged", now)
	if fi, err := os.Stat(first); err != nil || fi.Size() != 0 {
		t.Fatalf("the first name is not reserved: %v %v", fi, err)
	}
	if err := os.WriteFile(first, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := asideFile(dir, "server-damaged", now)
	if second == first {
		t.Fatalf("the same name %s was chosen twice", first)
	}
	if b, err := os.ReadFile(first); err != nil || string(b) != "one" {
		t.Fatalf("the first copy was written over: %q %v", b, err)
	}
	if filepath.Dir(second) != dir {
		t.Fatalf("the second name is outside the folder: %s", second)
	}
}
