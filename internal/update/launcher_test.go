package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/proc"
)

// claude-owl (baranwang on X) is a command of its own beside owl's:
// on Windows a .cmd that runs the owl beside it (owl.com, the
// console one, before owl.exe), elsewhere a shell script that runs
// this program by its path. Each passes every argument on as it is.
func TestLauncherText(t *testing.T) {
	win := launcherText("windows", `C:\ignored\owl.exe`, "claude", "run")
	if win != "@echo off\r\nrem written by owl: runs owl claude run\r\n\"%~dp0owl\" claude run %*\r\n" {
		t.Fatalf("Windows launcher:\n%q", win)
	}
	if LauncherFile("windows", "claude-owl") != "claude-owl.cmd" || LauncherFile("linux", "claude-owl") != "claude-owl" {
		t.Fatal("launcher file names")
	}
	sh := launcherText("darwin", "/Users/o'neil/Applications/owl.app/Contents/MacOS/owl", "claude", "run")
	want := "#!/bin/sh\n# written by owl: runs owl claude run\nexec '/Users/o'\\''neil/Applications/owl.app/Contents/MacOS/owl' 'claude' 'run' \"$@\"\n"
	if sh != want {
		t.Fatalf("shell launcher:\n%s\nwant:\n%s", sh, want)
	}
	if !strings.Contains(win, launcherMark) || !strings.Contains(sh, launcherMark) {
		t.Fatal("a launcher without owl's mark")
	}
}

// The shell launcher, run, hands the program every argument as it is:
// spaces, quotes, $ and --help included.
func TestLauncherRunsWithEveryArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	prog := filepath.Join(dir, "it's owl")
	out := filepath.Join(dir, "args")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := filepath.Join(dir, "claude-owl")
	if err := writeLauncher(l, launcherText(runtime.GOOS, prog, "claude", "run")); err != nil {
		t.Fatal(err)
	}
	if b, err := proc.Command(l, "-p", "a b", `"q"`, "$HOME", "--help").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	b, _ := os.ReadFile(out)
	if got := string(b); got != "claude\nrun\n-p\na b\n\"q\"\n$HOME\n--help\n" {
		t.Fatalf("the program got:\n%s", got)
	}
}

// A file of the user's own by the launcher's name is left as it is; one
// owl wrote is written again.
func TestLauncherLeavesTheUsersOwnFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "claude-owl")
	own := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(p, []byte(own), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeLauncher(p, launcherText("linux", "/opt/owl", "claude", "run")); err == nil {
		t.Fatal("the user's own claude-owl was written over")
	}
	if b, _ := os.ReadFile(p); string(b) != own {
		t.Fatalf("the user's file is now:\n%s", b)
	}
	if ownLauncher(p) {
		t.Fatal("the user's file reads as owl's")
	}
	if err := os.WriteFile(p, []byte(launcherText("linux", "/old/owl", "claude", "run")), 0o755); err != nil {
		t.Fatal(err)
	}
	text := launcherText("linux", "/new/owl", "claude", "run")
	if err := writeLauncher(p, text); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Fatalf("owl's own launcher wasn't brought up to date:\n%s", b)
	}
}
