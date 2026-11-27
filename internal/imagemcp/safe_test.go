package imagemcp

import (
	"bytes"
	"image"
	pngimg "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reference is a plain file, under a sane size, and what its bytes are
// decides whether it is an image: a text file named .png is refused
// (#M37a).
func TestReferenceChecksWhatTheFileIs(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho not an image\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reference(dir, fake); err == nil || !strings.Contains(err.Error(), "isn't an image") {
		t.Errorf("a text file named .png was taken: %v", err)
	}
	if _, err := reference(dir, dir); err == nil || !strings.Contains(err.Error(), "plain file") {
		t.Errorf("a folder was taken as a reference: %v", err)
	}
	var buf bytes.Buffer
	if err := pngimg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real.png")
	if err := os.WriteFile(real, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := reference(dir, real)
	if err != nil {
		t.Fatalf("a real PNG was refused: %v", err)
	}
	if !strings.HasPrefix(ref, "data:image/png;base64,") {
		t.Errorf("reference = %.40q", ref)
	}
}

// A reference over 32 MiB is refused before it is read into memory
// (#M37a).
func TestReferenceRefusesAHugeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.png")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, 32<<20+1); err != nil {
		t.Fatal(err)
	}
	if _, err := reference(dir, p); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Errorf("a 32 MiB reference was read: %v", err)
	}
}

// A name a dangling link holds is taken: target steps past it, and the
// write makes a new file rather than writing through the link (#M37a).
func TestWriteDoesNotFollowADanglingLink(t *testing.T) {
	base := t.TempDir()
	folder := filepath.Join(base, "images")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(folder, "out.png")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	got, err := target(base, link, "", ".png", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(folder, "out-2.png")
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	// the write goes to the new name; the link still points at the victim,
	// whose bytes are as they were
	saved, err := writeNew(base, link, "", ".png", 0, 1, []byte("\x89PNG\r\n\x1a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if saved != want {
		t.Errorf("writeNew = %q, want %q", saved, want)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep me" {
		t.Errorf("the dangling link's target was written: %q", b)
	}
}
