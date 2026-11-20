package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/istoo/owl/internal/agent"
	"github.com/istoo/owl/internal/claudecode"
	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/plugin"
	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/update"
)

// `owl claude run [args…]` is claude-owl (baranwang on X): Claude
// Code through owl, while a plain `claude` stays on the user's own
// account. claude starts with --settings and owl's launch file
// (agent/claude_launch.go), so ~/.claude/settings.json isn't switched;
// every argument is claude's, --help too. `owl claude launcher` puts
// the claude-owl command beside the owl one on the PATH.

const claudeRunUsage = `owl claude run [args…]      start Claude Code through owl
owl claude launcher [add|remove]
                               the claude-owl command, which runs this

claude starts with --settings and a settings file owl keeps for it, wired
to owl's gateway, key and models; your own ~/.claude/settings.json and a
plain claude stay as they are. Every argument after run is claude's.
`

// claudeLauncherName is the command a terminal runs for claude-owl.
const claudeLauncherName = "claude-owl"

// claudeLaunchCmd is `owl claude run|launcher …`; ok false for
// another form.
func claudeLaunchCmd(args []string) (ok bool, err error) {
	if len(args) < 2 || args[0] != "claude" && args[0] != "claude-code" && args[0] != "cc" {
		return false, nil
	}
	switch args[1] {
	case "run":
		return true, claudeRun(args[2:])
	case "launcher":
		rest := args[2:]
		if len(rest) > 0 && slices.Contains([]string{"-h", "--help", "help"}, rest[0]) {
			fmt.Print(claudeRunUsage)
			return true, nil
		}
		return true, claudeLauncher(rest)
	}
	return false, nil
}

// claudeRunExec runs claude (claude_run_*.go) and waits for it; tests set it.
var claudeRunExec = claudeRunOS

// claudeFind is the Claude Code to start; tests set it.
var claudeFind = claudecode.Find

// claudeGatewayUp says whether owl's gateway answers; tests set it.
var claudeGatewayUp = gateway.Running

// claudeLetGo hands claude the terminal (its Ctrl+C, its exit) and ends
// what owl started (probes, plugin hosts), so nothing of owl's runs
// beside a session for its hours; tests set it, since a host ended stays
// ended in the process.
var claudeLetGo = func() {
	leaveSignals()
	proc.EndProbes()
	plugin.EndHosts()
}

func claudeRun(args []string) error {
	bin := claudeFind()
	if bin == "" {
		return errors.New("Claude Code isn't installed: install it from https://claude.com/claude-code, " + claudecode.InstallHint)
	}
	if self, err := os.Executable(); err == nil && sameProgram(self, bin) {
		return errors.New("the claude on your PATH is owl itself; install Claude Code")
	}
	home, _ := os.UserHomeDir()
	plugin.ServePages(false)
	startUp()
	path, err := agent.ClaudeLaunch(home)
	if err != nil {
		return fmt.Errorf("writing claude-owl's settings: %w", err)
	}
	if !claudeGatewayUp() {
		fmt.Fprintln(os.Stderr, "owl: the gateway isn't running, so Claude Code's requests will fail: open owl, or run `owl serve`")
	}
	mark := agent.ClaudeLaunchBefore(home)
	claudeLetGo()
	runErr := claudeRunExec(bin, append([]string{"--settings", path}, args...), os.Environ())
	if err := agent.ClaudeLaunchAfter(home, mark); err != nil {
		fmt.Fprintln(os.Stderr, "owl: putting your settings.json's model back:", err)
	}
	return runErr
}

func claudeLauncher(args []string) error {
	what := "status"
	if len(args) > 0 {
		what = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("owl claude launcher takes one word, not %q", args[1])
	}
	switch what {
	case "status":
		if p := update.Launcher(claudeLauncherName); p != "" {
			fmt.Println(p)
			return nil
		}
		fmt.Println("claude-owl isn't on your PATH: run `owl claude launcher add`")
		return nil
	case "add":
		p, err := update.AddLauncher(claudeLauncherName, "claude", "run")
		if err != nil {
			return err
		}
		fmt.Println("claude-owl:", p)
		return nil
	case "remove":
		p, err := update.RemoveLauncher(claudeLauncherName)
		if err != nil {
			return err
		}
		if p != "" {
			fmt.Println("removed", p)
		}
		return nil
	}
	return fmt.Errorf("owl claude launcher: add, remove or nothing, not %q", what)
}
