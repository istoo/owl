package pii

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/istoo/owl/internal/redact"
)

// What Find learned is kept on disk too, so that a restart doesn't ask the
// model about every history again: after one, each request's whole
// history was a miss, and on a CPU the requests queued for minutes behind
// each other (#3682). The file holds a text's sha256, and where in it the
// model found what, with the label: never the text. It is named for the
// model it came from, so another model's answers are never read as this
// one's, and lives with the model, in owl's cache (Remove deletes it).

// cacheMagic starts the file; a file that doesn't start with it (or
// cacheMagic1), or ends part way, is read as empty.
const cacheMagic = "pfound02"

// cacheMagic1 starts a file written before a span kept its score, and
// only what was over the model's default threshold. Its spans are read
// as sure (score 1), masked at any threshold as they were before; a
// text not in it is asked about with its scores.
const cacheMagic1 = "pfound01"

func cachePath() string { return filepath.Join(dir(), "found-"+modelSHA[:12]+".bin") }

// saveAfter is how long after an answer the file is written: one write
// for a burst of requests.
var saveAfter = 2 * time.Second

// saveEvery is the least time between two writes of one cache file. A
// change is written saveAfter after it, but not before saveEvery has
// passed since the last write began: answers arrive every few seconds,
// and each write encodes and fsyncs the whole cache. What waits is only
// what the model found, which asks again after a restart (#3682).
var saveEvery = 30 * time.Second

// diskState is a backend's cache file: read once, written saveAfter
// after an answer, and no sooner than saveEvery after the write before.
type diskState struct {
	mu     sync.Mutex
	loaded string // the file read into cache, once
	timer  *time.Timer
	last   time.Time // when the last write began
	saving sync.Mutex
	// saves counts a timer's write from when it is set until it is
	// done, so a test's cleanup can wait for one under way.
	saves sync.WaitGroup
}

var disk diskState

