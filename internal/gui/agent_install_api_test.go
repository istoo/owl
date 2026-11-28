package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/agent"
	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/testenv"
)

// The Agents page's Install: the request names an agent and a command's
// number, never a command; an unknown agent or number is a 400, and a second
// install beside a running one a 409. A page of another origin is refused
// before any /api route by the guard in front of the whole mux
// (web_oneport_test.go's onePortCases, corsGuard).
func TestAgentInstallAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir, bin := t.TempDir(), t.TempDir()
	t.Setenv("OWL_T", dir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	testenv.Program(t, filepath.Join(bin, "brew"), "#!/bin/sh\necho \"$@\" > \"$OWL_T/ran\"\nwhile [ ! -e \"$OWL_T/release\" ]; do sleep 0.05; done\n")
	proc.Forget()
	restore := agent.InstallsForTest([]agent.Install{{ID: "fakeagent", Name: "Fake", Commands: []agent.InstallCmd{{Via: "brew", Command: "brew install fakeagent"}}}})
	defer func() {
		agent.CancelInstall()
		for end := time.Now().Add(20 * time.Second); agent.InstallStatus().State == "running" && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		}
		restore()
		proc.Forget()
	}()
	h := Handler(nil, nil)
	call := func(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		h.ServeHTTP(rec, r)
		return rec
	}
	ran := func() bool { _, err := os.Stat(filepath.Join(dir, "ran")); return err == nil }
	// the shell makes the file when it redirects the command's output,
	// before echo has written into it: a busy machine showed it empty. What
	// is waited for is the line itself, not the file
	ranWith := func() string { b, _ := os.ReadFile(filepath.Join(dir, "ran")); return strings.TrimSpace(string(b)) }
	for _, body := range []string{`{"id":"nosuch","index":0}`, `{"id":"fakeagent","index":1}`, `{"id":"fakeagent","index":-1}`, `{`, ``, `{"id":"fakeagent;touch x","index":0}`} {
		if rec := call("POST", "/api/agents/install", body); rec.Code != 400 {
			t.Errorf("%q: %d %s", body, rec.Code, rec.Body)
		}
	}
	if ran() {
		t.Fatal("a refused request ran something")
	}
	// a command in the request is no field of it: the listed one runs
	rec := call("POST", "/api/agents/install", `{"id":"fakeagent","index":0,"command":"touch evil","Command":"touch evil"}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var run agent.InstallRun
	json.Unmarshal(rec.Body.Bytes(), &run)
	if run.State != "running" || run.Command != "brew install fakeagent" {
		t.Errorf("%+v", run)
	}
	for end := time.Now().Add(20 * time.Second); ranWith() != "install fakeagent" && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
	}
	if got := ranWith(); got != "install fakeagent" {
		t.Errorf("brew ran with %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Error("the request's own command ran")
	}
	if rec := call("POST", "/api/agents/install", `{"id":"fakeagent","index":0}`); rec.Code != 409 {
		t.Errorf("a second install: %d %s", rec.Code, rec.Body)
	}
	if rec := call("GET", "/api/agents/install/run", ""); !strings.Contains(rec.Body.String(), `"running"`) {
		t.Errorf("status: %s", rec.Body)
	}
	rec = call("POST", "/api/agents/install/cancel", "{}")
	if rec.Code != 200 {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if agent.InstallStatus().State == "canceled" {
			return
		}
	}
	t.Errorf("not canceled: %+v", agent.InstallStatus())
}
