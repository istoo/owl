package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/agent"
	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/testenv"
)

// `owl install <agent>` prints the command and where it comes from and runs
// nothing; --yes runs it, by number, and only what the agent lists. A flag
// or a number it doesn't know is refused, never taken as data.
func TestInstallCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	helpHome(t)
	dir, bin := t.TempDir(), t.TempDir()
	t.Setenv("OWL_T", dir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	testenv.Program(t, filepath.Join(bin, "brew"), "#!/bin/sh\necho \"$@\" > \"$OWL_T/ran\"\necho brew says hi\n")
	proc.Forget()
	defer proc.Forget()
	defer agent.InstallsForTest([]agent.Install{{ID: "fakeagent", Name: "Fake Agent", Commands: []agent.InstallCmd{
		{Via: "brew", Command: "brew install fakeagent"},
		{Via: "npm", Command: "npm install -g fakeagent", Admin: `C:\Program Files\nodejs`},
	}}})()
	ran := func() bool { _, err := os.Stat(filepath.Join(dir, "ran")); return err == nil }

	out, err := runOut(t, "install")
	if err != nil || !strings.Contains(out, "fakeagent") || !strings.Contains(out, "brew install fakeagent") {
		t.Errorf("list: %v %q", err, out)
	}
	out, err = runOut(t, "install", "fakeagent")
	if err != nil || !strings.Contains(out, "brew install fakeagent") || !strings.Contains(out, "Homebrew") || !strings.Contains(out, "--yes") || ran() {
		t.Errorf("without --yes: %v %q ran=%v", err, out, ran())
	}
	for _, args := range [][]string{
		{"install", "fakeagent", "--bogus"}, {"install", "--bogus"}, {"install", "fakeagent", "0"}, {"install", "fakeagent", "3", "--yes"},
		{"install", "fakeagent", "x", "--yes"}, {"install", "nosuch", "--yes"}, {"install", "--yes"}, {"install", "fakeagent", "1", "2"},
	} {
		if _, err := runOut(t, args...); err == nil {
			t.Errorf("owl %s was accepted", strings.Join(args, " "))
		}
		if ran() {
			t.Fatalf("owl %s ran something", strings.Join(args, " "))
		}
	}
	// a flag is refused as a flag, not taken as an agent name or a number
	for _, args := range [][]string{{"install", "--bogus"}, {"install", "fakeagent", "--bogus", "--yes"}} {
		if _, err := runOut(t, args...); err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("owl %s: %v", strings.Join(args, " "), err)
		}
	}
	// an npm command that wants an administrator is shown, never run
	if _, err := runOut(t, "install", "fakeagent", "2", "--yes"); err == nil || ran() {
		t.Errorf("the admin command: %v ran=%v", err, ran())
	}
	// --yes runs command 1; the fake list never gains the agent, so it says it failed, with the command to copy
	out, err = runOut(t, "install", "fakeagent", "--yes")
	if !ran() || err == nil || !strings.Contains(err.Error(), "brew install fakeagent") || !strings.Contains(out, "brew says hi") {
		t.Errorf("--yes: ran=%v err=%v out=%q", ran(), err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ran")); strings.TrimSpace(string(b)) != "install fakeagent" {
		t.Errorf("brew ran with %q", b)
	}
}
