package sessions

import (
	"bytes"
	"hash/fnv"
	"slices"
)

// uuidSet is a set of message uuids, each kept as its 16 bytes rather
// than its 36-character text: Claude Code's sessions that start alike
// keep every message's uuid (ccUUIDMemo), and as strings in a
// map[string]bool they were ~5 MB of an idle heap of ~40. An id that is
// not a uuid is kept by a 128-bit hash of it.
//
// A map of 16-byte keys was still ~31 bytes a uuid once its buckets,
// tophash and spare slots are counted (measured on 600k). The keys are a
// sorted slice instead, 16 bytes each, and a lookup bisects it. The ids
// added since the last sort wait in added, which a lookup reads in turn:
// they are merged into keys when a lookup meets addedMost of them, or a
// fraction of keys while adding, so a scan that adds one line at a time
// doesn't copy the whole set each time.
type uuidSet struct {
	keys  [][16]byte // sorted
	added [][16]byte
}

// addedMost is the number of ids added since the last merge that a
// lookup reads in turn before it merges them.
const addedMost = 64

func (s *uuidSet) add(id string) {
	s.added = append(s.added, uuidKey(id))
	if len(s.added) >= addedMost && len(s.added) >= len(s.keys)/8 {
		s.settle()
	}
}

func (s *uuidSet) has(id string) bool {
	if len(s.added) >= addedMost {
		s.settle()
	}
	k := uuidKey(id)
	for _, x := range s.added {
		if x == k {
			return true
		}
	}
	_, found := slices.BinarySearchFunc(s.keys, k, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
	return found
}

// Len is how many uuids are in the set, each one.
func (s *uuidSet) Len() int {
	s.settle()
	return len(s.keys)
}

// settle merges added into keys, sorted and without repeats.
//
// debt: a merge copies every key, O(n), and a lookup merges once added
// reaches addedMost. The scans here add a whole file before a lookup, so
// the cost is one merge a scan; a caller that added 64 ids and looked one
// up over and over would pay O(n) each time. If one appears, keep added
// sorted and bisect it too instead of merging on the lookup.
func (s *uuidSet) settle() {
	if len(s.added) == 0 {
		return
	}
	slices.SortFunc(s.added, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
	merged := make([][16]byte, 0, len(s.keys)+len(s.added))
	i, j := 0, 0
	for i < len(s.keys) && j < len(s.added) {
		switch c := bytes.Compare(s.keys[i][:], s.added[j][:]); {
		case c < 0:
			merged = append(merged, s.keys[i])
			i++
		case c > 0:
			merged = append(merged, s.added[j])
			j++
		default:
			merged = append(merged, s.keys[i])
			i++
			j++
		}
	}
	merged = append(merged, s.keys[i:]...)
	merged = append(merged, s.added[j:]...)
	// a uuid added twice before the merge is one entry, as the map's was
	out := merged[:0]
	for _, k := range merged {
		if len(out) == 0 || out[len(out)-1] != k {
			out = append(out, k)
		}
	}
	s.keys, s.added = out, nil
}

// uuidKey is id's 16 bytes when it is a uuid (8-4-4-4-12 hex digits, any
// case), else an FNV-128a hash of it, which a uuid's bytes are told
// apart from by nothing but chance.
func uuidKey(id string) (k [16]byte) {
	if len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-' {
		j, ok := 0, true
		for i := 0; i < 36 && ok; i++ {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				continue
			}
			h, okh := unhex(id[i])
			l, okl := unhex(id[i+1])
			ok = okh && okl
			k[j] = h<<4 | l
			j++
			i++
		}
		if ok {
			return k
		}
		k = [16]byte{}
	}
	h := fnv.New128a()
	h.Write([]byte(id))
	h.Sum(k[:0])
	return k
}

func unhex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
