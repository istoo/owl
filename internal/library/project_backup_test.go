package library

import (
	"path/filepath"
	"testing"
)

// Claude Code's own record is copied aside before owl writes a project
// into it, as every agent's file is (#L11).
func TestClaudeRecordIsKeptBeforeAProjectWrite(t *testing.T) {
	h, proj := scopeProject(t)
	claude := filepath.Join(h, ".claude.json")
	write(t, claude, `{"mcpServers":{},"projects":{}}`)
	before := read(t, claude)
	res := ok(t)(ProjectServerOff(proj, "fs", []string{"claude"}))
	if res.Backup == "" {
		t.Fatal("no backup was made")
	}
	if got := read(t, filepath.Join(res.Backup, "claude", ".claude.json")); got != before {
		t.Errorf("kept %q, want %q", got, before)
	}
	if after := read(t, claude); after == before {
		t.Error("the project wasn't written")
	}
}
