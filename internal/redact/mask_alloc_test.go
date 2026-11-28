package redact

import (
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

// allocPerRun is what one call of f allocates, in bytes and allocations,
// after a warm-up that fills what masking remembers.
func allocPerRun(n int, f func()) (bytes, mallocs uint64) {
	f()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(n), (after.Mallocs - before.Mallocs) / uint64(n)
}

// manyStringsBody is a request whose text is many short strings, each
// JSON escaped (\n), with nothing in them to mask: a walk of it unescaped
// a copy of every one before, which is what a request's own text cost on
// every pass.
func manyStringsBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"model":"m","messages":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"role":"user","content":"A line of the conversation, and\nthe one after it, nothing to mask here."}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// A hit's head is a copy of the value's first runes, not a piece of it: a
// masked value is often a piece of a request's text, and a head that
// pointed into it kept the whole request in every route that recorded the
// hit (TestKeptRoutesHoldNoTranslatedBody, 274 MB for 60 routes of 4 MB).
func TestHintDoesNotKeepTheValue(t *testing.T) {
	v := "百" + strings.Repeat("x", 1<<20)
	head, n := Hint("TERM", v)
	if head != "百" || n != utf8.RuneCountInString(v) {
		t.Fatalf("Hint = %q, %d; want %q, %d", head, n, "百", utf8.RuneCountInString(v))
	}
	from := uintptr(unsafe.Pointer(unsafe.StringData(v)))
	to := from + uintptr(len(v))
	if p := uintptr(unsafe.Pointer(unsafe.StringData(head))); p >= from && p < to {
		t.Fatalf("a hit's head points into the %d-byte value it came from", len(v))
	}
}

// Masking a request allocates what it is over its bytes, and not two
// copies a pass: the walk unescapes values into one buffer, and the
// masked body is one buffer of about its size.
//
// Measured on this machine before the walk's one buffer: 1 MB of Claude
// Code's request (tools, turns with a tool call and its result, a
// thinking block and an image) allocated 3,818,121 bytes and 16,542
// allocations, 3.64x its bytes; 4,000 escaped strings made 16,020, four
// each. The bounds below are above what it measures now and
// under what it did then. Under -race a pool drops what it holds, so the
// walk makes its buffer again more often: room for that.
func TestMaskJSONAllocates(t *testing.T) {
	body := benchBody(1 << 20)
	if len(body) < 900_000 {
		t.Fatalf("the fixture is %d bytes, want about 1 MB", len(body))
	}
	o := Options{Secrets: true, Personal: true}
	per, mallocs := allocPerRun(3, func() {
		out, n := MaskJSON(body, o)
		if len(out) < len(body)/2 || n == 0 {
			t.Fatalf("masked %d of %d bytes, %d values", len(out), len(body), n)
		}
	})
	t.Logf("%d-byte request: %d bytes and %d allocations", len(body), per, mallocs)
	bytesBound, mallocBound := uint64(3*len(body)), uint64(20_000)
	if raceOn {
		// -race lets go of a pool's buffers at each GC, so a value that
		// would reuse one allocates it again: 54 MB and 21,000 here
		// 62.9 MB (60x) on e29029645 and 84.6 MB (80.6x) here, 85.6 MB
		// (81.56x) on ubuntu CI: the shape rules added since read more
		// text, and -race drops the pools these numbers are made of. The
		// non-race bound above is what guards the cost
		bytesBound, mallocBound = uint64(100*len(body)), 40_000
	}
	if per > bytesBound {
		t.Errorf("a %d-byte masked request allocated %d bytes, want at most %d (%.2fx)", len(body), per, bytesBound, float64(per)/float64(len(body)))
	}
	if mallocs > mallocBound {
		t.Errorf("a %d-byte masked request made %d allocations, want at most %d", len(body), mallocs, mallocBound)
	}

	// many short escaped strings, none masked: a buffer a string was the
	// walk's cost on every pass
	text := manyStringsBody(4_000)
	_, mallocs = allocPerRun(3, func() {
		if out, _ := MaskJSON(text, o); len(out) < len(text)/2 {
			t.Fatalf("masked %d of %d bytes", len(out), len(text))
		}
	})
	bound := uint64(2_000)
	if raceOn {
		// the same: 32,000 here, eight a string
		bound = 50_000
	}
	t.Logf("%d strings of %d bytes: %d allocations", strings.Count(string(text), `"role"`), len(text), mallocs)
	if mallocs > bound {
		t.Errorf("%d escaped strings made %d allocations, want at most %d", strings.Count(string(text), `"role"`), mallocs, bound)
	}
}
