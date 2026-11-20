package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/testenv"
)

// fakeInstallEnv lists one agent, "fakeagent", as installable by a brew
// that is the given script, and gives the test a folder (OWL_T) the script
// writes in. The agent counts as installed once OWL_T/landed exists. What
// the test starts is ended with it.
func fakeInstallEnv(t *testing.T, brew string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir, bin := t.TempDir(), t.TempDir()
	t.Setenv("OWL_T", dir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	testenv.Program(t, filepath.Join(bin, "brew"), brew)
	proc.Forget()
	was := installList
	installList = func() []Install {
		if _, err := os.Stat(filepath.Join(dir, "landed")); err == nil {
			return []Install{}
		}
		return []Install{{ID: "fakeagent", Name: "Fake", Commands: []InstallCmd{{Via: "brew", Command: "brew install fakeagent"}}}}
	}
	t.Cleanup(func() {
		CancelInstall()
		waitInstall(t, "")
		installList = was
		proc.Forget()
	})
	return dir
}

// waitInstall waits until no install runs, or the one running ends in state
// (when given), and gives its status.
func waitInstall(t *testing.T, state string) InstallRun {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r := InstallStatus()
		if r.State != "running" && (state == "" || r.State == state) {
			return r
		}
	}
	t.Fatalf("install didn't end in %q: %+v", state, InstallStatus())
	return InstallRun{}
}

func waitFile(t *testing.T, p string) string {
	t.Helper()
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
	}
	t.Fatalf("%s never written", p)
	return ""
}

// Every command Installs() can list is one owl runs without a shell pipe,
// and an npm one that wants an administrator is refused with the command to
// run in a terminal; no other kind exists.
func TestInstallPlansEveryListedCommand(t *testing.T) {
	n := 0
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, node := range []bool{true, false} {
			for _, a := range All() {
				for _, c := range installCommands(a.ID, goos, node) {
					n++
					p, err := planFor(c, goos)
					if err != nil {
						t.Errorf("%s on %s: %v", c.Command, goos, err)
						continue
					}
					if p.url != "" && !strings.HasPrefix(p.url, "https://") {
						t.Errorf("%s: downloads %s", c.Command, p.url)
					}
					if p.url != "" && !strings.Contains(c.Command, p.url) {
						t.Errorf("%s: downloads %s", c.Command, p.url)
					}
					if c.Via == "npm" {
						c.Admin = `C:\Program Files\nodejs`
						if _, err := planFor(c, goos); err == nil || !strings.Contains(err.Error(), c.Command) {
							t.Errorf("%s with an admin prefix: %v", c.Command, err)
						}
					}
				}
			}
		}
	}
	if n < 30 {
		t.Fatalf("only %d commands planned", n)
	}
}

// A command that is more than a plain install is not run: nothing a shell
// would read as a second command, a pipe, or an http address.
func TestInstallPlanRefusesWhatItDoesNotRead(t *testing.T) {
	for _, c := range []InstallCmd{
		{Via: "brew", Command: "brew install x; touch /tmp/evil"},
		{Via: "brew", Command: "brew install x && rm -rf ~"},
		{Via: "brew", Command: "rm -rf /"},
		{Via: "npm", Command: "npm install -g x | sh"},
		{Via: "npm", Command: "npm install -g $(touch evil)"},
		{Via: "script", Command: "curl -fsSL http://example.com/i.sh | bash"},
		{Via: "script", Command: "curl -fsSL https://example.com/i.sh | bash; id"},
		{Via: "script", Command: "curl -fsSL https://example.com/i.sh | sudo bash"},
		{Via: "other", Command: "npm install -g x"},
	} {
		if _, err := planFor(c, "linux"); err == nil {
			t.Errorf("%q was planned", c.Command)
		}
	}
}

