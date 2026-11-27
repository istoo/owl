package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/appdir"
	"github.com/istoo/owl/internal/library"
)

// Data follows owl's folder link only after checking what it leads to: a
// link made by hand to $HOME must not turn uninstalling into deleting the
// user's home (#H48).
func TestDataRefusesALinkToHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	sentinel := filepath.Join(home, "keep-me.txt")
	if err := os.WriteFile(sentinel, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".config", "owl")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	r := &Report{}
	Data(r)
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("Data deleted the home directory: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("the refused link was removed: %v", err)
	}
	refused := false
	for _, d := range r.Data {
		if strings.Contains(d.Err, "isn't one of owl's folders") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("no refusal was reported: %+v", r.Data)
	}
}

// A hand-made link with the Library in use is kept whole: an agent reads
// its skills and servers through the link's own path, and unlinking it or
// deleting the folder takes them away (#H48).
func TestDataKeepsALinkedLibraryInUse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	target := t.TempDir()
	write(t, filepath.Join(target, "settings.json"), "{}")
	link := appdir.Config()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(link) // an empty folder Config may have made
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	skill := filepath.Join(library.Dir(), "skills", "docx", "SKILL.md")
	write(t, skill, "# docx")

	r := &Report{}
	Data(r)
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("the link an agent reads the Library through was removed: %v", err)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("a skill in use was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "settings.json")); err != nil {
		t.Fatalf("the linked folder was partly deleted: %v", err)
	}
	keptFolder, keptLib := false, false
	for _, d := range r.Kept {
		if d.What == target && strings.Contains(d.Err, "still used through it") {
			keptFolder = true
		}
		if d.What == library.Dir() {
			keptLib = true
		}
	}
	if !keptFolder || !keptLib {
		t.Fatalf("Kept doesn't name the link's folder and its Library: %+v", r.Kept)
	}
}
