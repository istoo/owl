//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/istoo/owl/internal/testenv"
)

// `owl claude launcher add` puts the owl command on PATH when it
// isn't (as Settings › Command line does), then claude-owl beside it;
// remove takes out only the launcher. Everything is in a temporary home.
func TestAddLauncherBesideTheCommand(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("ZDOTDIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	exe := filepath.Join(h, "Applications", "owl.app", "Contents", "MacOS", "owl")
	bin := filepath.Join(h, ".local", "bin")
	for _, d := range []string{filepath.Dir(exe), bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Program(t, exe, "#!/bin/sh\n")
	oldExe, oldShells, oldPath, oldStuck := cliExe, cliShells, cliShellPath, cliStuck
	t.Cleanup(func() { cliExe, cliShells, cliShellPath, cliStuck = oldExe, oldShells, oldPath, oldStuck })
	cliExe = func() (string, error) { return exe, nil }
	cliShells = func() []cliShell { return []cliShell{{Name: "zsh", Path: "/bin/zsh", Default: true}} }
	cliShellPath = func(string) []string { return []string{bin, "/usr/bin"} }
	cliStuck = func() string { return "" }

	if p := Launcher("claude-owl"); p != "" {
		t.Fatalf("a launcher before any was added: %s", p)
	}
	p, err := AddLauncher("claude-owl", "claude", "run")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(bin, "claude-owl") {
		t.Fatalf("launcher at %s, want in %s", p, bin)
	}
	if l, err := os.Readlink(filepath.Join(bin, "owl")); err != nil || l != exe {
		t.Fatalf("the owl command wasn't put on PATH first: %q, %v", l, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != launcherText("darwin", exe, "claude", "run") {
		t.Fatalf("launcher:\n%s", b)
	}
	if fi, _ := os.Stat(p); fi.Mode()&0o111 == 0 {
		t.Fatalf("launcher isn't executable: %v", fi.Mode())
	}
	if Launcher("claude-owl") != p {
		t.Fatal("the launcher added isn't found")
	}
	if got, err := RemoveLauncher("claude-owl"); err != nil || got != p {
		t.Fatalf("remove: %q, %v", got, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("the launcher is still there")
	}
	if _, err := os.Lstat(filepath.Join(bin, "owl")); err != nil {
		t.Fatal("removing the launcher took the owl command out")
	}

	// Settings › Command line puts claude-owl beside owl, with
	// nothing more asked
	os.Remove(filepath.Join(bin, "owl"))
	if _, err := AddCLI(""); err != nil {
		t.Fatal(err)
	}
	if Launcher("claude-owl") != p {
		t.Fatal("adding the owl command didn't put claude-owl beside it")
	}
	// one of the user's own by that name stays theirs
	os.WriteFile(p, []byte("#!/bin/sh\necho mine\n"), 0o755)
	if _, err := AddCLI(""); err != nil {
		t.Fatalf("the user's own claude-owl failed the command's add: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "#!/bin/sh\necho mine\n" {
		t.Fatalf("the user's own claude-owl was written over:\n%s", b)
	}
}
