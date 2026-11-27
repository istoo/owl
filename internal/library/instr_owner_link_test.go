package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/istoo/owl/internal/agent"
)

// Two agents share an instructions file, one of them through a link: the
// agent switched on is found, wherever its own path to the file points
// (#L11).
func TestInstrOwnerFindsASharedFileThroughALink(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "GEMINI.md")
	write(t, shared, "text\n")
	link := filepath.Join(dir, "linked.md")
	if err := os.Symlink(shared, link); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	l := &Library{Instructions: Instructions{Agents: []string{"app"}}}
	cli := &Target{Instructions: shared, Agent: &agent.Agent{ID: "cli"}}
	app := &Target{Instructions: link, Agent: &agent.Agent{ID: "app"}}
	if got := l.instrOwner(cli, []*Target{cli, app}); got != "app" {
		t.Errorf("instrOwner = %q, want app", got)
	}
	// with the other switched on too, the first of them found is the owner
	l.Instructions.Agents = []string{"cli", "app"}
	if got := l.instrOwner(app, []*Target{cli, app}); got != "cli" {
		t.Errorf("instrOwner = %q, want cli", got)
	}
}
