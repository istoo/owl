package appdir

import (
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
