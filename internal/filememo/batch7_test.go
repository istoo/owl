package filememo

import (
	"testing"
	"time"
)

// settle drops the bytes of the entry it was given only while that entry
// is still the one kept: a read that parsed a newer version meanwhile is
// not rolled back to the older one (#M41).
func TestSettleKeepsANewerEntry(t *testing.T) {
	key := memoKey{"settle-test", "/tmp/settle-test"}
	t.Cleanup(func() {
		mu.Lock()
		delete(seen, key)
		mu.Unlock()
	})
	old := entry{mod: time.Now().Add(-time.Hour), size: 1, v: "old", raw: []byte("old")}
	newer := entry{mod: time.Now().Add(-10 * time.Second), size: 2, v: "new", raw: []byte("new")}
	mu.Lock()
	seen[key] = newer
	mu.Unlock()
	settle(key, old)
	mu.Lock()
	got := seen[key]
	mu.Unlock()
	if got.v != "new" || got.raw == nil {
		t.Fatalf("settle put the older entry back: %+v", got)
	}

	// the same entry, once old enough, is still settled
	mu.Lock()
	seen[key] = newer
	mu.Unlock()
	settle(key, newer)
	mu.Lock()
	got = seen[key]
	mu.Unlock()
	if got.raw != nil || got.again != nil {
		t.Fatalf("the entry's bytes were not dropped: %+v", got)
	}
}
