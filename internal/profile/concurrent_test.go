package profile

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/istoo/owl/internal/filelock"
)

// Two saves at once each read profiles.json, add their own and write the
// whole map back: without one at a time the later write loses the other's
// profile. Every save is kept, and the file is the user's alone.
func TestConcurrentSavesKeepEveryProfile(t *testing.T) {
	sandbox(t)
	const n = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := Save(fmt.Sprintf("p%d", i), Profile{Fields: map[string]string{"claude.model": fmt.Sprintf("m%d", i)}}); err != nil {
				t.Errorf("save p%d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	ps, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != n {
		t.Fatalf("%d profiles after %d saves, want %d", len(ps), n, n)
	}
	for i := 0; i < n; i++ {
		if got := ps[fmt.Sprintf("p%d", i)].Fields["claude.model"]; got != fmt.Sprintf("m%d", i) {
			t.Errorf("p%d has %q", i, got)
		}
	}
	if fi, err := os.Stat(Path()); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("profiles.json mode %v", fi.Mode())
	}
}

// A profile change that can't get the cross-process lock writes nothing:
// waiting out another owl's read-change-write and then writing anyway
// would put back what it changed. Nothing is created or overwritten.
func TestSaveRefusesWithoutTheCrossProcessLock(t *testing.T) {
	sandbox(t)
	unlock, held := filelock.Lock(Path()+".lock", time.Second)
	if !held {
		t.Skip("the file system keeps no locks here")
	}
	defer unlock()
	old := profilesLockWait
	profilesLockWait = 0
	t.Cleanup(func() { profilesLockWait = old })
	err := Save("mine", Profile{Fields: map[string]string{"claude.model": "m"}})
	if err == nil {
		t.Fatal("the save went through without the lock")
	}
	if !errors.Is(err, filelock.ErrNotHeld) {
		t.Fatalf("the refusal: %v", err)
	}
	if _, serr := os.Stat(Path()); !os.IsNotExist(serr) {
		t.Fatalf("profiles.json written without the lock: %v", serr)
	}
	// and the locked file the other owl wrote is left as it was
	if err := os.WriteFile(Path(), []byte(`{"theirs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save("mine", Profile{}); err == nil {
		t.Fatal("a second save went through without the lock")
	}
	if b, _ := os.ReadFile(Path()); string(b) != `{"theirs":{}}` {
		t.Fatalf("the other owl's file was changed: %s", b)
	}
}
