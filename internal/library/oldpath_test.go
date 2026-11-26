package library

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A path that errors with something other than "not there" (here the
// parent is a regular file, ENOTDIR) stops the search for a free name at
// once instead of trying every number forever while holding library.mu
// (#40).
func TestOldPathStopsOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path under a file as not there")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := oldPath(filepath.Join(file, "skill"), "skill")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a path under a file found a free name")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("oldPath looped under a file")
	}
}

// setAside stops on the same kind of error, rather than choosing the next
// number forever.
func TestSetAsideStopsOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path under a file as not there")
	}
	sandbox(t)
	if err := os.MkdirAll(filepath.Dir(BackupDir()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BackupDir(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := setAside("claude", t.TempDir())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("setAside found a free name under a file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setAside looped under a file")
	}
}
