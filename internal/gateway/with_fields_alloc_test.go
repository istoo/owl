package gateway

import (
	"encoding/json"
	"testing"
)

// Rewriting a top-level field allocates about the body, not the decoded
// body and the encoded one: it reads the top level, copies what it does
// not change, and writes the new values.
//
// Measured on this machine before the top-level read: one field set on
// the 1 MB Claude Code request allocated 7,097,241 bytes and 16,214
// allocations, 7.1x its bytes, and took 4.7 ms; withoutTurnEffort, which
// also rewrites the messages, 11,586,204 bytes and 16,246 allocations.
// The bounds below are above what both measure now and under what they
// did then.
func TestWithFieldsAllocates(t *testing.T) {
	body := claudeCodeRequest(150)
	if len(body) < 900_000 {
		t.Fatalf("the fixture is %d bytes, want about 1 MB", len(body))
	}
	per, mallocs := oneRequestAllocs(t, 4, func() {
		out := withFields(body, map[string]any{"stream": true})
		if len(out) == 0 || !json.Valid(out) {
			t.Fatalf("withFields wrote %d bytes", len(out))
		}
	})
	t.Logf("%d-byte request: %d bytes and %d allocations a rewrite", len(body), per, mallocs)
	bytesBound, mallocBound := uint64(3*len(body))/2, uint64(300)
	if raceOn {
		bytesBound, mallocBound = uint64(2*len(body)), 600
	}
	if per > bytesBound {
		t.Errorf("a %d-byte rewrite allocated %d bytes, want at most %d (%.2fx)", len(body), per, bytesBound, float64(per)/float64(len(body)))
	}
	if mallocs > mallocBound {
		t.Errorf("a %d-byte rewrite made %d allocations, want at most %d", len(body), mallocs, mallocBound)
	}
}

// Taking a refused field out of every message allocates the body and the
// messages once more, not the messages decoded, encoded and copied again.
// Before: 11,586,204 bytes and 16,246 allocations for the 1 MB request.
func TestWithoutTurnEffortAllocates(t *testing.T) {
	body := benchTurnEffortBody(150)
	if len(body) < 900_000 {
		t.Fatalf("the fixture is %d bytes, want about 1 MB", len(body))
	}
	per, mallocs := oneRequestAllocs(t, 4, func() {
		out := withoutTurnEffort(body)
		if len(out) >= len(body) || !json.Valid(out) {
			t.Fatalf("withoutTurnEffort wrote %d of %d bytes", len(out), len(body))
		}
	})
	t.Logf("%d-byte request: %d bytes and %d allocations a rewrite", len(body), per, mallocs)
	bytesBound, mallocBound := uint64(5*len(body)), uint64(6_000)
	if raceOn {
		bytesBound, mallocBound = uint64(6*len(body)), 8_000
	}
	if per > bytesBound {
		t.Errorf("a %d-byte rewrite allocated %d bytes, want at most %d (%.2fx)", len(body), per, bytesBound, float64(per)/float64(len(body)))
	}
	if mallocs > mallocBound {
		t.Errorf("a %d-byte rewrite made %d allocations, want at most %d", len(body), mallocs, mallocBound)
	}
}
