package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/appdir"
)

// `owl data move` marks the folder it moved into and leaves a link to it:
// uninstalling removes what the link leads to, not the link alone, or every
// file stays behind there (#79, #H48).
func TestDataFollowsAMovedFolderLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cfg, cache := appdir.Config(), appdir.Cache()
	write(t, filepath.Join(cfg, "settings.json"), "{}")
	write(t, filepath.Join(cache, "calls", "1.json"), "x")
	disk := filepath.Join(home, "disk")
	if err := os.MkdirAll(disk, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := appdir.Target(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := appdir.Move(root); err != nil {
		t.Fatal(err)
	}
	at := filepath.Join(root, "config")
	if !appdir.MovedOwns(at) {
		t.Fatal("the move did not mark the folder as owl's")
	}

	r := &Report{}
	Data(r)
	if !gone(t, at) {
		t.Fatal("the moved folder's files stayed on the other disk")
	}
	if _, err := os.Lstat(cfg); !os.IsNotExist(err) {
		t.Fatalf("the link is still there: %v", err)
	}
}

// A link made by hand to an ordinary folder is not owl's: uninstalling
// removes the link and leaves every file where it is (#H48).
func TestDataLeavesAHandMadeLinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	personal := filepath.Join(home, "dotfiles")
	write(t, filepath.Join(personal, "notes.md"), "mine")
	link := filepath.Join(home, ".config", "owl")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(personal, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	r := &Report{}
	Data(r)
	if _, err := os.Stat(filepath.Join(personal, "notes.md")); err != nil {
		t.Fatalf("Data deleted a hand-made link's files: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the link is still there: %v", err)
	}
	kept := false
	for _, d := range r.Kept {
		if strings.Contains(d.Err, "only the link was removed") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("no kept note was reported: %+v", r.Kept)
	}
}
