package mcpauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/istoo/owl/internal/settings"
)

func homeSandbox(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
}

// signInVia has every sign-in of the test go through proxy.
func signInVia(t *testing.T, proxy *url.URL) {
	t.Helper()
	old := signInProxyFor
	t.Cleanup(func() { signInProxyFor = old })
	signInProxyFor = func(*http.Request) (*url.URL, error) { return proxy, nil }
}

// resolveNames answers the test's own names, so a fixture needs no DNS.
func resolveNames(t *testing.T, names map[string]string) {
	t.Helper()
	old := signInLookup
	t.Cleanup(func() { signInLookup = old })
	signInLookup = func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		if ip, ok := names[host]; ok {
			return []netip.Addr{netip.MustParseAddr(ip)}, nil
		}
		return old(ctx, network, host)
	}
}

// trustCert has a proxied sign-in's TLS check trust cert's CA, the way a
// system's roots would.
func trustCert(t *testing.T, cert *x509.Certificate) {
	t.Helper()
	old := signInTLSConfig
	t.Cleanup(func() { signInTLSConfig = old })
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	signInTLSConfig = func(name string) *tls.Config {
		return &tls.Config{ServerName: name, RootCAs: roots}
	}
}

// tlsOrigin is an HTTPS server the test's CA signs, which shows the test
// every request it serves.
func tlsOrigin(t *testing.T, seen func(*http.Request)) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			seen(r)
		}
		io.WriteString(w, "ok")
	}))
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

// connectProxy answers CONNECT by tunnelling to addr, and records the
// authority each CONNECT names.
func connectProxy(t *testing.T, addr string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var hosts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", addr)
		if err != nil {
			conn.Close()
			return
		}
		io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go io.Copy(up, conn)
		io.Copy(conn, up)
		up.Close()
		conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hosts...)
	}
}

// httpsSignIn is one https sign-in fixture: an origin the test's own CA
// signs, reached through a local proxy, with the sign-in's names answered
// by the test. It records what the proxy was asked to connect to and what
// the origin was told.
type httpsSignIn struct {
	tr      http.RoundTripper
	connect func() []string
	mu      sync.Mutex
	served  string // the TLS name the origin's certificate was checked for
	host    string // the Host header inside the tunnel
}

func newHTTPSignIn(t *testing.T, names map[string]string, trust bool) *httpsSignIn {
	t.Helper()
	resolveNames(t, names)
	f := &httpsSignIn{}
	origin := tlsOrigin(t, func(r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.TLS != nil {
			f.served = r.TLS.ServerName
		}
		f.host = r.Host
	})
	if trust {
		trustCert(t, origin.Certificate())
	}
	ou, _ := url.Parse(origin.URL)
	proxy, hosts := connectProxy(t, ou.Host)
	f.connect = hosts
	pu, _ := url.Parse(proxy.URL)
	signInVia(t, pu)
	f.tr = protectedTransport()
	return f
}

func (f *httpsSignIn) seen() (served, host string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served, f.host
}

// A sign-in to a server on the open internet dials a public address only,
// checked on the socket's own peer, so a name that resolved to a public
// address when it was checked and a private one when it is dialled (DNS
// rebinding) is refused (#H28).
func TestProtectedTransportRefusesALoopbackPeer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := protectedTransport().RoundTrip(req)
	if err == nil {
		res.Body.Close()
		t.Fatalf("a loopback peer was dialled: %s", srv.URL)
	}
	if !errors.Is(err, errPrivatePeer) {
		t.Errorf("err = %v, want errPrivatePeer", err)
	}
}

// A remote server's sign-in gets the guarded transport; a local one keeps
// the client's own (#H28).
func TestClientForGuardsARemoteServer(t *testing.T) {
	old := Client
	Client = &http.Client{Timeout: 5 * time.Second, Transport: signInTransport{proxied()}}
	t.Cleanup(func() { Client = old })
	ours := Client.Transport
	if c := clientFor(context.Background(), "https://mcp.example.com/mcp"); c.Transport == ours {
		t.Error("a remote server's sign-in keeps the unguarded transport")
	}
	oldLocal := serverLocal
	serverLocal = func(context.Context, string) bool { return true }
	t.Cleanup(func() { serverLocal = oldLocal })
	if c := clientFor(context.Background(), "http://127.0.0.1:1/mcp"); c.Transport != ours {
		t.Error("a local server's sign-in was guarded away from loopback")
	}
}

