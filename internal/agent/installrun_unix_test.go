//go:build !windows

package agent

import (
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Cancelling ends the installer and what it started.
func TestCancelEndsTheInstallerTree(t *testing.T) {
	dir := fakeInstallEnv(t, "#!/bin/sh\nsleep 300 &\necho $! > \"$OWL_T/pid\"\nwait\n")
	if _, err := StartInstall("fakeagent", 0); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(waitFile(t, filepath.Join(dir, "pid")))
	if err != nil {
		t.Fatal(err)
	}
	CancelInstall()
	waitInstall(t, "canceled")
	for end := time.Now().Add(20 * time.Second); syscall.Kill(pid, 0) == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the installer's child %d is still running", pid)
		}
	}
}
