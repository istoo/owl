package imagemcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A path whose parent is a regular file makes every Stat of the name fail
// with ENOTDIR, not ErrNotExist. target used to read that as "the name is
// taken" and count k up forever, one failed Stat at a time, pinning a CPU
// (#43). It must come back with an error at once.
func TestTargetStopsWhenTheNameCannotBeTold(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-folder")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "out.png")

	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := target(base, path, "a cat", ".png", 0, 1)
		done <- struct {
			out string
			err error
		}{out, err}
	}()
	select {
	case got := <-done:
		if got.err == nil {
			t.Fatalf("target = %q, nil: a name it can't check answered as free", got.out)
		}
		if !strings.Contains(got.err.Error(), "taken") {
			t.Fatalf("target = %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("target never returned: a Stat failing with ENOTDIR was read as a taken name")
	}
}

// A name that is there is still given a number, and a free one comes back
// as it is.
func TestTargetNumbersATakenName(t *testing.T) {
	base := t.TempDir()
	folder := filepath.Join(base, "images")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "out.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := target(base, filepath.Join(folder, "out.png"), "", ".png", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(folder, "out-2.png"); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	free, err := target(base, filepath.Join(folder, "free.png"), "", ".png", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(folder, "free.png"); free != want {
		t.Fatalf("target = %q, want %q", free, want)
	}
}
