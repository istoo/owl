package mcpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// remoteServer makes every server look like one on the open internet, so
// the checks that only apply there run: set for the test, and restored.
func remoteServer(t *testing.T) {
	t.Helper()
	old := serverLocal
	serverLocal = func(context.Context, string) bool { return false }
	t.Cleanup(func() { serverLocal = old })
}

// A server on the open internet may not send owl's sign-in to this machine,
// a private network or a metadata endpoint: that would make the backend
// probe what only the server can reach (#38).
func TestSignTargetRefusesPrivateForRemoteServer(t *testing.T) {
	remoteServer(t)
	const server = "https://mcp.example.com/mcp"
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://127.0.0.1:9999/token",
		"http://10.0.0.5/register",
		"http://192.168.1.2/",
		"http://[::1]/token",
		"file:///etc/passwd",
		"ftp://evil.example/token",
	} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetAllowed(context.Background(), server, u); err == nil {
			t.Errorf("%s was allowed", target)
		}
	}
	// a public address is fine
	u, _ := url.Parse("https://8.8.8.8/token")
	if err := targetAllowed(context.Background(), server, u); err != nil {
		t.Errorf("a public token endpoint was refused: %v", err)
	}
}

// A server on this machine or the user's own network may name loopback and
// private addresses: that is where it runs (a local MCP server, a test's
// httptest server).
func TestSignTargetAllowsLocalForLocalServer(t *testing.T) {
	old := serverLocal
	serverLocal = func(context.Context, string) bool { return true }
	t.Cleanup(func() { serverLocal = old })
	for _, target := range []string{"http://127.0.0.1:9999/token", "http://192.168.1.2/register"} {
		u, _ := url.Parse(target)
		if err := targetAllowed(context.Background(), "http://127.0.0.1:3425/mcp", u); err != nil {
			t.Errorf("%s was refused for a local server: %v", target, err)
		}
	}
}

// Discovery refuses a resource metadata a remote server names on this
// machine, and one that isn't a web address at all.
func TestDiscoverRefusesPrivateMetadata(t *testing.T) {
	remoteServer(t)
	for _, metadata := range []string{
		"http://127.0.0.1:9999/meta",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	} {
		t.Run(metadata, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadata+`"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()
			_, err := discover(context.Background(), srv.URL+"/mcp")
			if err == nil {
				t.Fatalf("discovered through %s", metadata)
			}
			if !strings.Contains(err.Error(), "private network") && !strings.Contains(err.Error(), "web address") {
				t.Fatalf("error %v", err)
			}
		})
	}
}

// A redirect the sign-in's client follows is checked too, so a public
// endpoint can't bounce owl to this machine.
func TestSignRedirectRefusesPrivate(t *testing.T) {
	remoteServer(t)
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1:9999", Path: "/token"}}
	c := clientFor(context.Background(), "https://mcp.example.com/mcp")
	if c.CheckRedirect(req, nil) == nil {
		t.Fatal("a redirect to loopback was followed")
	}
}
