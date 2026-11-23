package gateway

// A request's inline images, kept apart from its text for the views that
// show the request again: the Routing card and the Sessions page
// (matrix0487 on Discord). Sending images to DeepSeek by file id (#5407)
// let a conversation go past the 64 MiB DeepSeek takes inline, and its
// bodies past what those views keep: a body over promptBodyMax lost the
// card's text and pictures, and one over conversationBodyMax its turn.
// Almost all of such a body is base64, so each view keeps it with every
// picture swapped for a short ref, and the card reads the picture back
// from keptImages, bounded, as the bodies are: the bytes sit in files of
// owl's cache folder, so they cost no heap.

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/istoo/owl/internal/appdir"
)

const (
	// imageRef stands in a lean body for a picture's base64, followed by
	// its id in keptImages; never sent anywhere, and not base64, so a
	// picture's checks never take it for one
	imageRef = "owl-image:"
	// leanMin is the shortest string swapped: a smaller picture costs
	// less than its ref's bookkeeping
	leanMin = 1 << 10
)

// keptImagesMax is what the kept pictures take at most together, the
// least recently used going first; a var for tests.
var keptImagesMax = 128 << 20

// keepGap runs between a picture's file being written and its entry being
// made, where another keep of the same picture can come and go; a test
// widens it.
var keepGap = func() {}

// keptImage is one picture: its bytes in a file of their own under
// keptImagesDir (a name of its own, 0600), or, when the file can't be
// written or the base64 isn't canonical, its base64 in memory. A picture
// never goes missing because the disk said no.
type keptImage struct {
	id   int64
	sum  [32]byte
	size int    // the base64's length, what keptImagesMax counts
	b64  string // in memory only when path is empty
	path string
}

var keptImages = struct {
	sync.Mutex
	bySum map[[32]byte]*list.Element
	byID  map[int64]*list.Element
	lru   *list.List // front: the newest
	size  int
	next  int64
}{bySum: map[[32]byte]*list.Element{}, byID: map[int64]*list.Element{}, lru: list.New()}

var keptDirState struct {
	sync.Mutex
	done bool
	dir  string // "" when the cache folder can't be had, or owl is ending
}

// keptImagesDir is this process's folder of kept pictures, made on first
// use. Folders other processes left (a crash, a kill) older than a day
// are taken out then; a live process's folder untouched that long loses
// its pictures to this, which the card shows as gone, as an evicted one.
// debt: a folder per process swept by age; a lock file per process would
// tell a live one from a dead one.
func keptImagesDir() string {
	st := &keptDirState
	st.Lock()
	defer st.Unlock()
	if st.done {
		return st.dir
	}
	st.done = true
	root, err := appdir.SystemCache()
	if err != nil {
		return ""
	}
	root = filepath.Join(root, "kept-images")
	if es, err := os.ReadDir(root); err == nil {
		for _, e := range es {
			if fi, err := e.Info(); err == nil && e.IsDir() && time.Since(fi.ModTime()) > 24*time.Hour {
				os.RemoveAll(filepath.Join(root, e.Name()))
			}
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return ""
	}
	if d, err := os.MkdirTemp(root, "p-"); err == nil {
		st.dir = d
	}
	return st.dir
}

// RemoveKeptImages deletes this process's folder of kept pictures, for
// owl ending: they were in memory only before, and go with the process.
// Later keeps stay in memory. A crash leaves the folder to the sweep of
// the next start.
func RemoveKeptImages() {
	st := &keptDirState
	st.Lock()
	defer st.Unlock()
	if st.dir != "" {
		os.RemoveAll(st.dir)
	}
	st.done, st.dir = true, ""
}

// spillImage writes a picture's decoded bytes to a file of its own, whose
// name is unique to this write: a keep of the same picture at the same
// time, or the eviction of an older entry of it, never reaches this file.
// "" when it can't, or when the base64 doesn't decode back to itself (then
// its text is kept as it is).
func spillImage(b64 []byte) string {
	dir := keptImagesDir()
	if dir == "" {
		return ""
	}
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(b64)))
	n, err := base64.StdEncoding.Decode(raw, b64)
	if err != nil || base64.StdEncoding.EncodedLen(n) != len(b64) {
		return ""
	}
	f, err := os.CreateTemp(dir, "img-*") // 0600
	if err != nil {
		return ""
	}
	_, werr := f.Write(raw[:n])
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(f.Name())
		return ""
	}
	return f.Name()
}

