package sessions

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// A uuid is kept by its bytes, so it is found again in any case and no
// other id is taken for it; an id that is not a uuid is kept too.
func TestUUIDSetHoldsWhatWasAdded(t *testing.T) {
	s := &uuidSet{}
	var ids []string
	for i := range 2000 {
		ids = append(ids, fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", i*7919, i, i%4096, i%4096, i*104729))
	}
	for _, id := range ids[:1000] {
		s.add(id)
	}
	s.add("msg_01-not-a-uuid")
	for i, id := range ids {
		if got := s.has(id); got != (i < 1000) {
			t.Fatalf("has(%s) = %v", id, got)
		}
	}
	if !s.has(strings.ToUpper(ids[3])) {
		t.Fatal("a uuid isn't found in upper case")
	}
	if !s.has("msg_01-not-a-uuid") || s.has("msg_02-not-a-uuid") || s.has("") {
		t.Fatal("an id that isn't a uuid is kept wrong")
	}
	if n := s.Len(); n != 1001 {
		t.Fatalf("%d kept, want 1001", n)
	}
}

// The set keeps 16 bytes a uuid and no spare slots: a map of 16-byte keys
// was ~31 bytes each once its buckets and spare slots were counted, and
// ~12 MB of an idle owl. Ids added since the last merge are still found,
// and repeats count once.
func TestUUIDSetHolds16BytesAUUID(t *testing.T) {
	s := &uuidSet{}
	seen := map[string]bool{}
	for i := 0; i < 50000; i++ {
		id := fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", i*7919, i%65536, i%4096, (i/7)%4096, i*104729)
		if seen[id] {
			continue
		}
		seen[id] = true
		s.add(id)
		if i%997 == 0 {
			if !s.has(id) {
				t.Fatalf("an id added a moment ago isn't found: %s", id)
			}
			s.add(id) // a repeat counts once
		}
	}
	if n := s.Len(); n != len(seen) {
		t.Fatalf("%d kept, want %d", n, len(seen))
	}
	if held, most := cap(s.keys)*16, len(s.keys)*16*5/4; held > most {
		t.Fatalf("the set holds %d bytes of keys for %d uuids, more than %d", held, len(s.keys), most)
	}
	for id := range seen {
		if !s.has(id) {
			t.Fatalf("%s isn't found", id)
		}
	}
	if s.has("ffffffff-ffff-4fff-afff-ffffffffffff") {
		t.Fatal("an id that was never added is found")
	}
}

// A set of 600k uuids is measured in a process of its own: the package's
// earlier tests leave allocations a whole-heap reading can't tell from
// the set's (tasks/lessons.md, 11-14). A map of 16-byte keys was ~31
// bytes a uuid; the sorted keys are 16, so 22 is the bar.
func TestUUIDSetMemory(t *testing.T) {
	const child = "OWL_TEST_UUID_SET_HEAP"
	if os.Getenv(child) != "" {
		const n = 600000
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		id := func(i int) string {
			return fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", i*7919&0xffffffff, i%65536, (i/3)%4096, (i/7)%4096, i*104729&0xffffffffffff)
		}
		s := uuidSet{}
		for i := 0; i < n; i++ {
			s.add(id(i))
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		held := int64(after.HeapAlloc) - int64(before.HeapAlloc)
		t.Logf("%d uuids hold %d bytes, %.1f each", n, held, float64(held)/n)
		if held > n*22 {
			t.Fatalf("%d uuids hold %d bytes, over %d", n, held, n*22)
		}
		if !s.has(id(42)) {
			t.Fatal("a uuid doesn't read back in the process of its own")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestUUIDSetMemory$", "-test.v")
	cmd.Env = append(os.Environ(), child+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the measurement failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "uuids hold") {
		t.Fatalf("the measurement printed nothing: %s", out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}
