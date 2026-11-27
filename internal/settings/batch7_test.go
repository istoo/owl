package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/filelock"
)

// A save waits for another owl's write: the file's lock is taken across
// processes, so a GUI and a CLI saving at once don't read the same file
// and write over each other (#H43).
func TestSaveWaitsForAnotherOwl(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Settings{Theme: "light"}); err != nil {
		t.Fatal(err)
	}
	unlock, held := filelock.Lock(Path()+".lock", time.Second)
	if !held {
		t.Fatal("the test could not take the file's lock")
	}
	done := make(chan error, 1)
	go func() { done <- Save(Settings{Theme: "dark"}) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Save went on while another owl held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save never went on after the lock was let go")
	}
	if got := Load().Theme; got != "dark" {
		t.Fatalf("theme = %q, want dark", got)
	}
}

// A write that can't have the file's lock does not write: the save fails
// rather than run beside another owl (#H43). The lock path here is under a
// regular file, a local synthetic failure.
func TestSaveRefusesWhenTheFileLockCantBeTaken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := fileLock
	fileLock = &filelock.Mutex{Path: func() string { return filepath.Join(blocker, "settings.json.lock") }}
	t.Cleanup(func() { fileLock = old })

	if err := Save(Settings{Theme: "light"}); err == nil {
		t.Fatal("Save went on without the file's lock")
	}
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Fatalf("settings were written without the lock: %v", err)
	}
}

// The router setters are ordinary saves: they never replace local
// recording consent, which only SetGatewayConversations may (#H44).
func TestRouterSettersKeepRecordingConsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SetGatewayConversations(true); err != nil {
		t.Fatal(err)
	}
	if err := SetGatewayConversations(false); err != nil {
		t.Fatal(err)
	}
	if err := SetRouterNote("Los Angeles"); err != nil {
		t.Fatal(err)
	}
	if err := SetRouterAllTools(true); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.GatewayConversations {
		t.Fatal("a router setting turned recording on")
	}
	if s.RouterNote != "Los Angeles" || !s.RouterAllTools {
		t.Fatalf("router settings: %q, all tools %v", s.RouterNote, s.RouterAllTools)
	}
}
