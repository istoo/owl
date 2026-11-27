package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A trash note's paths belong to the session it names: inside the folders
// Restore checks a trash note's paths by the shape they have below a
// folder the agent's own listing reads, not by a piece of the home folder:
// a session file, its side files and a subagent's (in another project's
// folder too, or a Claude copy with its own id) fit; another agent's folder
// or a config file at a session folder's root does not. A symlinked agent
// folder is resolved on both sides.
func TestRestorablePathsBelongToTheAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	real := filepath.Join(t.TempDir(), "real-claude")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "claude-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", link)
	if err := os.MkdirAll(filepath.Join(real, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "offline"), filepath.Join(real, "projects", "unavailable")); err != nil {
		t.Fatal(err)
	}
	id := "11111111-2222-3333-4444-555555555555"
	sub := "22222222-2222-3333-4444-555555555555"
	under := func(parts ...string) string { return filepath.Join(append([]string{link}, parts...)...) }
	one := func(p string) []moved { return []moved{{From: p, Name: "0-x"}} }
	for _, c := range []struct {
		name  string
		agent string
		items []moved
		want  bool
	}{
		{"session file", "claude", one(under("projects", "p", id+".jsonl")), true},
		{"已断开的项目目录链接", "claude", one(under("projects", "unavailable", id+".jsonl")), false},
		{"a copy with its own id", "claude", one(under("projects", "elsewhere", sub+".jsonl")), true},
		{"side file", "claude", one(under("file-history", id)), true},
		{"subagent inside it", "claude", one(under("projects", "p", id, "subagents", "agent-a1.jsonl")), true},
		{"home, not the agent's", "claude", one(filepath.Join(home, ".ssh", id)), false},
		{"another agent's folder", "claude", one(filepath.Join(home, ".codex", "sessions", id+".jsonl")), false},
		{"a config file at its root", "claude", one(under("settings.json")), false},
		{"projects, no session below", "claude", one(under("projects", "p")), false},
		{"relative", "claude", []moved{{From: "projects/" + id + ".jsonl", Name: "0-x"}}, false},
		{"no files, not forgotten", "claude", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := restorablePaths(c.agent, c.items, false); got != c.want {
				t.Fatalf("restorable = %v, want %v", got, c.want)
			}
		})
	}
	if !restorablePaths("claude", nil, true) {
		t.Fatal("a forgotten agent's session, which moves no file, isn't restorable")
	}
	// an external folder the user set is still the agent's own
	ext := t.TempDir()
	t.Setenv("CODEX_HOME", ext)
	codex := filepath.Join(ext, "sessions", "2026", "01", "01", "rollout-2026-01-01T00-00-00-"+id+".jsonl")
	if !restorablePaths("codex", one(codex), false) {
		t.Fatal("a Codex folder the user set is not restorable")
	}
	if restorablePaths("codex", one(filepath.Join(ext, "config.toml")), false) {
		t.Fatal("Codex's config file is restorable as a session")
	}
	// a dsh subagent's folder in another project's folder
	dsh := t.TempDir()
	t.Setenv("DSH_HOME", dsh)
	subDir := filepath.Join(dsh, "sessions", "--another-project--", "session-"+sub)
	if !restorablePaths("dsh", one(subDir), false) {
		t.Fatal("a dsh subagent's folder in another project is not restorable")
	}
	if restorablePaths("dsh", one(filepath.Join(dsh, "storages", "other", sub+".json")), false) {
		t.Fatal("a file outside dsh's session cache is restorable")
	}
	// a subagent's files are beside the session's, named as sessions are
	factory := t.TempDir()
	t.Setenv("FACTORY_HOME_OVERRIDE", factory)
	dir := filepath.Join(factory, ".factory", "sessions", "-work")
	items := []moved{
		{From: filepath.Join(dir, id+".jsonl"), Name: "0-a"},
		{From: filepath.Join(dir, sub+".jsonl"), Name: "1-b"},
		{From: filepath.Join(dir, sub+".settings.json"), Name: "2-c"},
	}
	if !restorablePaths("droid", items, false) {
		t.Fatal("a subagent's files beside the session's are not restorable")
	}
}

