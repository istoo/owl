//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// hide has nothing to do: only Windows gives a child a window of its own.
func hide(*exec.Cmd) {}

// batch has nothing to do: only Windows runs a script through cmd.exe.
func batch(*exec.Cmd) {}

// cancelTree puts a context-cancelled command in a process group of its
// own and has its context ending kill that group, not only the command: a
// CLI that is a script running node leaves node holding the output, and
// Output waits for it however long it takes (#M41).
func cancelTree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// the command leads the group; its children are ended with it
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
}