// loadCache reads the file into cache, the first time Find is asked.
func (bk *backend) loadCache() {
	disk, cache := bk.disk, bk.cache
	disk.mu.Lock()
	defer disk.mu.Unlock()
	p := bk.path()
	if disk.loaded == p {
		return
	}
	disk.loaded = p
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	es, err := decodeCache(b)
	if err != nil {
		return // a file cut short or not ours: nothing is kept, the model asked again
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, e := range es {
		if _, ok := cache.m[e.k]; !ok {
			cache.put(e.k, e.fs)
		}
	}
}

// saveSoon writes the file saveAfter from now, unless a write is due:
// no sooner than saveEvery after the last one.
func (bk *backend) saveSoon() {
	disk := bk.disk
	disk.mu.Lock()
	defer disk.mu.Unlock()
	bk.armSave(time.Now())
}

// armSave sets the one write for the later of saveAfter from now and
// saveEvery after the last write. The caller holds disk.mu.
func (bk *backend) armSave(now time.Time) {
	disk := bk.disk
	if disk.timer != nil {
		return
	}
	wait := saveAfter
	if due := disk.last.Add(saveEvery); due.After(now) {
		if d := due.Sub(now); d > wait {
			wait = d
		}
	}
	disk.saves.Add(1)
	disk.timer = time.AfterFunc(wait, func() {
		defer disk.saves.Done()
		now := time.Now()
		disk.mu.Lock()
		disk.timer = nil
		if !disk.last.IsZero() && now.Before(disk.last.Add(saveEvery)) {
			// a write the timer didn't schedule began since: wait the
			// rest of saveEvery from it.
			bk.armSave(now)
			disk.mu.Unlock()
			return
		}
		disk.mu.Unlock()
		bk.saveCache()
	})
}

// saveCache writes what cache holds, oldest first, to a file beside the
// old one and renames it over it: a reader never sees half of it.
func saveCache() error { return pf.saveCache() }

func (bk *backend) saveCache() error {
	disk, cache := bk.disk, bk.cache
	disk.saving.Lock()
	defer disk.saving.Unlock()
	disk.mu.Lock()
	disk.last = time.Now()
	disk.mu.Unlock()
	cache.mu.Lock()
	b := encodeCache(cache)
	cache.mu.Unlock()
	p := bk.path()
	f, err := os.CreateTemp(filepath.Dir(p), "found-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// encodeCache is c, oldest first: the magic, a count, then each entry's
// hash, its spans' count and each span's start, end, score (a float32)
// and label. The size is counted first, so the file is written in one
// allocation rather than a buffer that doubles and a reflection call per
// field; it is called with the cache locked on every save.
func encodeCache(c *lru) []byte {
	written := 0
	n := len(cacheMagic) + 4
	for e := c.ll.Back(); e != nil; e = e.Prev() {
		en := e.Value.(*entry)
		if len(en.fs) > math.MaxUint16 {
			// the count is a uint16: writing it cut would leave the file
			// with spans its count doesn't name, which decodeCache reads as
			// a cut-short file, and every entry in it with that one (#65).
			// This entry is kept in memory and not written.
			continue
		}
		written++
		n += len(en.k) + 2
		for _, f := range en.fs {
			n += 13 + len(f.Label)
			if len(f.Label) > 255 {
				n -= len(f.Label) - 255
			}
		}
	}
	b := make([]byte, 0, n)
	b = append(b, cacheMagic...)
	b = binary.LittleEndian.AppendUint32(b, uint32(written))
	for e := c.ll.Back(); e != nil; e = e.Prev() {
		en := e.Value.(*entry)
		if len(en.fs) > math.MaxUint16 {
			continue
		}
		b = append(b, en.k[:]...)
		b = binary.LittleEndian.AppendUint16(b, uint16(len(en.fs)))
		for _, f := range en.fs {
			label := f.Label
			if len(label) > 255 {
				label = label[:255]
			}
			b = binary.LittleEndian.AppendUint32(b, uint32(f.Start))
			b = binary.LittleEndian.AppendUint32(b, uint32(f.End))
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(float32(f.Score)))
			b = append(b, byte(len(label)))
			b = append(b, label...)
		}
	}
	return b
}

var errBadCache = errors.New("not a cache file")

func decodeCache(b []byte) ([]entry, error) {
	scored := bytes.HasPrefix(b, []byte(cacheMagic))
	if !scored && !bytes.HasPrefix(b, []byte(cacheMagic1)) {
		return nil, errBadCache
	}
	r := bufio.NewReader(bytes.NewReader(b[len(cacheMagic):]))
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n > uint32(len(b)/34) { // each entry is 34 bytes at least
		return nil, errBadCache
	}
	out := make([]entry, 0, n)
	for i := uint32(0); i < n; i++ {
		var e entry
		if _, err := io.ReadFull(r, e.k[:]); err != nil {
			return nil, err
		}
		var spans uint16
		if err := binary.Read(r, binary.LittleEndian, &spans); err != nil {
			return nil, err
		}
		for j := uint16(0); j < spans; j++ {
			var se [2]uint32
			if err := binary.Read(r, binary.LittleEndian, &se); err != nil {
				return nil, err
			}
			score := float32(1)
			if scored {
				if err := binary.Read(r, binary.LittleEndian, &score); err != nil {
					return nil, err
				}
				if !(score >= 0 && score <= 1) {
					return nil, errBadCache
				}
			}
			l, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			label := make([]byte, l)
			if _, err := io.ReadFull(r, label); err != nil {
				return nil, err
			}
			if se[0] >= se[1] {
				return nil, errBadCache
			}
			e.fs = append(e.fs, redact.Found{Start: int(se[0]), End: int(se[1]), Label: string(label), Score: float64(score)})
		}
		out = append(out, e)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return nil, errBadCache // more than it said
	}
	return out, nil
}
