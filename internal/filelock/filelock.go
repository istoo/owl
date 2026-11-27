// Package filelock keeps a change to a file one at a time across the
// owls on a computer: the gateway that runs all day, and each owl
// run from a terminal or a script meanwhile.
package filelock

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Wait is how long Mutex waits for another owl to let go, before it
// goes on without the lock, as it did before there was one: a owl
// stuck holding it (or two owls each waiting on a lock the other
// holds) never hangs every other.
const Wait = 10 * time.Second

// mkdirAll makes a lock file's folder; a test counts the calls.
var mkdirAll = os.MkdirAll

// Lock takes the lock on the file at path, made if it isn't there, waiting
// up to wait for another process to let it go. The system holds it on the
// file and lets it go when the process holding it ends, however it ends,
// so none is ever left behind; the file stays, since taken away one
// process could hold the lock on it while another took one on a new file.
// held is false when it went on without the lock: on a file system
// without locks, or after wait.
func Lock(path string, wait time.Duration) (unlock func(), held bool) {
	// the folder is made only when the file can't be opened for want of
	// it: made first, each lock asked the system for the folder too, on
	// every take (#1665: ~190 times in a Windows start's first 25s, each
	// an antivirus filter driver's work)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		mkdirAll(filepath.Dir(path), 0o700)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	}
	if err != nil {
		return func() {}, false // no lock to be had here: no file to change either
	}
	start := time.Now()
	for {
		got, err := TryLock(f)
		if err != nil { // a file system without locks, as an NFS home with no lockd
			f.Close()
			return func() {}, false
		}
		if got {
			return func() { Unlock(f); f.Close() }, true
		}
		if time.Since(start) > wait {
			f.Close()
			log.Printf("%s: another owl held it for %v; going on without it", path, wait)
			return func() {}, false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Mutex is a sync.Mutex that also holds the lock on the file Path names
// while it is held, so a read, change and write of a file under it is
// one at a time across owls too, not only within this one. Every
// read of the file a change is made from is to be made after Lock.
type Mutex struct {
	mu sync.Mutex
	// Path is the lock file: the file guarded, with ".lock".
	Path func() string
	// Locked, when set, is called once both locks are held: to have the
	// file guarded read afresh, not from what was parsed of it before
	// another owl wrote it.
	Locked func()
	unlock func()
	held   bool
}

// ErrNotHeld is a strict take that couldn't hold the file's lock: the
// folder can't be locked, or another owl held it for the whole wait.
var ErrNotHeld = errors.New("the file's lock was not taken")

// Lock takes both locks, waiting up to Wait for another owl to let the
// file's go. It keeps sync.Locker's shape: the change goes on without the
// file's lock when none can be had or Wait passes, and Held says which.
// A change that must be exclusive across owls uses LockStrict.
func (m *Mutex) Lock() { m.LockTimeout(Wait) }

// LockTimeout is Lock, waiting up to wait for another owl's lock. It is
// the same contract as Lock: a wait that passes lets the change go on
// without the lock rather than hang, and held says so.
func (m *Mutex) LockTimeout(wait time.Duration) (held bool) {
	m.mu.Lock()
	m.unlock, held = Lock(m.Path(), wait)
	m.held = held
	if m.Locked != nil {
		m.Locked()
	}
	return held
}

// LockStrict takes both locks and fails (ErrNotHeld) when the file's
// can't be had: for a write that must not run beside another owl's.
func (m *Mutex) LockStrict() error { return m.LockStrictTimeout(Wait) }

// LockStrictTimeout is LockStrict with its own wait.
func (m *Mutex) LockStrictTimeout(wait time.Duration) error {
	m.mu.Lock()
	unlock, held := Lock(m.Path(), wait)
	if !held {
		m.mu.Unlock()
		return fmt.Errorf("%s: %w", m.Path(), ErrNotHeld)
	}
	m.unlock, m.held = unlock, true
	if m.Locked != nil {
		m.Locked()
	}
	return nil
}

// Held says whether the last Lock took the file's lock (false when it went
// on without it).
func (m *Mutex) Held() bool { return m.held }

func (m *Mutex) Unlock() {
	unlock := m.unlock
	m.unlock, m.held = nil, false
	if unlock != nil {
		unlock()
	}
	m.mu.Unlock()
}
