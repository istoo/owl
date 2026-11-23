//go:build packedassets

package gui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"testing"

	"github.com/istoo/owl/internal/proc"
)

// The packed UI must be the tree's: every file, byte for byte, and no other.
// Run after `go run ./build/assetpack internal/gui/assets internal/gui/assets.zip`.
func TestPackedAssetsAreTheTree(t *testing.T) {
	tree := os.DirFS("assets")
	var want []string
	fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			want = append(want, p)
		}
		return nil
	})
	if len(want) < 100 {
		t.Fatalf("only %d files in the tree", len(want))
	}
	var got []string
	fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			got = append(got, p[len("assets/"):])
		}
		return nil
	})
	sort.Strings(want)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("packed has %d files, the tree %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i] != n {
			t.Fatalf("file %d: packed %q, tree %q", i, got[i], n)
		}
		a, _ := fs.ReadFile(tree, n)
		b, err := assets.ReadFile("assets/" + n)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("%s differs from the tree (err %v)", n, err)
		}
	}
}

// The page is served as the tree's was: index at /, a range of a file, 404.
func TestPackedAssetsServe(t *testing.T) {
	h := http.FileServer(http.FS(staticFS()))
	get := func(path, rng string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if rng != "" {
			r.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	idx, _ := os.ReadFile("assets/index.html")
	if w := get("/", ""); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), idx) {
		t.Fatalf("/ gave %d, %d bytes (index.html is %d)", w.Code, w.Body.Len(), len(idx))
	}
	// a range of the tree's largest file, whatever it is: a video, once, and
	// the interface's own files have changed under it
	var big string
	var bigSize int64
	fs.WalkDir(os.DirFS("assets"), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil && fi.Size() > bigSize {
				big, bigSize = p, fi.Size()
			}
		}
		return nil
	})
	if bigSize < 200 {
		t.Fatalf("the tree's largest file is %d bytes", bigSize)
	}
	body, _ := os.ReadFile("assets/" + big)
	w := get("/"+big, "bytes=100-199")
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), body[100:200]) {
		t.Fatalf("range of %s gave %d, %d bytes", big, w.Code, w.Body.Len())
	}
	if w := get("/nope.js", ""); w.Code != 404 {
		t.Fatalf("a missing file gave %d", w.Code)
	}
	if lm := get("/app.js", "").Header().Get("Last-Modified"); lm != "" {
		t.Fatalf("Last-Modified %q: an embedded file has none", lm)
	}
}

// The page's files are not kept for the life of the process: after every
// one has been read and let go, the live heap is where it was (the cache
// held ~7.7 MB of them), and a file still open keeps its bytes. Run in a
// child of its own, which measures only this; the bound is the difference
// between the two states.
func TestPackedAssetsAreNotKept(t *testing.T) {
	const child = "OWL_TEST_PACKED_HEAP"
	if os.Getenv(child) == "" {
		cmd := proc.CommandContext(context.Background(), os.Args[0], "-test.run=^TestPackedAssetsAreNotKept$", "-test.timeout=4m", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatalf("unpacked files stayed on the heap: %v", err)
		}
		return
	}
	live := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	open, err := assets.Open("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	before := live()
	total := 0
	fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			b, _ := assets.ReadFile(p)
			total += len(b)
		}
		return nil
	})
	after := live()
	fmt.Printf("read %d bytes of files; live heap grew by %d\n", total, int64(after)-int64(before))
	if total < 4<<20 || int64(after)-int64(before) > int64(total)/8 {
		t.Fatalf("read %d bytes; live heap grew by %d", total, int64(after)-int64(before))
	}
	b, err := io.ReadAll(open) // the open file still has its bytes after the collections
	if err != nil || len(b) < 1<<20 {
		t.Fatalf("an open file lost its bytes: %d, %v", len(b), err)
	}
}
