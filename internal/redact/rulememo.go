package redact

import (
	"crypto/sha256"
	"hash"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
)

// memoRules remembers what the rules found in a long text. An agent sends
// its whole history on every turn, so without it every turn ran each rule
// over every message before it again: on a long Claude Code session, with
// masking on, most of owl's CPU went to the rules' regexps (#4353). What
// they find in a text depends on the text, what o covers, the user's own
// rules and the day (dateOK: a date over a year ago can be a birthday),
// and on nothing a request changes, so it is kept by a hash of those. Only
// the hash and the places found are kept, nothing of a value.
const (
	ruleMemoMin     = 256 // a shorter text costs less to read than to look up
	ruleMemoEntries = 1 << 16
)

var ruleMemo struct {
	sync.Mutex
	cur, old map[[32]byte][]span
}

// ruleRuns counts the texts of ruleMemoMin bytes or more the rules have
// run over (a test reads it).
var ruleRuns atomic.Uint64

// digestPool shares a SHA-256 among a request's texts: a digest object
// made for each of the hundred-odd long texts an agent sends a turn was
// an allocation a text.
var digestPool = sync.Pool{New: func() any { return sha256.New() }}

func memoRules(s string, o Options, find func() []span) []span {
	if len(s) < ruleMemoMin {
		return find()
	}
	h := digestPool.Get().(hash.Hash)
	h.Reset()
	sc := memoScratchPool.Get().(*memoScratch)
	key := sc.buf[:0]
	key = append(key, dateClock().Format("2006-01-02")...)
	key = append(key, 0, bit(o.Secrets), 0, bit(o.Personal), 0)
	kinds := make([]string, 0, len(o.Kinds))
	for k := range o.Kinds {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		key = memoField(key, k)
		key = append(key, bit(o.Kinds[k]), 0)
	}
	key = append(key, 0)
	if o.Secrets {
		for _, r := range o.Rules {
			key = memoField(key, r.Kind)
			key = memoField(key, r.Prefix)
			key = memoField(key, r.Regex)
		}
	}
	key = append(key, 0)
	h.Write(key)
	h.Write(bytesOf(s))
	var k [32]byte
	copy(k[:], memoSum(h, sc))
	sc.buf = key[:0]
	memoScratchPool.Put(sc)
	digestPool.Put(h)
	ruleMemo.Lock()
	sp, ok := ruleMemo.cur[k]
	if !ok {
		if sp, ok = ruleMemo.old[k]; ok {
			ruleAdd(k, sp) // still in use: kept past the next turnover
		}
	}
	ruleMemo.Unlock()
	if ok {
		return slices.Clone(sp)
	}
	sp = find()
	ruleMemo.Lock()
	ruleAdd(k, slices.Clip(slices.Clone(sp)))
	ruleMemo.Unlock()
	return sp
}

// bit is b as a memo key's byte.
func bit(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// memoScratch is what a memo key is written into, and where the digest
// is put: a buffer and a sum of the heap already, so neither the key nor
// the sum is a slice that escapes when it is passed to hash.Write or
// hash.Sum, and the pool holds a pointer rather than a slice boxed in an
// interface. Each was an allocation a text of every turn.
type memoScratch struct {
	buf []byte
	sum [sha256.Size]byte
}

var memoScratchPool = sync.Pool{New: func() any { return &memoScratch{} }}

// memoSum is h's digest into sc, which the caller copies to its map key.
func memoSum(h hash.Hash, sc *memoScratch) []byte {
	return h.Sum(sc.sum[:0])
}

// memoField adds s to a memo key, its length first so that no two fields
// can read as one.
func memoField(key []byte, s string) []byte {
	key = strconv.AppendInt(key, int64(len(s)), 10)
	key = append(key, ':')
	key = append(key, s...)
	return append(key, 0)
}

// ruleAdd is called with ruleMemo held. Two generations, as sentMemo
// keeps: when the newer is full it becomes the older.
func ruleAdd(k [32]byte, sp []span) {
	if ruleMemo.cur == nil || len(ruleMemo.cur) >= ruleMemoEntries {
		ruleMemo.old, ruleMemo.cur = ruleMemo.cur, make(map[[32]byte][]span)
	}
	ruleMemo.cur[k] = sp
}
