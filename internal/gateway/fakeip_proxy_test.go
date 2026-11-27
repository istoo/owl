package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
)

// H1: an answer in 198.18.0.0/15 handed to a proxy on this computer is
// taken as a fake-IP resolver's only for a host the user named itself
// (fetchPrivate, or a vendor's own host). A public A record can be
// 198.18.0.1 as well, and the proxy resolves the real name where owl
// can't see, to whatever it says (a service only this computer reaches).
// Another name is never looked up to guess: how a resolver answers it
// says nothing about where this one goes, and the lookup sits on every
// download.
func TestFakeAnswerThroughLocalProxyOnlyForANamedHost(t *testing.T) {
	fresh(t)
	proxy, asked := fakeProxy(t, "reached")
	useProxy(t, proxy.URL)
	answers(t, map[string][]string{
		"attacker.test": {"198.18.0.1"},
		"range.test":    {"198.18.0.2"}, // its address range is allowed, not its name
		"named.test":    {"198.18.0.3"},
	})
	if err := guardedGet("http://attacker.test/x.png"); !errors.Is(err, errPrivateAddress) {
		t.Fatalf("a published 198.18 address through a local proxy: %v", err)
	}
	if a := asked(); len(a) != 0 {
		t.Fatalf("the proxy was asked for the name: %q", a)
	}
	// an allowed range covers the fake address, but the proxy dials the
	// name, which owl never checks: the range is not the destination
	allowFetch(t, "198.18.0.0/15")
	if err := guardedGet("http://range.test/x.png"); !errors.Is(err, errPrivateAddress) {
		t.Fatalf("an allowed range under a name the proxy resolves: %v", err)
	}
	if a := asked(); len(a) != 0 {
		t.Fatalf("the proxy was asked for a name whose address alone was allowed: %q", a)
	}
	// the host named itself: the name is what the user authorized, and
	// the proxy is given it, as before #2593
	allowFetch(t, "named.test")
	if err := guardedGet("http://named.test/x.png"); err != nil {
		t.Fatalf("a named host in fake-IP mode: %v", err)
	}
	if a := asked(); len(a) != 1 || a[0] != "http://named.test:80/x.png Host=named.test:80" {
		t.Fatalf("the proxy was asked %q, want the name", a)
	}
}

// No proxy also needs a name or an address the user gave: owl can't tell
// a fake-IP resolver's TUN from whatever the OS routes 198.18.0.0/15 to,
// so an un-named fake answer is refused rather than dialled (H1). Named,
// the pinned fake address is dialled, as #2593's TUN needs.
func TestFakeAnswerWithNoProxyNeedsANamedHost(t *testing.T) {
	fresh(t)
	useProxy(t, "direct")
	answers(t, map[string][]string{
		"unnamed.test": {"198.18.0.7"},
		"named.test":   {"198.18.0.8"},
	})
	var dialled []string
	was := dialPinned
	dialPinned = func(_ context.Context, _ *net.Dialer, _, addr string) (net.Conn, error) {
		dialled = append(dialled, addr)
		return nil, errors.New("the test's dial")
	}
	t.Cleanup(func() { dialPinned = was })
	if err := guardedGet("http://unnamed.test/x.png"); !errors.Is(err, errPrivateAddress) {
		t.Fatalf("an un-named fake answer with no proxy: %v", err)
	}
	if len(dialled) != 0 {
		t.Fatalf("an un-named fake destination was dialled: %v", dialled)
	}
	allowFetch(t, "named.test")
	if err := guardedGet("http://named.test/x.png"); errors.Is(err, errPrivateAddress) {
		t.Fatalf("a named fake answer with no proxy: %v", err)
	}
	if len(dialled) != 1 || dialled[0] != "198.18.0.8:80" {
		t.Fatalf("dialled %q, want the named host's pinned fake address", dialled)
	}
}
