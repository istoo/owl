package pii

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/redact"
)

// oldEncodeCache is encodeCache as it was before the size was counted
// first: a bytes.Buffer that grows by doubling, and binary.Write per
// field through an any. The file it writes is the format; a new encoder
// must match it byte for byte.
func oldEncodeCache(c *lru) []byte {
	var b bytes.Buffer
	b.WriteString(cacheMagic)
	put := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	put(uint32(c.ll.Len()))
	for e := c.ll.Back(); e != nil; e = e.Prev() {
		en := e.Value.(*entry)
		b.Write(en.k[:])
		put(uint16(len(en.fs)))
		for _, f := range en.fs {
			label := f.Label
			if len(label) > 255 {
				label = label[:255]
			}
			put(uint32(f.Start))
			put(uint32(f.End))
			put(float32(f.Score))
			b.WriteByte(byte(len(label)))
			b.WriteString(label)
		}
	}
	return b.Bytes()
}

// sampleLRU is n entries, oldest first by key, each with the spans and
// labels a model gives back.
func sampleLRU(n int) *lru {
	c := newLRU(n)
	labels := []string{"private_person", "private_email", "private_phone", "private_address", "private_url"}
	for i := range n {
		k := sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		fs := make([]redact.Found, 2+i%2)
		for j := range fs {
			fs[j] = redact.Found{
				Start: 10 * (j + 1),
				End:   10*(j+1) + 5,
				Label: labels[(i+j)%len(labels)],
				Score: float64(float32(float64(i%7) / 7)),
			}
		}
		c.put(k, fs)
	}
	return c
}

// The encoder still writes the format decodeCache reads and older
// versions wrote: the same bytes as the buffer encoder, for an empty
// cache, odd labels and a full one.
func TestEncodeCacheMatchesTheOldBytes(t *testing.T) {
	long := strings.Repeat("é", 200) // 400 bytes: cut through a rune at 255
	cases := []struct {
		name string
		c    *lru
	}{
		{"empty", newLRU(10)},
		{"one with no span", func() *lru {
			c := newLRU(10)
			c.put(sha256.Sum256([]byte("x")), nil)
			return c
		}()},
		{"labels at the byte cap", func() *lru {
			c := newLRU(10)
			c.put(sha256.Sum256([]byte("a")), []redact.Found{
				{Start: 0, End: 1, Label: strings.Repeat("a", 255), Score: 1},
				{Start: 2, End: 3, Label: strings.Repeat("b", 256), Score: 0.5},
				{Start: 4, End: 5, Label: long, Score: 0.98},
			})
			return c
		}()},
		{"full", sampleLRU(20000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeCache(tc.c)
			want := oldEncodeCache(tc.c)
			if !bytes.Equal(got, want) {
				t.Fatalf("encodeCache wrote %d bytes, the format's %d", len(got), len(want))
			}
			es, err := decodeCache(got)
			if err != nil {
				t.Fatalf("decodeCache: %v", err)
			}
			if len(es) != tc.c.ll.Len() {
				t.Fatalf("read back %d entries, wrote %d", len(es), tc.c.ll.Len())
			}
		})
	}
}

// encodeCache is handed the whole cache under its lock on every save, so
// it allocates its output once: no buffer that doubles, no reflection.
func TestEncodeCacheAllocsOnce(t *testing.T) {
	c := sampleLRU(20000)
	if n := testing.AllocsPerRun(5, func() { _ = encodeCache(c) }); n > 3 {
		t.Fatalf("encodeCache allocated %v times per call, want at most 3", n)
	}
}

// A write encodes and fsyncs the whole cache, so two writes of one file
// are at least saveEvery apart, and the last change still lands.
func TestSaveCacheWaitsBetweenWrites(t *testing.T) {
	wasAfter, wasEvery := saveAfter, saveEvery
	saveAfter, saveEvery = 20*time.Millisecond, time.Second
	t.Cleanup(func() { saveAfter, saveEvery = wasAfter, wasEvery })
	d := t.TempDir()
	b := &backend{cache: newLRU(10), disk: new(diskState), path: func() string { return filepath.Join(d, "found.bin") }}
	t.Cleanup(func() {
		b.disk.mu.Lock()
		if b.disk.timer != nil && b.disk.timer.Stop() {
			b.disk.saves.Done()
		}
		b.disk.timer = nil
		b.disk.mu.Unlock()
		b.disk.saves.Wait()
	})
	b.cache.put(sha256.Sum256([]byte("a")), []redact.Found{{Start: 0, End: 1, Label: "private_person", Score: 1}})
	b.saveSoon()
	waitFor(t, "the first write", func() bool { _, err := os.Stat(b.path()); return err == nil })
	b.disk.mu.Lock()
	first := b.disk.last
	b.disk.mu.Unlock()

	// an answer right after the write waits out saveEvery, not saveAfter
	b.saveSoon()
	if left := first.Add(saveEvery).Sub(time.Now()); left > 200*time.Millisecond {
		time.Sleep(left / 2)
		b.disk.mu.Lock()
		early, last := b.disk.last != first, b.disk.last
		b.disk.mu.Unlock()
		if early {
			t.Fatalf("a second write began %v after the first", last.Sub(first))
		}
	}

	waitFor(t, "the second write", func() bool {
		b.disk.mu.Lock()
		defer b.disk.mu.Unlock()
		return b.disk.last.After(first)
	})
	b.disk.mu.Lock()
	second := b.disk.last
	b.disk.mu.Unlock()
	if gap := second.Sub(first); gap < saveEvery {
		t.Fatalf("two writes %v apart, want at least %v", gap, saveEvery)
	}
}

func BenchmarkEncodeCache(b *testing.B) {
	c := sampleLRU(20000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = encodeCache(c)
	}
}
