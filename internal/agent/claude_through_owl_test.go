package agent

import (
	"path/filepath"
	"testing"

	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/provider"
)

// provider.ClaudeThroughOwl, which keeps Claude Code's sign-in where the
// user put it while its requests go through the gateway (oreoft on
// Discord), is told by Claude Code's own settings.json: its
// ANTHROPIC_BASE_URL at the gateway, whether Claude Code sends owl's key
// or its claude.ai sign-in there; not at Anthropic's or with none.
func TestClaudeThroughOwlReadsItsSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path := filepath.Join(home, ".claude", "settings.json")

	if provider.ClaudeThroughOwl() {
		t.Fatal("no settings.json, and Claude Code counted routed through owl")
	}
	for _, c := range []struct {
		settings string
		want     bool
	}{
		{`{"env": {"ANTHROPIC_BASE_URL": "` + gateway.URL() + `", "ANTHROPIC_AUTH_TOKEN": "` + gateway.Token + `"}}`, true},
		// its claude.ai sign-in kept: the connectors work, and the
		// requests still go through owl
		{`{"env": {"ANTHROPIC_BASE_URL": "` + gateway.URL() + `", "ANTHROPIC_AUTH_TOKEN": ""}}`, true},
		{`{"env": {"ANTHROPIC_BASE_URL": "https://api.anthropic.com"}}`, false},
		{`{"model": "opus"}`, false},
	} {
		writeFile(t, path, c.settings)
		if got := provider.ClaudeThroughOwl(); got != c.want {
			t.Errorf("%s: routed through owl %v, want %v", c.settings, got, c.want)
		}
	}
}
