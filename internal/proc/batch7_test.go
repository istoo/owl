//go:build !windows

package proc

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A context ending kills what the command started, not only the command: a
// CLI that is a script leaves its child holding the output (#M41).
func TestCommandContextEndsTheTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := CommandContext(ctx, "/bin/sh", "-c", "sleep 600 >/dev/null 2>&1 & echo $!; wait")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		cancel()
		cmd.Wait()
		t.Fatalf("no child pid: %q", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the command outlived its context")
	}
	if !gone(child) {
		t.Fatalf("its child %d outlived the context", child)
	}
}
