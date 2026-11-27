package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/edit"
)

// hasPrefixFold compares a Windows path as Windows does (#4133).
func TestHasPrefixFold(t *testing.T) {
	dir := `c:\users\me\.config\` + "owl" + `\`
	for _, p := range []string{
		`C:\Users\Me\.config\owl\settings.json`,
		`C:\USERS\ME\.CONFIG\OWL\settings.json`,
	} {
		if !hasPrefixFold(p, dir, true) {
			t.Errorf("%q not under %q on Windows", p, dir)
		}
		if hasPrefixFold(p, dir, false) {
			t.Errorf("%q under %q without folding", p, dir)
		}
	}
}

// A user's own write waits out a catalog sync's kept-away filter: the
// filter covers every write through edit while it runs, so writing a pick
// into it would be rolled back by it (#4133).
func TestExplicitWriteWaitsOutTheFilter(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, "probe.json")
	a := &Agent{ID: "probe", Name: "Probe", Path: path}
	a.Fields = []Field{{
		Key: "model",
		Get: func() string {
			b, _ := os.ReadFile(path)
			return string(b)
		},
		Set: func(v string) error { return edit.WriteAtomic(path, []byte(v)) },
	}}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = synced(func() error {
			close(entered)
			<-release
			return nil
		})
		close(done)
	}()
	<-entered
	applied := make(chan error, 1)
	go func() { applied <- a.Apply("model", "user") }()
	select {
	case err := <-applied:
		t.Fatalf("the pick ran inside the filter window (%v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-done
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != "user" {
		t.Fatalf("the file holds %q, want the user's value", got)
	}
}

// A stopped distro's row reads the woken agent's file: Disconnect takes the
// user's originals back with Unwire, and the row must not read what owl
// remembered before as still owl's and clear them (#4133).
func TestWSLAsleepDisconnectKeepsRestoredUserValue(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "me")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".kilo-model")
	if err := os.WriteFile(cfg, []byte("native-original"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeWSL(t, "Ubuntu\r\n", "Ubuntu\r\n", map[string]string{
		"Ubuntu": "home:/home/me\n",
	}, map[string]string{"Ubuntu": root})
	in := func(at place) *Agent {
		return &Agent{ID: "kilo", Name: "Kilo", Fields: []Field{{
			Key: "model",
			Get: func() string {
				b, _ := os.ReadFile(at.join(".kilo-model"))
				return string(b)
			},
			Set: func(v string) error { return os.WriteFile(at.join(".kilo-model"), []byte(v), 0o644) },
		}},
			Unwire: func() error {
				return os.WriteFile(at.join(".kilo-model"), []byte("native-original"), 0o644)
			}}
	}
	k := wslKind{id: "kilo", name: "Kilo", in: in}
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me"}
	wsl.Lock()
	if wsl.seen == nil {
		wsl.seen = map[string]*distro{}
	}
	if wsl.probed == nil {
		wsl.probed = map[string]bool{}
	}
	if wsl.failed == nil {
		wsl.failed = map[string]time.Time{}
	}
	wsl.seen["Ubuntu"] = &d
	wsl.Unlock()
	// as owl last saw it while stopped: on owl
	wslRemember("Ubuntu", k.memo("model"), owlID)
	a := asleep(in(d.place("kilo@wsl:Ubuntu")), k, d)
	if got := a.Field("model").Get(); got != owlID {
		t.Fatalf("before waking, the row reads %q", got)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(cfg); got != "native-original" {
		t.Fatalf("Disconnect left the user's original as %q", got)
	}
}

// With CLAUDE_CONFIG_DIR elsewhere, a ~/.claude-default already holds the
// name "default": ~/.claude is another row under a free name, never a
// second one of that name and never written over (#4133).
func TestClaudeDirDefaultNameUnique(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(env, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", env)
	for _, d := range []string{".claude-default", ".claude"} {
		writeFile(t, filepath.Join(home, d, "settings.json"), `{}`)
	}
	got := map[string]string{}
	for _, a := range claudeDirAgents(home) {
		if _, dup := got[a.ID]; dup {
			t.Errorf("%s twice", a.ID)
		}
		got[a.ID] = a.Dir
	}
	if len(got) != 2 {
		t.Fatalf("rows %v, want two", got)
	}
	if got["claude#default"] != filepath.Join(home, ".claude-default") {
		t.Errorf("claude#default is %q", got["claude#default"])
	}
	if got["claude#default-2"] != filepath.Join(home, ".claude") {
		t.Errorf("~/.claude's row %v", got)
	}
}

// A variable that names no folder is kept as it is in the copy's
// environment; one that names a folder not under home is still dropped
// (#4133).
func TestPreviewEnvKeepsNotPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CONFIG_DIR", "a-relative-name")
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/else")
	got := map[string]string{}
	for _, kv := range previewEnv(home, "/tmp/the-copy") {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	if got["PI_CONFIG_DIR"] != "a-relative-name" {
		t.Errorf("PI_CONFIG_DIR = %q", got["PI_CONFIG_DIR"])
	}
	if _, ok := got["XDG_CONFIG_HOME"]; ok {
		t.Errorf("XDG_CONFIG_HOME kept: %q", got["XDG_CONFIG_HOME"])
	}
	if got["HOME"] != "/tmp/the-copy" || got["USERPROFILE"] != "/tmp/the-copy" {
		t.Errorf("home %q, %q", got["HOME"], got["USERPROFILE"])
	}
}

// A command's variable values and arguments are shell-quoted: a space or a
// quote in the gateway's address, a key or a path is no break and no
// injection (#4133).
func TestLaunchCommandsQuoteValues(t *testing.T) {
	if got, want := agyLaunchAt("http://h/a b", "k'ey", "/s p/x", "m'x", false),
		"GEMINI_API_KEY='k'\\''ey' GOOGLE_GEMINI_BASE_URL='http://h/a b' agy --settings '/s p/x' --model 'm'\\''x'"; got != want {
		t.Errorf("agy posix:\n%s\nwant\n%s", got, want)
	}
	l := &launchSpec{bin: "bin", env: map[string]string{"K": "a b'c"}}
	if got, want := l.command("gw", "k", "m", false), "K='a b'\\''c' bin"; got != want {
		t.Errorf("posix %q, want %q", got, want)
	}
	if got, want := l.command("gw", "k", "m", true), "$env:K='a b''c'; bin"; got != want {
		t.Errorf("powershell %q, want %q", got, want)
	}
	if runtime.GOOS != "windows" {
		cmd := CursorLocalLaunch("/Applications/It's.app/Cursor", "http://h/a b")
		if !strings.HasSuffix(cmd, " '/Applications/It'\\''s.app/Cursor'") || !strings.Contains(cmd, "CURSOR_LOCAL_AGENT_BASE_URL='http://h/a b/v1'") {
			t.Errorf("Cursor Private Inference: %q", cmd)
		}
	}
}
