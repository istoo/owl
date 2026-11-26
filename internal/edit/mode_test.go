package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A secret (sync.json's passphrase, autobackup.json's) is never on disk
// 0644 waiting for a caller to chmod it (#4133).
func TestWriteAtomicModeIsPrivateFromTheStart(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sync.json")
	if err := WriteAtomicMode(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new: %v %v", fi.Mode(), err)
	}
	// an older file made 0644 is narrowed, not kept
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicMode(p, []byte("{} "), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("existing: %v %v", fi.Mode(), err)
	}
	// the plain write keeps what it did
	q := filepath.Join(dir, "plain.json")
	if err := WriteAtomic(q, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(q); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("plain: %v %v", fi.Mode(), err)
	}
}

// A transaction that fails after removing a secret file puts it back at the
// mode it had, not at the 0644 a new file gets.
func TestAtomicallyPutsASecretBackAtItsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(`{"key":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Atomically(func() error {
		if err := os.Remove(p); err != nil {
			return err
		}
		if err := WriteAtomicMode(p, []byte(`{"key":"sk-new"}`), 0o600); err != nil {
			return err
		}
		return errors.New("the second file failed")
	}, p)
	if err == nil {
		t.Fatal("the failure was lost")
	}
	b, rerr := os.ReadFile(p)
	if rerr != nil || string(b) != `{"key":"sk-secret"}` {
		t.Fatalf("not put back (%v): %q", rerr, b)
	}
	if fi, serr := os.Stat(p); serr != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("put back at %v, %v", fi.Mode(), serr)
	}
}
