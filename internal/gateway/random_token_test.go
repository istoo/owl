package gateway

import "testing"

// L4: a callback token is random, not a clock value another process can
// guess, and long enough to be one.
func TestRandomTokenIsRandom(t *testing.T) {
	a, b := randomToken(), randomToken()
	if a == b {
		t.Fatal("two callback tokens are the same")
	}
	if len(a) < 20 {
		t.Fatalf("a token of %d characters is too short", len(a))
	}
	for _, r := range a {
		if !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') {
			t.Fatalf("a token with %q isn't crypto/rand.Text's", r)
		}
	}
}
