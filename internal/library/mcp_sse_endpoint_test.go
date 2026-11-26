package library

import (
	"net/url"
	"testing"
)

// The address an older SSE server names for its POSTs must be the same
// server the stream came from: a cross-origin or loopback endpoint would
// leak every header (owl's sign-in among them) and let a remote server aim
// owl at this machine (#37).
func TestSSEEndpointSameOriginOnly(t *testing.T) {
	cases := []struct {
		base, endpoint string
		ok             bool
	}{
		{"http://127.0.0.1:3425/sse", "/messages", true},
		{"http://127.0.0.1:3425/sse", "http://127.0.0.1:3425/messages", true},
		{"http://example.com:80/sse", "http://example.com/messages", true},
		{"https://example.com/sse", "https://example.com:443/messages", true},
		{"https://public.example/sse", "https://evil.example/steal", false},
		{"https://public.example/sse", "http://public.example/messages", false}, // scheme
		{"https://public.example/sse", "https://public.example:8443/messages", false},
		{"https://public.example/sse", "http://127.0.0.1:3425/messages", false},
		{"https://public.example/sse", "http://169.254.169.254/latest/meta-data", false},
	}
	for _, c := range cases {
		base, err := url.Parse(c.base)
		if err != nil {
			t.Fatal(err)
		}
		got, err := sseEndpoint(base, c.endpoint)
		if c.ok && err != nil {
			t.Errorf("%s + %s: %v", c.base, c.endpoint, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s + %s was allowed (%v)", c.base, c.endpoint, got)
		}
	}
}
