package claudetui

import (
	"bufio"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The whole tail Claude Code wrote is read when it has exited: an
// assistant message, a synthetic error and the turn's end are written
// together, and reading one line lost the other two (#13).
func TestReadTailRowsEveryLineAtExit(t *testing.T) {
	body := `{"type":"assistant"}` + "\n" +
		`{"type":"system","subtype":"synthetic_error"}` + "\n" +
		`{"type":"system","subtype":"turn_duration"}` + "\n"
	var rows []string
	readTail(bufio.NewReader(strings.NewReader(body)), nil, func(b []byte) {
		rows = append(rows, string(b))
	})
	if len(rows) != 3 {
		t.Fatalf("read %d line(s), want 3: %q", len(rows), rows)
	}
	// a line left unended before the tail begins is joined to its first
	// line, as a read that stopped mid-line left it
	rows = nil
	readTail(bufio.NewReader(strings.NewReader(`"}`+"\n")), []byte(`{"type":"assistant`), func(b []byte) {
		rows = append(rows, string(b))
	})
	if len(rows) != 1 || rows[0] != `{"type":"assistant"}`+"\n" {
		t.Fatalf("a line left whole and ended by the tail: %q", rows)
	}
}

// A signal or the deadline drops the tap while its tail is reading it:
// the two are kept apart, and a second drop does nothing (#14).
func TestTapReadAndDropApart(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "tap.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Repeat("{}\n", 50)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	tp := &tap{dir: dir, f: f, rd: bufio.NewReader(f), open: map[int]*tapStream{}, ended: map[int]bool{}, started: map[string]bool{}, dropped: map[string]bool{}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tp.read()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		tp.drop()
		tp.drop() // the deferred end drops it too
	}()
	wg.Wait()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the tap's folder is still there: %v", err)
	}
}

// A message's images are written while another goroutine may drop them,
// as the tail does at a turn's end or a signal (#14).
func TestWriteImageAndDropApart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	s := &shim{out: io.Discard}
	data := base64.StdEncoding.EncodeToString([]byte("an image"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := s.writeImage("base64", "image/png", data); err != nil {
					t.Errorf("writeImage: %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			s.dropImages()
		}
	}()
	wg.Wait()
	s.dropImages()
	if left, _ := filepath.Glob(filepath.Join(tmp, imagesPrefix+"*")); len(left) != 0 {
		t.Errorf("image folders left: %v", left)
	}
}

// The end's work runs once, and removes what the helper made: a signal
// runs it before os.Exit, which runs no defer (#14).
func TestCleanupRunsOnceAndRemovesItsOwn(t *testing.T) {
	tmp := t.TempDir()
	images := filepath.Join(tmp, "images")
	wsl := filepath.Join(tmp, "wsl")
	tapDir := filepath.Join(tmp, "tap")
	for _, d := range []string{images, wsl, tapDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s := &shim{out: io.Discard, images: images, wslDir: wsl, tap: &tap{dir: tapDir}}
	s.cleanup()
	s.cleanup() // the deferred end calls it after a handler did
	for _, d := range []string{images, wsl, tapDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s is still there after cleanup: %v", d, err)
		}
	}
}
