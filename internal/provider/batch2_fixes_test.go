package provider

import (
	"math"
	"testing"
	"time"
)

// A plugin's other URL may go to the host its loader sends to, and to
// that host only: not over plain http, not with a userinfo hiding another
// host, not with a backslash a client may read as a path break (#C7).
func TestPluginSameOrigin(t *testing.T) {
	base := "https://api.vendor.com/v1"
	cases := []struct {
		rest string
		want bool
	}{
		{"https://api.vendor.com/other", true},
		{"https://api.vendor.com:443/other", true},
		{"https://API.VENDOR.COM/other", true},
		{"http://api.vendor.com/other", false}, // downgraded
		{"https://api.vendor.com.evil.com/x", false},
		{"https://evil.com@api.vendor.com/x", false}, // userinfo, not a host
		{"https://api.vendor.com@evil.com/x", false}, // the host is evil.com
		{"https://api.vendor.com:8443/x", false},     // another port
		{"https://api.vendor.com\\@evil.com/x", false},
		{"https://evil.com/api.vendor.com", false},
		{"://api.vendor.com/x", false},
	}
	for _, c := range cases {
		if got := sameOrigin(c.rest, base); got != c.want {
			t.Errorf("sameOrigin(%q) = %v, want %v", c.rest, got, c.want)
		}
	}
	// an http base keeps its own scheme; a https URL to it is another's
	if !sameOrigin("http://host.example/x", "http://host.example") {
		t.Error("an http base refused its own http URL")
	}
	if sameOrigin("https://host.example/x", "http://host.example") {
		t.Error("an http base took an https URL")
	}
}

// Two readings of a window with the same figures are the same point
// however the Start and ResetsAt pointers were made (#H19).
func TestSameQuotaPoint(t *testing.T) {
	at := time.Now()
	a := QuotaPoint{At: at, Left: 42, Start: quotaTimePtr(at.Add(-time.Hour)), ResetsAt: quotaTimePtr(at.Add(time.Hour))}
	b := QuotaPoint{At: at, Left: 42, Start: quotaTimePtr(at.Add(-time.Hour)), ResetsAt: quotaTimePtr(at.Add(time.Hour))}
	if !sameQuotaPoint(a, b) {
		t.Error("the same reading made twice read as different")
	}
	if sameQuotaPoint(a, QuotaPoint{At: at, Left: 41, Start: a.Start, ResetsAt: a.ResetsAt}) {
		t.Error("a different figure read as the same point")
	}
	if sameQuotaPoint(a, QuotaPoint{At: at, Left: 42, Start: quotaTimePtr(at), ResetsAt: a.ResetsAt}) {
		t.Error("a different start read as the same point")
	}
	if !sameQuotaPoint(QuotaPoint{At: at, Left: 1}, QuotaPoint{At: at, Left: 1}) {
		t.Error("two points with no times were not the same")
	}
}

func quotaTimePtr(t time.Time) *time.Time { return &t }

// A vendor's percentage is kept from 0 to 100, and a NaN or Inf is 0
// rather than written into a card (#M19).
func TestClampPercent(t *testing.T) {
	cases := map[float64]float64{
		0:            0,
		42.5:         42.5,
		100:          100,
		-3:           0,
		140:          100,
		math.NaN():   0,
		math.Inf(1):  0,
		math.Inf(-1): 0,
	}
	for in, want := range cases {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%v) = %v, want %v", in, got, want)
		}
	}
}

// A Copilot GHE account waiting for its rename, while a group is saved
// and the provider order is set, must not deadlock: the settings move
// needs providers.json's lock, and the saving holds it while the accounts
// are listed (#H8).
func TestCopilotRenameNoDeadlockWithProviderWrites(t *testing.T) {
	oldGHEFiles(t, `{"providers": [{"id": "copilot", "accountCaps": {"mona": 70}}]}`)
	loginsSeenAt.Store(0)
	done := make(chan error, 2)
	go func() {
		done <- SaveGroup(Group{ID: "g", Name: "G", Members: []string{"copilot/gpt-5"}})
	}()
	go func() {
		done <- SetOrder([]string{"copilot"})
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("a provider write: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a provider write deadlocked on the account rename")
		}
	}
}
