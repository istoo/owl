package appdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// migrate never writes over or removes owl's own folder when it is a link:
// its files live on another disk (owl data move), which may be unmounted,
// and the link, broken or not, is the user's (#H47).
func TestMigrateLeavesALinkedFolderAlone(t *testing.T) {
	for _, broken := range []bool{false, true} {
		name := "mounted"
		if broken {
			name = "unmounted"
		}
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("symlinks need a privilege here")
			}
			home := root(t)
			src := filepath.Join(home, ".config", legacyUpName)
			touch(t, filepath.Join(src, "providers.json"))
			dst := filepath.Join(home, ".config", "owl")
			target := filepath.Join(home, "external", "config")
			if !broken {
				if err := os.MkdirAll(target, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, dst); err != nil {
				t.Skipf("no symlinks here: %v", err)
			}
			if got := migrate(src, dst); got != "" {
				t.Fatalf("migrated from %q over a link", got)
			}
			fi, err := os.Lstat(dst)
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("the link is gone: %v %v", fi, err)
			}
			to, err := os.Readlink(dst)
			if err != nil || to != target {
				t.Fatalf("the link now points at %q (%v), want %q", to, err, target)
			}
			if !broken {
				if es, err := os.ReadDir(target); err != nil || len(es) != 0 {
					t.Fatalf("the linked folder was written into: %v %v", es, err)
				}
			}
		})
	}
}

// Move puts the settings folder back when its cache can't move: half a
// move leaves owl reading one folder here and the other there (#M41).
func TestMovePutsTheSettingsFolderBack(t *testing.T) {
	r := t.TempDir()
	home := filepath.Join(r, "home")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	cfg, cache := Config(), Cache()
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "providers.json"), []byte("providers"), 0o600); err != nil {
		t.Fatal(err)
	}
	// the cache is a file: moving it can't work
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	disc := filepath.Join(r, "disc")
	if err := os.MkdirAll(disc, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := Target(disc)
	if err != nil {
		t.Fatal(err)
	}
	if err := Move(root); err == nil {
		t.Fatal("Move succeeded with a cache that isn't a folder")
	}
	if _, linked := linkOf(cfg); linked {
		t.Fatal("the settings folder is left as a link to the other disk")
	}
	fi, err := os.Stat(cfg)
	if err != nil || !fi.IsDir() {
		t.Fatalf("the settings folder is gone: %v %v", fi, err)
	}
	if b, err := os.ReadFile(filepath.Join(cfg, "providers.json")); err != nil || string(b) != "providers" {
		t.Fatalf("its files: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "config")); !os.IsNotExist(err) {
		t.Errorf("the moved copy is left at %s: %v", filepath.Join(root, "config"), err)
	}
}

// Only a marker this move wrote, naming this folder and part, makes it
// owl's: a mismatched, forged or unreadable file is not ownership (#H48).
func TestMovedOwnsChecksTheMarker(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if MovedOwns(cfg) {
		t.Fatal("a folder with no marker is owl's")
	}
	if err := markMoved(cfg, root); err != nil {
		t.Fatal(err)
	}
	if !MovedOwns(cfg) {
		t.Fatal("a marked folder is not owl's")
	}
	marker := filepath.Join(cfg, MovedMark)
	for _, forged := range []string{
		fmt.Sprintf(`{"format":"owl-moved","version":1,"part":"cache","root":%q}`, root),         // other part
		fmt.Sprintf(`{"format":"owl-moved","version":1,"part":"config","root":%q}`, t.TempDir()), // other root
		`not a marker`,
	} {
		if err := os.WriteFile(marker, []byte(forged), 0o600); err != nil {
			t.Fatal(err)
		}
		if MovedOwns(cfg) {
			t.Fatalf("a marker %q made the folder owl's", forged)
		}
	}
}

// markMoved never writes over a file already there, nor follows a link:
// a marker it didn't make stays as it is (#H48).
func TestMarkMovedNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cfg, MovedMark)
	if err := os.WriteFile(marker, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := markMoved(cfg, root); err == nil {
		t.Fatal("markMoved wrote over a marker already there")
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "mine" {
		t.Fatalf("the marker was changed: %q %v", b, err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, marker); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := markMoved(cfg, root); err == nil {
		t.Fatal("markMoved wrote through a link")
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "not ours" {
		t.Fatalf("the link's target was written: %q %v", b, err)
	}
	if fi, err := os.Lstat(marker); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", fi, err)
	}
}