// The caller names an agent and a number: a name Installs() doesn't list,
// or a number outside its commands, runs nothing.
func TestInstallRunsOnlyWhatIsListed(t *testing.T) {
	dir := fakeInstallEnv(t, "#!/bin/sh\necho \"$@\" > \"$OWL_T/args\"\ntouch \"$OWL_T/landed\"\n")
	for _, c := range []struct {
		id string
		i  int
	}{{"fakeagent", 1}, {"fakeagent", -1}, {"fakeagent", 99}, {"claude", 0}, {"", 0}, {"fakeagent; touch x", 0}} {
		if _, err := StartInstall(c.id, c.i); !errors.Is(err, ErrInstallNone) {
			t.Errorf("%q #%d: %v", c.id, c.i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "args")); err == nil {
		t.Fatal("brew ran for a command that isn't listed")
	}
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	r := waitInstall(t, "done")
	if got := waitFile(t, filepath.Join(dir, "args")); got != "install fakeagent" {
		t.Errorf("brew ran with %q", got)
	}
	if r.Command != "brew install fakeagent" || r.ID != "fakeagent" || !strings.Contains(r.Tail, "running") {
		t.Errorf("status %+v", r)
	}
}

// An install that exits well but leaves the agent unfound is a failure,
// whose tail says what it printed.
func TestInstallThatLandsNothingFails(t *testing.T) {
	fakeInstallEnv(t, "#!/bin/sh\necho 'all done, honest'\n")
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	r := waitInstall(t, "failed")
	if !strings.Contains(r.Error, "still doesn't find") || !strings.Contains(r.Tail, "all done, honest") {
		t.Errorf("%+v", r)
	}
}

// A failing installer is a failure with the end of its output.
func TestInstallFailureKeepsTheTail(t *testing.T) {
	fakeInstallEnv(t, "#!/bin/sh\nfor i in 1 2 3 4 5 6; do echo line$i; done\necho 'no space left' >&2\nexit 3\n")
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	r := waitInstall(t, "failed")
	if !strings.Contains(r.Error, "exit status 3") || !strings.Contains(r.Tail, "no space left") || !strings.Contains(r.Tail, "line6") {
		t.Errorf("%+v", r)
	}
}

// Only one install runs at a time.
func TestOnlyOneInstallRuns(t *testing.T) {
	dir := fakeInstallEnv(t, "#!/bin/sh\necho up > \"$OWL_T/started\"\nwhile [ ! -e \"$OWL_T/release\" ]; do sleep 0.05; done\ntouch \"$OWL_T/landed\"\n")
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(dir, "started"))
	if _, err := StartInstall("fakeagent", 0); !errors.Is(err, ErrInstallBusy) {
		t.Errorf("a second install: %v", err)
	}
	if err := RunInstall(context.Background(), "fakeagent", 0, &tailBuf{}); !errors.Is(err, ErrInstallBusy) {
		t.Errorf("a foreground install beside it: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "release"), []byte("x"), 0o644)
	waitInstall(t, "done")
	// the slot is free again: the agent is landed, so there is nothing to run, but it isn't busy
	if _, err := StartInstall("fakeagent", 0); errors.Is(err, ErrInstallBusy) {
		t.Errorf("after it ended: %v", err)
	}
}

// The timeout ends an install that doesn't, as a failure that says so.
func TestInstallTimeout(t *testing.T) {
	fakeInstallEnv(t, "#!/bin/sh\nsleep 300 &\nwait\n")
	was := installTimeout
	installTimeout = 300 * time.Millisecond
	defer func() { installTimeout = was }()
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	if r := waitInstall(t, "failed"); !strings.Contains(r.Error, "didn't finish") {
		t.Errorf("%+v", r)
	}
}

// An npm command that wants an administrator is not run, and the answer
// has the command for a terminal.
func TestAdminNPMIsNotRun(t *testing.T) {
	dir := fakeInstallEnv(t, "#!/bin/sh\ntouch \"$OWL_T/ran\"\n")
	installList = func() []Install {
		return []Install{{ID: "fakeagent", Name: "Fake", Commands: []InstallCmd{{Via: "npm", Command: "npm install -g fakeagent", Admin: `C:\Program Files\nodejs`}}}}
	}
	_, err := StartInstall("fakeagent", 0)
	if err == nil || !strings.Contains(err.Error(), "npm install -g fakeagent") || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("%v", err)
	}
	if err := RunInstall(context.Background(), "fakeagent", 0, &tailBuf{}); err == nil {
		t.Error("the foreground install ran it")
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Error("something ran")
	}
}

// A script is downloaded to a file first, and a download that isn't a 200
// with a body is never run.
func TestScriptIsDownloadedBeforeItRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	for _, c := range []struct {
		name   string
		status int
		body   string
		err    string
		ran    bool
	}{
		{"good", 200, "#!/bin/sh\ntouch \"$OWL_T/ran\"\n", "", true},
		{"not found", 404, "#!/bin/sh\ntouch \"$OWL_T/ran\"\n", "HTTP 404", false},
		{"server error", 503, "#!/bin/sh\ntouch \"$OWL_T/ran\"\n", "HTTP 503", false},
		{"empty", 200, "", "nothing", false},
		{"blank", 200, " \n\n", "nothing", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OWL_T", dir)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			}))
			defer srv.Close()
			tail := &tailBuf{}
			err := runPlan(context.Background(), plan{url: srv.URL + "/i.sh", interp: []string{"sh"}, ext: ".sh"}, tail)
			if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
				t.Errorf("err %v, want %q", err, c.err)
			}
			_, ranErr := os.Stat(filepath.Join(dir, "ran"))
			if (ranErr == nil) != c.ran {
				t.Errorf("ran %v, want %v (%s)", ranErr == nil, c.ran, tail)
			}
		})
	}
}

// A download that can't be reached at all is a failure, and nothing runs.
func TestScriptDownloadUnreachable(t *testing.T) {
	t.Setenv("OWL_T", t.TempDir())
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/i.sh"
	srv.Close()
	if err := runPlan(context.Background(), plan{url: url, interp: []string{"sh"}, ext: ".sh"}, &tailBuf{}); err == nil {
		t.Error("an unreachable download went on")
	}
}

// The tail keeps the end, with colours taken out.
func TestTailKeepsTheEnd(t *testing.T) {
	var b tailBuf
	b.Write([]byte(strings.Repeat("x", installTailMax)))
	b.Write([]byte("\x1b[31mred end\x1b[0m"))
	s := b.String()
	if !strings.HasSuffix(s, "red end") || strings.Contains(s, "\x1b") || len(s) > installTailMax {
		t.Errorf("%d bytes, ends %q", len(s), s[len(s)-20:])
	}
}
