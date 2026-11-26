package jsmod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A module beside its imports, inside the plugin folder, compiles.
func TestCompileInside(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.js"), `export const a = 1;`)
	write(t, filepath.Join(dir, "entry.js"), `import { a } from "./a.js"; export const b = a + 1;`)
	if _, err := Compile(dir, filepath.Join(dir, "entry.js"), "a middleware"); err != nil {
		t.Fatalf("a module and its import: %v", err)
	}
}

// An entry outside the plugin folder is refused: root is where its files
// may come from, and a ".." in the path given must not reach past it.
func TestCompileRefusesEntryOutside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "plug")
	other := filepath.Join(base, "other")
	write(t, filepath.Join(root, "entry.js"), `export const x = 1;`)
	write(t, filepath.Join(other, "entry.js"), `export const x = 1;`)
	if _, err := Compile(root, filepath.Join(other, "entry.js"), "a middleware"); err == nil {
		t.Fatal("compiled a file outside the plugin")
	}
}

// A relative import that climbs out with ".." is refused.
func TestCompileRefusesImportEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "plug")
	write(t, filepath.Join(base, "outside.js"), `export const x = 1;`)
	write(t, filepath.Join(root, "entry.js"), `import { x } from "../outside.js"; export const b = x;`)
	_, err := Compile(root, filepath.Join(root, "entry.js"), "a middleware")
	if err == nil || !strings.Contains(err.Error(), "outside the plugin") {
		t.Fatalf("an import out of the plugin: %v", err)
	}
}

// A symlink inside the plugin that points out of it is refused, both as
// an imported file and as the entry itself: the escape is the real path,
// not the ".." it is written without.
func TestCompileRefusesSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "plug")
	write(t, filepath.Join(base, "outside.js"), `export const x = 1;`)
	write(t, filepath.Join(root, "entry.js"), `import { x } from "./link.js"; export const b = x;`)
	if err := os.Symlink(filepath.Join(base, "outside.js"), filepath.Join(root, "link.js")); err != nil {
		t.Skipf("symlinks aren't made here: %v", err)
	}
	if _, err := Compile(root, filepath.Join(root, "entry.js"), "a middleware"); err == nil || !strings.Contains(err.Error(), "outside the plugin") {
		t.Fatalf("an import through a symlink out of the plugin: %v", err)
	}
	if err := os.Symlink(filepath.Join(base, "outside.js"), filepath.Join(root, "entry-link.js")); err != nil {
		t.Skipf("symlinks aren't made here: %v", err)
	}
	if _, err := Compile(root, filepath.Join(root, "entry-link.js"), "a middleware"); err == nil || !strings.Contains(err.Error(), "outside the plugin") {
		t.Fatalf("an entry that is a symlink out of the plugin: %v", err)
	}
}
