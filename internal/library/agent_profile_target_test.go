package library

import (
	"path/filepath"
	"testing"

	"github.com/istoo/owl/internal/agent"
)

// A Claude Code profile (claude#work) reads its own settings.json: the
// target has it, so the Library's per-tool off switch and the skill
// router's permissions reach it (#47).
func TestClaudeProfileTargetHasSettings(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	a := &agent.Agent{ID: "claude#work", Name: "Claude", Dir: dir}
	tg := targetOf(a)
	if tg == nil {
		t.Fatal("no target for a Claude Code profile")
	}
	if want := filepath.Join(dir, "settings.json"); tg.Settings != want {
		t.Fatalf("settings %q, want %q", tg.Settings, want)
	}
	if got, want := claudeCredentialsOf(tg), filepath.Join(dir, ".credentials.json"); got != want {
		t.Fatalf("credentials %q, want %q", got, want)
	}
	if got := toolsOffOf(tg); got != toolsClaudeDeny {
		t.Fatalf("tools off %v, want Claude's deny list", got)
	}
	if r := routerRules(tg, nil); len(r) == 0 {
		t.Fatal("the skill router wrote no rules for a Claude Code profile")
	}
}
