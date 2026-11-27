package proc

import (
	"os/exec"
	"sync"
	"time"
)

// A Tree is a started command and the processes it starts in turn: an
// agent's CLI with the MCP servers, shells and helpers it runs. Killing the
// CLI alone leaves those running, so they are ended with it.
type Tree struct {
	cmd *exec.Cmd

	mu      sync.Mutex
	done    bool          // waited for: its pid may be another process's now
	waited  chan struct{} // closed once Wait has
	stopped chan struct{} // Stop's, closed once it has ended the tree
	group
}

// StartTree starts cmd in a group of its own: a session on Unix, a job on
// Windows. It has no terminal then, and a Ctrl+C there doesn't reach it.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	ownGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := &Tree{cmd: cmd, waited: make(chan struct{})}
	t.join(cmd)
	return t, nil
}

// Wait waits for the command, then ends what it left running.
func (t *Tree) Wait() error {
	err := t.cmd.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.done {
		t.done = true
		close(t.waited)
	}
	t.sweep(t.cmd)
	return err
}

// groupWait is how long Kill and Stop wait for the group to be gone before
// returning: a signal to a group is quick, and a group that won't go (or
// whose leader no one is waiting for yet) must not hold the caller.
var groupWait = 2 * time.Second

// Kill ends the command and every process of its tree, and waits a moment
// for the group to be gone. It does not keep the tree's lock while it
// waits: Wait must be able to reap the leader, which is what lets a Unix
// group's kill see it is gone (#M41).
func (t *Tree) Kill() {
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return
	}
	gone := t.kill(t.cmd)
	_ = t.cmd.Process.Kill()
	t.mu.Unlock()
	waitGone(gone)
}

// waitGone waits for a group's kill to see its processes gone, at most
// groupWait.
func waitGone(gone <-chan struct{}) {
	select {
	case <-gone:
	case <-time.After(groupWait):
	}
}

// Stop asks the command to end, so it can clean up first, and ends its tree
// once it has (Wait sweeps what it left) or grace has passed (Kill). Claude
// Code holding its OAuth refresh lock removes it as it exits on SIGTERM, and
// a SIGKILL left ~/.claude/.oauth_refresh.lock behind for every later
// request to wait on (#2195). Windows has no such signal: the tree is killed
// at once there. It returns at once; the channel closes once the tree is
// ended. A tree no one Waits for is killed after grace.
func (t *Tree) Stop(grace time.Duration) <-chan struct{} {
	t.mu.Lock()
	if t.stopped != nil {
		stopped := t.stopped
		t.mu.Unlock()
		return stopped
	}
	t.stopped = make(chan struct{})
	if t.done || t.term(t.cmd) != nil {
		var gone <-chan struct{}
		if !t.done {
			gone = t.kill(t.cmd)
			_ = t.cmd.Process.Kill()
		}
		t.mu.Unlock()
		// the channel closes once the tree is ended, not merely asked
		if gone != nil {
			waitGone(gone)
		}
		close(t.stopped)
		return t.stopped
	}
	t.mu.Unlock()
	go func() {
		defer close(t.stopped)
		select {
		case <-t.waited:
		case <-time.After(grace):
			t.Kill()
		}
	}()
	return t.stopped
}
