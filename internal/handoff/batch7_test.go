//go:build darwin || linux

package handoff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Opens at once write their pointer through names of their own: no fixed
// .tmp shared between them is left behind, and the pointer that stays
// reads as one (#M41).
func TestConcurrentOpensKeepTheirOwnTemporaryName(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var offers []*Offer
	var firstErr error
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := Open(dir, "/apps/owl")
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if o != nil {
				offers = append(offers, o)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	err := firstErr
	list := append([]*Offer(nil), offers...)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range list {
		defer o.Close()
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, pointerName+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Fatalf("temporary pointer files are left: %v", leftovers)
	}
	b, err := os.ReadFile(filepath.Join(dir, pointerName))
	if err != nil {
		t.Fatal(err)
	}
	var p pointer
	if err := json.Unmarshal(b, &p); err != nil || p.PID != os.Getpid() || p.Sock == "" {
		t.Fatalf("the pointer reads %q: %v %+v", b, err, p)
	}
}
