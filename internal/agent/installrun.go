package agent

// Running an install command from the Agents page and `owl install`.
//
// What runs is only what Installs() lists: the caller names an agent and the
// number of one of its commands, never a command line. A vendor's installer
// script is downloaded to a file first, its HTTP status and size checked,
// and only then run: through `curl … | sh` a dropped connection ended the
// pipe, sh ran what it had got (nothing) and the install reported success
// (CC Switch's bug). An npm command that wants an administrator is never run
// here; the user is told to run it in a terminal opened as one.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/istoo/owl/internal/lazyre"
	"github.com/istoo/owl/internal/netproxy"
	"github.com/istoo/owl/internal/proc"
)

var (
	// ErrInstallBusy is a second install asked for while one runs.
	ErrInstallBusy = errors.New("another agent is being installed: wait for it to finish, or cancel it")
	// ErrInstallNone is an agent or a command number Installs() doesn't list.
	ErrInstallNone = errors.New("no such install command")
)

// installTimeout bounds one install: a download and a package manager's
// work, nvm's Node.js build included.
var installTimeout = 15 * time.Minute

// installList is the commands that may run; a var so tests give their own.
var installList = listInstalls

// InstallsForTest makes Installs say list until restore is called.
func InstallsForTest(list []Install) (restore func()) {
	was := installList
	installList = func() []Install { return list }
	return func() { installList = was }
}

// installHTTP is the client a script is downloaded with, on the proxy
// owl's own requests take; a var so tests reach a local server.
var installHTTP = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = netproxy.Func
	netproxy.TrustEnvRoots(t)
	return &http.Client{Transport: netproxy.Dispatch(t), Timeout: 2 * time.Minute}
}

// installScriptMax is the largest script that is downloaded to run.
const installScriptMax = 4 << 20

// installTailMax is how much of an install's output is kept: its end, which
// is where a failure says why.
const installTailMax = 16 << 10

// A plan is how owl runs one install command without a pipe into a shell.
type plan struct {
	// url: a script downloaded to a file first, then run by interp
	url    string
	interp []string
	ext    string // the file's extension: powershell reads .ps1 only
	// argv: run directly (brew, npm)
	argv []string
	// shell: a line run by a shell of its own, for a command that is itself
	// a download-then-run chain (an npm command that installs Node.js first);
	// on Windows it goes to PowerShell on stdin, as provider/install.go does,
	// since Defender stops an "irm | iex" on its command line
	shell string
}

