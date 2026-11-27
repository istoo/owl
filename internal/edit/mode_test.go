package edit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Windows 的 FileMode 只反映只读属性，不能用它验证 POSIX 权限位或 ACL。
func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Mode().Perm()
	if runtime.GOOS == "windows" {
		got, want = got&0200, want&0200
	}
	if got != want {
		t.Fatalf("%s：权限 %04o，期望 %04o", path, got, want)
	}
}

// A secret (sync.json's passphrase, autobackup.json's) is never on disk
// 0644 waiting for a caller to chmod it (#4133).
func TestWriteAtomicModeIsPrivateFromTheStart(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sync.json")
	if err := WriteAtomicMode(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
	// an older file made 0644 is narrowed, not kept
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicMode(p, []byte("{} "), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
	// the plain write keeps what it did
	q := filepath.Join(dir, "plain.json")
	if err := WriteAtomic(q, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, q, 0o644)
}

// A secret file already holding the same bytes is still narrowed: the
// bytes matching is no reason to leave a 0644 copy of a key.
func TestWriteAtomicModeNarrowsOnSameBytes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(`{"key":"sk-secret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicMode(p, []byte(`{"key":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
}

// A transaction that fails after only widening the mode and leaving the
// bytes as they were puts the mode back too.
func TestAtomicallyPutsBackAModeOnlyChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(`{"key":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Atomically(func() error {
		if err := os.Chmod(p, 0o644); err != nil {
			return err
		}
		return errors.New("the other file failed")
	}, p)
	if err == nil {
		t.Fatal("the failure was lost")
	}
	assertFileMode(t, p, 0o600)
}

// A file the user made stricter than the mode asked for stays stricter:
// a write takes bits away, it never puts one back.
func TestWriteAtomicModeNeverWidens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(`{"key":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o400); err != nil {
		t.Fatal(err)
	}
	// same bytes
	if err := WriteAtomicMode(p, []byte(`{"key":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o400)
	// rewritten
	if err := WriteAtomicMode(p, []byte(`{"key":"sk-new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o400)
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
	assertFileMode(t, p, 0o600)
}
