package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/istoo/owl/internal/proc"
)

// Kept pictures cost no heap. The child keeps 32 distinct pictures of 1 MiB
// of base64 each, and the live heap after a collection must have grown by
// under a quarter of that: before they went to the cache folder it grew by
// all of it. Run in a child of its own, which measures only this: the heap
// of a suite's process holds whatever earlier tests left. The bound is the
// difference between the two states (before keeping, after), not a total.
func TestKeptImagesTakeNoHeap(t *testing.T) {
	const child = "OWL_TEST_KEPT_IMAGES_HEAP"
	if os.Getenv(child) != "" {
		keptImagesHeapChild(t)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := proc.CommandContext(ctx, os.Args[0], "-test.run=^TestKeptImagesTakeNoHeap$", "-test.timeout=4m", "-test.v")
	home := t.TempDir()
	cmd.Env = append(os.Environ(), child+"=1", "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "cache"), "LocalAppData="+filepath.Join(home, "cache"))
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("kept pictures held the heap: %v", err)
	}
}

func keptImagesHeapChild(t *testing.T) {
	const n, each = 32, 1 << 20
	live := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	before := live()
	var ids []int64
	for i := range n {
		ids = append(ids, keepImage([]byte(dsBigPNG(each, strconv.Itoa(i)))))
	}
	after := live()
	grew := int64(after) - int64(before)
	fmt.Printf("kept %d MiB of base64; live heap grew by %d bytes\n", n*each>>20, grew)
	if grew > n*each/4 {
		t.Fatalf("live heap grew by %d bytes for %d of base64", grew, n*each)
	}
	for i, id := range ids {
		got, ok := keptImageOf(strconv.FormatInt(id, 10))
		if !ok || got != dsBigPNG(each, strconv.Itoa(i)) {
			t.Fatalf("picture %d did not come back whole (found %t)", i, ok)
		}
	}
}

// A picture the disk won't take stays in memory, and one whose base64 is
// not what its bytes encode to is kept as written: neither is lost.
func TestKeptImagesFallBackToMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	keptDirOnce.Do(func() {}) // the folder is chosen once: here, none
	was := keptDir
	keptDir = ""
	t.Cleanup(func() { keptDir = was })
	b64 := dsBigPNG(4<<10, "mem")
	id := keepImage([]byte(b64))
	if got, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok || got != b64 {
		t.Fatalf("no folder: picture lost (%t)", ok)
	}
	// a folder that works, and base64 with its padding cut: canonical
	// base64 is spilled, this is not, and comes back as written
	dir := t.TempDir()
	keptDir = dir
	loose := []byte(dsBigPNG(4<<10, "loose"))
	loose = loose[:len(loose)-2] // padding gone
	id = keepImage(loose)
	if got, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok || got != string(loose) {
		t.Fatalf("non-canonical base64 changed (%t)", ok)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatalf("non-canonical base64 was written to disk: %d files", len(es))
	}
	// canonical: on disk, 0600, taken out when evicted
	good := dsBigPNG(4<<10, "disk")
	id = keepImage([]byte(good))
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Fatalf("%d files for one picture", len(es))
	}
	if fi, _ := es[0].Info(); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if got, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok || got != good {
		t.Fatal("picture on disk did not come back")
	}
	os.Remove(filepath.Join(dir, es[0].Name()))
	if _, ok := keptImageOf(strconv.FormatInt(id, 10)); ok {
		t.Fatal("a picture whose file is gone was drawn")
	}
}
