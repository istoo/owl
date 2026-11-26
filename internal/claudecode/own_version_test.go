package claudecode

import "testing"

// A machine whose only Claude Code is where its installer put it, not
// first on PATH, runs it: Own passed it over and owl ran nothing (#15).
func TestOwnFindsTheOnlyOneWhenPathHasNone(t *testing.T) {
	was := own
	own.first = func() string { return "" }
	own.all = func() []string { return []string{"/opt/claude/bin/claude"} }
	own.read = func(string) string { return "2.1.0" }
	own.version = func(string) string { return "" }
	t.Cleanup(func() { own = was })

	if got := Own(); got != "/opt/claude/bin/claude" {
		t.Errorf("Own() = %q, want the one found at /opt/claude/bin/claude", got)
	}
}

// A prerelease comes before its release, not after it (#19).
func TestNewerSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.1.0-rc1", "2.1.0", false},
		{"2.1.0", "2.1.0-rc1", true},
		{"2.1.0-rc2", "2.1.0-rc1", true},
		{"2.1.0", "2.1.0", false},
		{"2.1.1", "2.1.0", true},
		{"2.2.0", "2.10.0", false},
		{"2.1.0-alpha.2", "2.1.0-alpha.10", false},
		{"2.1.0-alpha.10", "2.1.0-alpha.2", true},
		{"2.1.0-alpha.2", "2.1.0-alpha", true},
		{"2.1.0-beta", "2.1.0-alpha", true},
		{"2.1.0-1", "2.1.0-alpha", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