// A rollback that can't put a file back must not remove the folder holding
// the only copy: it stays, with a note naming it, and the error says so.
func TestUndoDeleteKeepsACopyItCannotPutBack(t *testing.T) {
	trash := t.TempDir()
	if err := os.MkdirAll(filepath.Join(trash, "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trash, "files", "0-a"), []byte("only copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := os.Chmod(dest, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dest, 0o700) })
	t0 := Trashed{Agent: "claude", ID: "11111111-2222-3333-4444-555555555555",
		Items: []moved{{From: filepath.Join(dest, "a.jsonl"), Name: "0-a"}}}
	if err := undoDelete(trash, t0, errors.New("the move failed")); err == nil {
		t.Fatal("a rollback that failed was reported as done")
	}
	if b, err := os.ReadFile(filepath.Join(trash, "files", "0-a")); err != nil || string(b) != "only copy" {
		t.Fatalf("the only copy is gone: %v", err)
	}
	var note Trashed
	b, err := os.ReadFile(filepath.Join(trash, manifest))
	if err != nil || json.Unmarshal(b, &note) != nil || len(note.Items) != 1 || note.Items[0].Name != "0-a" {
		t.Fatalf("what is left isn't noted: %v %+v", err, note)
	}
}

// When every file is back the folder goes, and the error says why Delete
// was undone.
func TestUndoDeleteRemovesWhenAllAreBack(t *testing.T) {
	trash := t.TempDir()
	if err := os.MkdirAll(filepath.Join(trash, "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trash, "files", "0-a"), []byte("only copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	from := filepath.Join(dest, "a.jsonl")
	t0 := Trashed{Agent: "claude", ID: "11111111-2222-3333-4444-555555555555",
		Items: []moved{{From: from, Name: "0-a"}}}
	err := undoDelete(trash, t0, errors.New("the note failed"))
	if err == nil || !strings.Contains(err.Error(), "the note failed") {
		t.Fatalf("the error lost why: %v", err)
	}
	if b, err := os.ReadFile(from); err != nil || string(b) != "only copy" {
		t.Fatalf("the file isn't back: %v", err)
	}
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Fatalf("the emptied trash folder stayed: %v", err)
	}
}

// When no term can be asked of the index, it says so instead of returning
// an empty set: the caller then reads every session, rather than believing
// that nothing matches.
func TestSaidKeysAllTermsFailed(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys, filtered, err := saidKeys(context.Background(), db, []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered || keys != nil {
		t.Fatalf("keys %v, filtered %v: a broken index must fall back to the files", keys, filtered)
	}
}

// open waits for a refresh reading the db it is about to close.
func TestSaidOpenWaitsForRefresh(t *testing.T) {
	dir := t.TempDir()
	x := &saidIndex{
		where: func() string { return filepath.Join(dir, "said.db") },
		list:  func() []saidSession { return nil },
	}
	run := make(chan struct{})
	x.mu.Lock()
	x.running = run
	x.mu.Unlock()
	go func() {
		time.Sleep(50 * time.Millisecond)
		x.mu.Lock()
		x.running = nil
		x.mu.Unlock()
		close(run)
	}()
	start := time.Now()
	db, err := x.open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if waited := time.Since(start); waited < 40*time.Millisecond {
		t.Fatalf("open returned in %v, before the refresh left", waited)
	}
}

// A row whose columns are none the table has is skipped, not put into an
// INSERT with no columns (which SQLite refuses and used to fail the whole
// restore).
func TestRestoreInSkipsRowsWithNoKnownColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := sql.Open("sqlite", sqliteURL(path, "_pragma=busy_timeout(5000)"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE local_thread_catalog (host_id TEXT, thread_id TEXT, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := "x"
	rows := []codexCatalogRow{{DB: path, Cols: []string{"not_a_column"}, Vals: []sqlValue{{S: &s}}}}
	if err := restoreIn(path, rows); err != nil {
		t.Fatalf("a row with no known column failed the restore: %v", err)
	}
}
