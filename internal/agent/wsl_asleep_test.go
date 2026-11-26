package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// A stopped distro's row keeps what the agent is called and how its models
// are spelled, and has the callbacks a switch-off needs; the reads that
// only say what the row is never open the distro, and a catalog sync
// leaves it stopped (its files get the catalog once something wakes it).
func TestWSLAsleepKeepsItsMetadataAndCallbacks(t *testing.T) {
	live := &Agent{
		ID: "kilo", Name: "Kilo", Plugin: "kilo-spec",
		OwnVia:      func(string) string { return "" },
		SplitSuffix: func(string) (string, string, bool) { return "", "", true },
		Fields:      []Field{{Key: "model", Get: func() string { return "" }, Set: func(string) error { return nil }}},
		Joined:      func() bool { t.Error("Joined read a stopped distro's files"); return true },
		Beside:      func() bool { t.Error("Beside read a stopped distro's files"); return true },
		Routed:      func() bool { t.Error("Routed read a stopped distro's files"); return true },
		Sync:        func() error { t.Error("Sync woke a stopped distro"); return nil },
		Unwire:      func() error { return nil },
	}
	a := asleep(live, wslKind{}, distro{Name: "Stopped"})
	if a.Plugin != "kilo-spec" {
		t.Errorf("Plugin = %q", a.Plugin)
	}
	if a.SplitSuffix == nil || a.OwnVia == nil {
		t.Error("SplitSuffix or OwnVia lost")
	}
	if a.Joined() || a.Beside() || a.Routed() {
		t.Error("a stopped distro read as joined, beside or routed")
	}
	// a stopped row has no files to sync, and a catalog sync must not
	// start its distro: its own Sync is never reached
	if a.Sync != nil {
		t.Error("a stopped distro's row has a Sync")
	}
	if a.Unwire == nil {
		t.Error("no Unwire on a stopped distro's row")
	}
}

// Waking a distro reads it again: a NAT address that changed while it was
// stopped is the one its agent's config is written with, not the one owl
// remembered before it stopped.
func TestWSLAsleepWakesAtTheAddressNow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "home", "me", ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "home", "me", ".codex", "config.toml")
	if err := os.WriteFile(cfg, []byte("model = \"gpt-5.5\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeWSL(t, "Ubuntu\r\n", "Ubuntu\r\n", map[string]string{
		"Ubuntu": "home:/home/me\ndir:.codex\nbin:codex /usr/bin/codex\nroute:default via 172.20.0.1 dev eth0\n",
	}, map[string]string{"Ubuntu": root})

	// as owl remembered it while it was stopped: an address it had before
	stale := distro{Name: "Ubuntu", Root: root, Home: "/home/me",
		Has: map[string]bool{"dir:.codex": true}, Gateway: "172.20.0.9"}
	k := wslKindOf("codex")
	a := asleep(k.in(stale.place("codex@wsl:Ubuntu")), k, stale)
	if err := a.Field("model").Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "172.20.0.1") {
		t.Fatalf("the address before it stopped, not the one now:\n%s", got)
	}
	if strings.Contains(got, "172.20.0.9") {
		t.Fatalf("the stale address stayed:\n%s", got)
	}
}