// A name a proxy is asked for is judged by every answer here, and a
// fake-IP resolver's answer is not one a proxy can be pinned to (#H28).
func TestPublicAddrs(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "198.18.0.1"} {
		if _, err := publicAddrs(context.Background(), host); err == nil {
			t.Errorf("%s was public", host)
		}
	}
	if addrs, err := publicAddrs(context.Background(), "8.8.8.8"); err != nil || len(addrs) != 1 {
		t.Errorf("a public address was refused: %v %v", addrs, err)
	}
}

// A renewal without the file lock (another owl has it, here because it
// can't be made) is refused, never made unlocked (#M36a).
func TestRenewRefusesWithoutTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder permissions don't keep a file from being made")
	}
	homeSandbox(t)
	if err := update(func(m map[string]*Record) bool {
		m["srv"] = &Record{URL: "http://127.0.0.1:1/mcp", TokenURL: "http://127.0.0.1:1/token",
			ClientID: "c", Access: "old", Refresh: "r", Expires: time.Now().Add(-time.Hour).UnixMilli()}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	dir := settings.Dir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := renew(context.Background(), "srv", func(Record) bool { return true }); err == nil {
		t.Fatal("a sign-in was renewed without the lock")
	} else if !strings.Contains(err.Error(), "renewing") {
		t.Errorf("err = %v", err)
	}
}

// A sign-in started again for the same server, or the same callback port,
// ends the one still waiting and frees its listener: the port was
// otherwise held for the whole sign-in timeout (#M36b).
func TestStopFlowsFreesItsCallbackPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	port := ln.Addr().(*net.TCPAddr).Port
	id := "test-stop-" + randomToken(4)
	f := &flow{done: make(chan struct{}), name: "srv", port: port, st: State{ID: id}, srv: &http.Server{Handler: http.NewServeMux()}}
	flows.Lock()
	flows.m[id] = f
	flows.Unlock()
	t.Cleanup(func() {
		flows.Lock()
		delete(flows.m, id)
		flows.Unlock()
	})
	go f.srv.Serve(ln)

	stopFlows("srv", 0)
	if st := f.status(); st.State != "failed" {
		t.Errorf("the old sign-in is %q", st.State)
	}
	// the serving goroutine may only just run and see the closed server
	deadline := time.Now().Add(3 * time.Second)
	for {
		again, err := net.Listen("tcp", addr)
		if err == nil {
			again.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the callback port is still held: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A flow waiting for another server and port is left alone (#M36b).
func TestStopFlowsLeavesAnotherServer(t *testing.T) {
	id := "test-keep-" + randomToken(4)
	f := &flow{done: make(chan struct{}), name: "other", port: 1, st: State{ID: id, State: "waiting"}}
	flows.Lock()
	flows.m[id] = f
	flows.Unlock()
	t.Cleanup(func() {
		flows.Lock()
		delete(flows.m, id)
		flows.Unlock()
	})
	stopFlows("srv", 2)
	if st := f.status(); st.State != "waiting" {
		t.Errorf("another server's sign-in was ended: %q", st.State)
	}
}

// A proxy elsewhere is refused before anything is asked of it: owl can't
// confirm what address it would connect to (#H28).
func TestProtectedTransportRefusesAnExternalProxy(t *testing.T) {
	old := signInProxyFor
	t.Cleanup(func() { signInProxyFor = old })
	signInProxyFor = func(*http.Request) (*url.URL, error) {
		return url.Parse("http://8.8.8.8:3128")
	}
	req, err := http.NewRequest(http.MethodGet, "http://8.8.8.8/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protectedTransport().RoundTrip(req); err == nil || !strings.Contains(err.Error(), "can't confirm") {
		t.Fatalf("an external proxy was used: %v", err)
	}
}

// A proxy on this computer is dialled for its own request only: the same
// address is no exemption for the next request, which must still be a
// public peer (#H28). A plain-http target through it is an IP literal, and
// the proxy's request line carries that address, not a name (#H28).
func TestProtectedTransportDialsOnlyTheRequestsProxy(t *testing.T) {
	var seen, host string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, host = r.URL.String(), r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	pu, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	old := signInProxyFor
	t.Cleanup(func() { signInProxyFor = old })
	calls := 0
	signInProxyFor = func(*http.Request) (*url.URL, error) {
		calls++
		if calls == 1 {
			return pu, nil
		}
		return nil, nil // the next request goes direct
	}
	tr := protectedTransport()
	req, err := http.NewRequest(http.MethodGet, "http://8.8.8.8/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("a local proxy was refused: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || seen != "http://8.8.8.8/mcp" || host != "8.8.8.8" {
		t.Errorf("the proxy saw %q (Host %q), status %d", seen, host, res.StatusCode)
	}
	// the proxy's address is not excused for another request
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer loop.Close()
	req2, _ := http.NewRequest(http.MethodGet, loop.URL, nil)
	if res, err := tr.RoundTrip(req2); err == nil {
		res.Body.Close()
		t.Fatal("a direct loopback peer was dialled after a proxied request")
	} else if !errors.Is(err, errPrivatePeer) {
		t.Errorf("err = %v, want errPrivatePeer", err)
	}
}

// A proxy is dialled at an address resolved here, which must be one of
// this computer's or the user's network: never a name dialled again, and
// never a fake-IP answer (#H28).
func TestPinnedProxyAddr(t *testing.T) {
	for host, want := range map[string]string{
		"127.0.0.1": "127.0.0.1:3128", "10.1.2.3": "10.1.2.3:3128",
		"192.168.1.2": "192.168.1.2:3128", "::1": "[::1]:3128",
	} {
		u, _ := url.Parse("http://" + net.JoinHostPort(host, "3128"))
		got, err := pinnedProxyAddr(context.Background(), u)
		if err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", host, got, err, want)
		}
	}
	u, _ := url.Parse("http://locALHOST:3128")
	if got, err := pinnedProxyAddr(context.Background(), u); err != nil || got != "127.0.0.1:3128" {
		t.Errorf("localhost: %q %v", got, err)
	}
	u, _ = url.Parse("http://127.0.0.1")
	if got, err := pinnedProxyAddr(context.Background(), u); err != nil || got != "127.0.0.1:80" {
		t.Errorf("default port: %q %v", got, err)
	}
	for _, host := range []string{"8.8.8.8", "1.1.1.1", "198.18.0.1"} {
		u, _ := url.Parse("http://" + net.JoinHostPort(host, "3128"))
		if got, err := pinnedProxyAddr(context.Background(), u); err == nil {
			t.Errorf("%s was pinned to %q", host, got)
		}
	}
}

// A proxy mode whose target can't be pinned (SOCKS sends the name) is
// refused with a message rather than let through (#H28).
func TestProtectedTransportRefusesAnUnconfirmableProxyMode(t *testing.T) {
	old := signInProxyFor
	t.Cleanup(func() { signInProxyFor = old })
	signInProxyFor = func(*http.Request) (*url.URL, error) {
		return url.Parse("socks5://127.0.0.1:1080")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://8.8.8.8/mcp", nil)
	if _, err := protectedTransport().RoundTrip(req); err == nil || !strings.Contains(err.Error(), "socks5") {
		t.Fatalf("a socks proxy was used: %v", err)
	}
}

// A plain-http sign-in to a name through a proxy is refused: the library
// writes the proxy's request line from the Host header, so the address the
// proxy connects to and the host it serves can't both be carried, and the
// proxy would look the name up itself (#H28).
func TestProtectedTransportRefusesPlainHTTPToAName(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	signInVia(t, pu)
	req, _ := http.NewRequest(http.MethodGet, "http://oauth.example/mcp", nil)
	if _, err := protectedTransport().RoundTrip(req); err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Fatalf("plain http to a name through a proxy was taken: %v", err)
	}
	mu.Lock()
	n := hits
	mu.Unlock()
	if n != 0 {
		t.Errorf("the proxy was asked %d times before the sign-in was refused", n)
	}
}

// An https sign-in through a proxy is carried: CONNECT names the pinned
// address the sign-in resolved, the tunnel's Host header keeps the name,
// and the certificate is checked for that name against the system's roots
// (#H28).
func TestProtectedTransportCarriesHTTPSThroughAProxy(t *testing.T) {
	f := newHTTPSignIn(t, map[string]string{"example.com": "8.8.8.8"}, true)
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/mcp", nil)
	res, err := f.tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("an https sign-in through a local proxy was refused: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Errorf("status %d, body %q", res.StatusCode, body)
	}
	if hosts := f.connect(); len(hosts) != 1 || hosts[0] != "8.8.8.8:443" {
		t.Errorf("CONNECT named %v, want the pinned address 8.8.8.8:443", hosts)
	}
	served, host := f.seen()
	if served != "example.com" {
		t.Errorf("the certificate was checked for %q, want the sign-in's name example.com", served)
	}
	if host != "example.com" {
		t.Errorf("the origin's Host header was %q, want example.com", host)
	}
}

// A certificate the system's roots don't vouch for is still refused: the
// pinned address never stands in for the name (#H28).
func TestProtectedTransportRefusesAnUntrustedCertificate(t *testing.T) {
	f := newHTTPSignIn(t, map[string]string{"example.com": "8.8.8.8"}, false)
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/mcp", nil)
	if res, err := f.tr.RoundTrip(req); err == nil {
		res.Body.Close()
		t.Fatal("an untrusted certificate was accepted")
	} else if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("err = %v, want a certificate error", err)
	}
	if hosts := f.connect(); len(hosts) != 1 || hosts[0] != "8.8.8.8:443" {
		t.Errorf("CONNECT named %v, want the pinned address 8.8.8.8:443", hosts)
	}
}

// The certificate is checked for the name the sign-in asks for, never for
// the pinned address CONNECT carries (#H28).
func TestProtectedTransportChecksTheCertificateForTheRequestsName(t *testing.T) {
	f := newHTTPSignIn(t, map[string]string{"other.example": "8.8.8.8"}, true)
	req, _ := http.NewRequest(http.MethodGet, "https://other.example/mcp", nil)
	if res, err := f.tr.RoundTrip(req); err == nil {
		res.Body.Close()
		t.Fatal("a certificate for example.com was accepted for other.example")
	} else if !strings.Contains(err.Error(), "other.example") {
		t.Errorf("err = %v, want a name mismatch naming other.example", err)
	}
}

// A proxied sign-in request keeps no connection after it: each request has
// its own transport, with keep-alives off, so nothing outlives the request
// or serves the next one (#H28).
func TestProtectedTransportKeepsNoProxyConnection(t *testing.T) {
	var mu sync.Mutex
	var made, closed int
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	proxy.Config.ConnState = func(c net.Conn, st http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch st {
		case http.StateNew:
			made++
		case http.StateClosed:
			closed++
		}
	}
	proxy.Start()
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	signInVia(t, pu)
	tr := protectedTransport()
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://8.8.8.8/mcp", nil)
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		m, c := made, closed
		mu.Unlock()
		if m == 2 && c == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxy connections: %d made, %d closed: a sign-in kept or reused one", m, c)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A direct connection is made to a public address only: a fake-IP answer
// is refused too, since only the user's TUN knows where it leads (#H28).
func TestGuardPeer(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "10.0.0.5:443", "[::1]:443", "198.18.0.1:443", "169.254.169.254:80"} {
		if err := guardPeer(address); !errors.Is(err, errPrivatePeer) {
			t.Errorf("%s: %v", address, err)
		}
	}
	if err := guardPeer("8.8.8.8:443"); err != nil {
		t.Errorf("a public peer was refused: %v", err)
	}
}
