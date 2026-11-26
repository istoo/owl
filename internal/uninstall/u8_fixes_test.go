package uninstall

import (
	"os"
	"path/filepath"
	"testing"
)

// `owl data move` leaves a link to the chosen disk: uninstalling removes
// what the link leads to, not the link alone, or every file stays behind
// there (#79).
func TestDataFollowsAMovedFolderLink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	at := t.TempDir()
	write(t, filepath.Join(at, "settings.json"), "{}")
	write(t, filepath.Join(at, "usage", "2026-10.db"), "x")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".config", "owl")
	if err := os.Symlink(at, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	r := &Report{}
	Data(r)
	if !gone(t, at) {
		t.Fatal("the moved folder's files stayed on the other disk")
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the link is still there: %v", err)
	}
}