// keepImage keeps a picture's base64, giving its id; the same picture
// keeps one id.
func keepImage(b64 []byte) int64 {
	sum := sha256.Sum256(b64)
	k := &keptImages
	k.Lock()
	if e, ok := k.bySum[sum]; ok {
		k.lru.MoveToFront(e)
		id := e.Value.(*keptImage).id
		k.Unlock()
		return id
	}
	k.Unlock()
	// the disk is written outside the lock: a reader of another ref waits
	// on nothing slow
	path := spillImage(b64)
	keepGap()
	var drop []string // files to delete, outside the lock
	k.Lock()
	defer func() {
		k.Unlock()
		for _, p := range drop {
			os.Remove(p)
		}
	}()
	if e, ok := k.bySum[sum]; ok { // kept meanwhile by another turn
		k.lru.MoveToFront(e)
		if path != "" {
			drop = append(drop, path)
		}
		return e.Value.(*keptImage).id
	}
	k.next++
	im := &keptImage{id: k.next, sum: sum, size: len(b64), path: path}
	if path == "" {
		im.b64 = string(b64)
	}
	e := k.lru.PushFront(im)
	k.bySum[sum], k.byID[im.id] = e, e
	k.size += im.size
	for k.size > keptImagesMax && k.lru.Len() > 1 {
		old := k.lru.Back().Value.(*keptImage)
		k.lru.Remove(k.lru.Back())
		delete(k.bySum, old.sum)
		delete(k.byID, old.id)
		k.size -= old.size
		if old.path != "" {
			drop = append(drop, old.path)
		}
	}
	return im.id
}

// keptImageOf is the base64 of the picture a ref names; false when it
// has gone (evicted, or its file taken away).
func keptImageOf(ref string) (string, bool) {
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return "", false
	}
	k := &keptImages
	k.Lock()
	e, ok := k.byID[id]
	if !ok {
		k.Unlock()
		return "", false
	}
	k.lru.MoveToFront(e)
	im := e.Value.(*keptImage)
	b64, path := im.b64, im.path
	k.Unlock()
	if path == "" {
		return b64, true
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(raw), true
}

// imageMagic is how a PNG, JPEG, GIF or WebP's base64 starts: an
// Anthropic or Gemini source's data, which carries no data: head.
var imageMagic = []string{"iVBORw0KGg", "/9j/", "R0lGOD", "UklGR"}

// imagePayload is where a JSON string's picture base64 starts: after a
// data:image/…;base64, head, or at 0 for bare base64 that starts as a
// picture does. False for anything else.
func imagePayload(s []byte) (int, bool) {
	at := 0
	if bytes.HasPrefix(s, []byte("data:")) {
		head, _, ok := bytes.Cut(s, []byte(","))
		if !ok {
			return 0, false
		}
		h := strings.ToLower(string(head))
		if !strings.HasPrefix(h, "data:image/") || !strings.HasSuffix(h, ";base64") {
			return 0, false
		}
		at = len(head) + 1
	} else {
		magic := false
		for _, m := range imageMagic {
			magic = magic || bytes.HasPrefix(s, []byte(m))
		}
		if !magic {
			return 0, false
		}
	}
	if len(s)-at < leanMin {
		return 0, false
	}
	for _, c := range s[at:] {
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '+' || c == '/' || c == '=') {
			return 0, false
		}
	}
	return at, true
}

// leanImages is a JSON body with each inline picture's base64 swapped for
// a ref: kept in keptImages for the card to draw again when keep, a bare
// ref otherwise (the Sessions page shows a picture as [image]). The body
// itself when it has none. Only JSON strings without escapes are read, so
// the body's shape is never changed.
func leanImages(body []byte, keep bool) []byte {
	var out []byte
	last := 0
	for i := 0; i < len(body); i++ {
		if body[i] != '"' {
			continue
		}
		j, escaped := i+1, false
		for ; j < len(body) && body[j] != '"'; j++ {
			if body[j] == '\\' {
				escaped = true
				j++
			}
		}
		if j >= len(body) {
			break
		}
		if s := body[i+1 : j]; !escaped && len(s) >= leanMin {
			if at, ok := imagePayload(s); ok {
				if out == nil {
					out = make([]byte, 0, len(body)/8)
				}
				ref := imageRef
				if keep {
					ref += strconv.FormatInt(keepImage(s[at:]), 10)
				}
				out = append(out, body[last:i+1+at]...)
				out = append(out, ref...)
				last = j
			}
		}
		i = j
	}
	if out == nil {
		return body
	}
	return append(out, body[last:]...)
}
