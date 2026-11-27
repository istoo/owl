package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
