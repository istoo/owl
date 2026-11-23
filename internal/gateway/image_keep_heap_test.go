package gateway

import (
	"container/list"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
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

// withKeptDir makes dir the folder of kept pictures for a test ("" for
// none), and puts the state back after, the pictures kept in the test
// forgotten: their files go with its folder, and another test keeping the
// same picture must not meet an entry that points there.
func withKeptDir(t *testing.T, dir string) {
	t.Helper()
	st := &keptDirState
	st.Lock()
	wasDone, wasDir := st.done, st.dir
	st.done, st.dir = true, dir
	st.Unlock()
	t.Cleanup(func() {
		st.Lock()
		st.done, st.dir = wasDone, wasDir
		st.Unlock()
		k := &keptImages
		k.Lock()
		k.bySum, k.byID, k.lru, k.size = map[[32]byte]*list.Element{}, map[int64]*list.Element{}, list.New(), 0
		k.Unlock()
	})
}

// A picture the disk won't take stays in memory, and one whose base64 is
// not what its bytes encode to is kept as written: neither is lost.
func TestKeptImagesFallBackToMemory(t *testing.T) {
	withKeptDir(t, "")
	b64 := dsBigPNG(4<<10, "mem")
	id := keepImage([]byte(b64))
	if got, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok || got != b64 {
		t.Fatalf("no folder: picture lost (%t)", ok)
	}
	// a folder that works, and base64 with its padding cut: canonical
	// base64 is spilled, this is not, and comes back as written
	dir := t.TempDir()
	withKeptDir(t, dir)
	loose := []byte(dsBigPNG(4<<10, "loose"))
	loose = loose[:len(loose)-2] // padding gone
	id = keepImage(loose)
	if got, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok || got != string(loose) {
		t.Fatalf("non-canonical base64 changed (%t)", ok)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatalf("non-canonical base64 was written to disk: %d files", len(es))
	}
	// canonical: on disk, 0600
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

// Two keeps of one picture at once, the older entry of it evicted in
// between, leave the picture drawn: the eviction takes out its own file,
// not the one the other keep wrote. The gap between the write and the entry
// is held open by a hook, so the order is the test's, not the scheduler's.
func TestKeptImagesSamePictureKeptTwiceThenEvicted(t *testing.T) {
	dir := t.TempDir()
	withKeptDir(t, dir)
	was := keptImagesMax
	keptImagesMax = 6 << 10 // room for one 4 KiB picture
	t.Cleanup(func() { keptImagesMax = was })
	wasGap := keepGap
	t.Cleanup(func() { keepGap = wasGap })
	var first atomic.Bool
	inGap, release := make(chan struct{}), make(chan struct{})
	keepGap = func() {
		if first.CompareAndSwap(false, true) { // only the first keep waits here
			close(inGap)
			<-release
		}
	}
	x := []byte(dsBigPNG(4<<10, "x"))
	got := make(chan int64)
	go func() { got <- keepImage(x) }() // B: written, waiting
	<-inGap
	a := keepImage(x)                       // A: the same picture, kept and written
	keepImage([]byte(dsBigPNG(4<<10, "y"))) // evicts A
	if _, ok := keptImageOf(strconv.FormatInt(a, 10)); ok {
		t.Fatal("setup: A was not evicted")
	}
	close(release)
	b := <-got
	if g, ok := keptImageOf(strconv.FormatInt(b, 10)); !ok || g != string(x) {
		t.Fatalf("the picture B kept is gone after A's eviction (found %t)", ok)
	}
	// one picture fits: B kept, Y pushed out with its file
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Fatalf("%d files for the pictures kept", len(es))
	}
}

// RemoveKeptImages takes the folder, and what is kept after it stays
// drawn from memory.
func TestRemoveKeptImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p-x")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	withKeptDir(t, dir)
	b64 := dsBigPNG(4<<10, "gone")
	keepImage([]byte(b64))
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("%d files before", len(es))
	}
	RemoveKeptImages()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
	id := keepImage([]byte(dsBigPNG(4<<10, "after")))
	if _, ok := keptImageOf(strconv.FormatInt(id, 10)); !ok {
		t.Fatal("a picture kept after the folder went was lost")
	}
}