var (
	curlPipe = lazyre.New(`^curl -fsSL (https://[^\s|;&"'$` + "`" + `]+) \| (bash|sh)$`)
	irmPipe  = lazyre.New(`\birm (https://[^\s|;&"'$` + "`" + `]+)\s*\|\s*iex\b`)
	ansiSeq  = lazyre.New(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// planFor is how c runs on goos, or why it doesn't run here: an npm command
// that wants an administrator is for a terminal; one owl can't read as a
// plain install is refused rather than handed to a shell.
func planFor(c InstallCmd, goos string) (plan, error) {
	if c.Admin != "" {
		return plan{}, fmt.Errorf("npm installs into %s, which only an administrator can change: run %s in a terminal opened as administrator", c.Admin, c.Command)
	}
	switch c.Via {
	case "script":
		if m := curlPipe.FindStringSubmatch(c.Command); m != nil && goos != "windows" {
			return plan{url: m[1], interp: []string{m[2]}, ext: ".sh"}, nil
		}
	case "powershell":
		if m := irmPipe.FindStringSubmatch(c.Command); m != nil && goos == "windows" {
			return plan{url: m[1], interp: []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File"}, ext: ".ps1"}, nil
		}
	case "brew":
		if f := strings.Fields(c.Command); len(f) > 2 && f[0] == "brew" && f[1] == "install" && !strings.ContainsAny(c.Command, "|;&$`<>\"'") {
			return plan{argv: f}, nil
		}
	case "npm":
		if c.Node != "" {
			return plan{shell: c.Command}, nil
		}
		if f := strings.Fields(c.Command); len(f) > 3 && f[0] == "npm" && f[1] == "install" && f[2] == "-g" && !strings.ContainsAny(c.Command, "|;&$`<>\"'") {
			return plan{argv: f}, nil
		}
	}
	return plan{}, fmt.Errorf("owl doesn't run this command itself: run %s in a terminal", c.Command)
}

// An InstallRun is the install the Agents page started, as it stands.
type InstallRun struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Index   int    `json:"index"`
	Command string `json:"command"`
	Via     string `json:"via"`
	// State: running, done, failed or canceled
	State string `json:"state"`
	// Tail is the end of what the install printed
	Tail  string `json:"tail"`
	Error string `json:"error,omitempty"`
}

// tailBuf keeps the end of what is written to it, with ANSI colours taken out.
type tailBuf struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > installTailMax {
		t.b = append([]byte(nil), t.b[len(t.b)-installTailMax:]...)
	}
	return len(p), nil
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.ToValidUTF8(ansiSeq.ReplaceAllString(string(t.b), ""), "")
}

// installSlot holds the one install that runs; cur is the last one started.
var installSlot struct {
	sync.Mutex
	running bool
	cancel  context.CancelFunc
	cur     *InstallRun
	tail    *tailBuf
}

// resolve finds the command Installs() lists for agent id at index i.
func resolve(id string, i int) (Install, InstallCmd, plan, error) {
	for _, in := range installList() {
		if in.ID != id {
			continue
		}
		if i < 0 || i >= len(in.Commands) {
			return in, InstallCmd{}, plan{}, fmt.Errorf("%w: %s has %d, not number %d", ErrInstallNone, in.Name, len(in.Commands), i+1)
		}
		p, err := planFor(in.Commands[i], runtime.GOOS)
		return in, in.Commands[i], p, err
	}
	return Install{}, InstallCmd{}, plan{}, fmt.Errorf("%w: %q isn't an agent owl can install here", ErrInstallNone, id)
}

// StartInstall begins command i (from 0) of agent id's, in the background,
// and says where it stands. Only one runs at a time.
func StartInstall(id string, i int) (InstallRun, error) {
	in, c, p, err := resolve(id, i)
	if err != nil {
		return InstallRun{}, err
	}
	installSlot.Lock()
	if installSlot.running {
		installSlot.Unlock()
		return InstallRun{}, ErrInstallBusy
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	tail := &tailBuf{}
	run := &InstallRun{ID: in.ID, Name: in.Name, Index: i, Command: c.Command, Via: c.Via, State: "running"}
	installSlot.running, installSlot.cancel, installSlot.cur, installSlot.tail = true, cancel, run, tail
	installSlot.Unlock()
	go func() {
		defer cancel()
		err := execInstall(ctx, in.ID, p, tail)
		installSlot.Lock()
		defer installSlot.Unlock()
		run.State, run.Error = installEnd(ctx, err)
		run.Tail = tail.String()
		installSlot.running, installSlot.cancel = false, nil
	}()
	return InstallStatus(), nil
}

// InstallStatus is the install last started: running, or how it ended. The
// zero value when none was.
func InstallStatus() InstallRun {
	installSlot.Lock()
	defer installSlot.Unlock()
	if installSlot.cur == nil {
		return InstallRun{}
	}
	r := *installSlot.cur
	if r.State == "running" {
		r.Tail = installSlot.tail.String()
	}
	return r
}

// CancelInstall ends the running install, with what it started.
func CancelInstall() {
	installSlot.Lock()
	cancel := installSlot.cancel
	installSlot.Unlock()
	if cancel != nil {
		cancel()
	}
}

// installEnd is the state an install ended in, and what to say of it.
func installEnd(ctx context.Context, err error) (string, string) {
	switch {
	case err == nil:
		return "done", ""
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "failed", fmt.Sprintf("didn't finish in %s", installTimeout)
	case errors.Is(ctx.Err(), context.Canceled):
		return "canceled", ""
	}
	return "failed", err.Error()
}

// RunInstall runs command i (from 0) of agent id's in the foreground, its
// output written to w, for `owl install`. It holds the same one slot.
func RunInstall(ctx context.Context, id string, i int, w io.Writer) error {
	in, _, p, err := resolve(id, i)
	if err != nil {
		return err
	}
	installSlot.Lock()
	if installSlot.running {
		installSlot.Unlock()
		return ErrInstallBusy
	}
	installSlot.running = true
	installSlot.Unlock()
	defer func() { installSlot.Lock(); installSlot.running = false; installSlot.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	err = execInstall(ctx, in.ID, p, w)
	if state, msg := installEnd(ctx, err); state == "canceled" {
		return errors.New("install canceled")
	} else if state == "failed" {
		return errors.New(msg)
	}
	return nil
}

// execInstall runs p to its end, then looks for the agent again: an
// installer that exits well but leaves nothing to find is a failure.
func execInstall(ctx context.Context, id string, p plan, out io.Writer) error {
	err := runPlan(ctx, p, out)
	if err != nil {
		Rescan()
		return err
	}
	Rescan()
	if slices.ContainsFunc(installList(), func(x Install) bool { return x.ID == id }) {
		return errors.New("the installer finished, but owl still doesn't find the agent here: open a new terminal and check, or run the command there yourself")
	}
	return nil
}

// runPlan runs p, its output to out, and ends it and what it started when
// ctx does.
func runPlan(ctx context.Context, p plan, out io.Writer) error {
	var cmd *exec.Cmd
	switch {
	case p.url != "":
		dir, err := os.MkdirTemp("", "owl-install-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		tmp := filepath.Join(dir, "install"+p.ext)
		fmt.Fprintf(out, "downloading %s\n", p.url)
		if err := fetchScript(ctx, p.url, tmp); err != nil {
			return err
		}
		args := append(slices.Clone(p.interp[1:]), tmp)
		cmd = proc.CommandContext(ctx, lookOr(p.interp[0]), args...)
	case p.shell != "":
		if runtime.GOOS == "windows" {
			cmd = proc.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "-")
			cmd.Stdin = strings.NewReader(p.shell + "\n")
		} else {
			cmd = proc.CommandContext(ctx, lookOr("bash"), "-c", p.shell)
		}
	case len(p.argv) > 0:
		cmd = proc.CommandContext(ctx, lookOr(p.argv[0]), p.argv[1:]...)
	default:
		return errors.New("nothing to run")
	}
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Env = installEnv()
	cmd.Stdout, cmd.Stderr = out, out
	fmt.Fprintf(out, "running %s\n", strings.Join(append([]string{filepath.Base(cmd.Path)}, cmd.Args[1:]...), " "))
	t, err := proc.StartTree(cmd)
	if err != nil {
		return fmt.Errorf("couldn't run it: %w", err)
	}
	ended := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			t.Kill()
		case <-ended:
		}
	}()
	err = t.Wait()
	close(ended)
	if err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

// InstallSource says where c's install comes from, for the line shown
// before it runs: the script's address, or the package manager.
func InstallSource(c InstallCmd) string {
	if p, err := planFor(c, runtime.GOOS); err == nil && p.url != "" {
		return p.url
	}
	switch c.Via {
	case "brew":
		return "Homebrew"
	case "npm":
		if c.Node != "" {
			return "npm, with Node.js from nvm's or winget's own installer first"
		}
		return "the npm registry"
	}
	return c.Via
}

// lookOr is where name is as owl finds a tool, or name itself.
func lookOr(name string) string {
	if p, err := proc.Look(name); err == nil {
		return p
	}
	return name
}

// installEnv is the environment an install runs in: owl's proxy, and the
// folder of the npm owl found first on PATH, so a Node.js in a folder of the
// user's tools (nvm's, Homebrew's) finds its own node.
func installEnv() []string {
	env := netproxy.Env(os.Environ())
	if npm, err := proc.Look("npm"); err == nil {
		for i, kv := range env {
			if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, "PATH") {
				env[i] = k + "=" + filepath.Dir(npm) + string(os.PathListSeparator) + v
			}
		}
	}
	return append(env, "NONINTERACTIVE=1", "HOMEBREW_NO_ENV_HINTS=1", "NO_COLOR=1")
}

// fetchScript downloads url to path. Anything but a 200 and a body ends it:
// a script that didn't arrive is never run.
func fetchScript(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := installHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("downloading the installer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading the installer: %s answered HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, installScriptMax+1))
	if err != nil {
		return fmt.Errorf("downloading the installer: %w", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return fmt.Errorf("downloading the installer: %s answered with nothing", url)
	}
	if len(b) > installScriptMax {
		return fmt.Errorf("downloading the installer: %s is larger than %d MB", url, installScriptMax>>20)
	}
	return os.WriteFile(path, b, 0o700)
}
