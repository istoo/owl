package gateway

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
	"weak"
)

// TestRecentRingKeepsNoDroppedBodies: a call that has left the Recent-calls
// ring is no longer kept alive by it, bodies and all. Re-slicing the oldest
// off left the dropped calls in the ring's backing array, up to about as
// many again as the ring holds, each with up to 2×256 KiB of bodies. The
// ring keeps a body in S2 (zipBody), so a kept call's archive is what the
// ring holds and the original string is not.
func TestRecentRingKeepsNoDroppedBodies(t *testing.T) {
	s := &Server{}
	const calls = 3 * recentMost
	body := strings.Repeat("x", 4<<10)
	originals := make([]weak.Pointer[byte], calls)
	archives := make([]weak.Pointer[byte], calls)
	for i := range calls {
		b := strings.Repeat("x", 4<<10) // its own allocation, for the weak pointer
		originals[i] = weak.Make(unsafe.StringData(b))
		s.record(Call{Model: "m" + strconv.Itoa(i), RequestBody: b})
		s.mu.Lock()
		if c := s.recent[len(s.recent)-1]; len(c.reqZip) > 0 {
			archives[i] = weak.Make(unsafe.SliceData(c.reqZip))
		}
		s.mu.Unlock()
	}
	runtime.GC()
	runtime.GC()
	for i := range calls {
		if originals[i].Value() != nil {
			t.Fatalf("call %d's body string is still kept", i)
		}
	}
	var kept []int
	for i := range calls - recentMost {
		if archives[i].Value() != nil {
			kept = append(kept, i)
		}
	}
	if len(kept) > 0 {
		t.Fatalf("%d calls dropped from the ring are still alive: %v", len(kept), kept)
	}
	for i := calls - recentMost; i < calls; i++ {
		if archives[i].Value() == nil {
			t.Fatalf("call %d, still in the ring, lost its body", i)
		}
		c, ok := s.RecentCall(uint64(i + 1))
		if !ok || c.RequestBody != body {
			t.Fatalf("call %d, still in the ring, doesn't give its body back", i)
		}
	}
	got := s.Recent()
	if len(got) != recentMost {
		t.Fatalf("ring holds %d calls, want %d", len(got), recentMost)
	}
	for i, c := range got {
		if want := "m" + strconv.Itoa(calls-1-i); c.Model != want {
			t.Fatalf("Recent()[%d] = %s, want %s (newest first)", i, c.Model, want)
		}
		if c.ID != uint64(calls-i) {
			t.Fatalf("Recent()[%d].ID = %d, want %d", i, c.ID, calls-i)
		}
	}
	runtime.KeepAlive(s)
}
