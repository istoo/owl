package library

import (
	"os"
	"path/filepath"
	"testing"
)

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks aren't available: %v", err)
	}
}

// A path a plugin's manifest names is refused when it is, or goes through,
// a symlink out of the plugin's folder: the import must not read the user's
// files through it (#46).
func TestPluginInsideRefusesSymlinkOut(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, secret, filepath.Join(dir, "link"))
	if p, ok := inside(dir, "link"); ok {
		t.Fatalf("a link out of the folder was allowed: %s", p)
	}

	// a file of the plugin's own, and a link to it, are fine
	own := filepath.Join(dir, "skill.md")
	if err := os.WriteFile(own, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := inside(dir, "skill.md"); !ok {
		t.Fatal("a file of the plugin's own was refused")
	}
	symlinkOrSkip(t, own, filepath.Join(dir, "inlink"))
	if _, ok := inside(dir, "inlink"); !ok {
		t.Fatal("a link inside the folder was refused")
	}
}

// copyDir leaves out a link that leads out of the tree copied, and keeps
// one that stays inside it.
func TestCopyDirLeavesOutEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, secret, filepath.Join(src, "escape"))
	symlinkOrSkip(t, filepath.Join("..", "outside", "secret"), filepath.Join(src, "escape2"))
	symlinkOrSkip(t, filepath.Join(src, "a.txt"), filepath.Join(src, "inside"))

	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"escape", "escape2"} {
		if _, err := os.Lstat(filepath.Join(dst, n)); !os.IsNotExist(err) {
			t.Fatalf("%s was copied out of the tree", n)
		}
	}
	b, err := os.ReadFile(filepath.Join(dst, "inside"))
	if err != nil || string(b) != "a" {
		t.Fatalf("a link inside the tree was lost: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(b) != "a" {
		t.Fatalf("a file of the tree was lost: %q %v", b, err)
	}
}
