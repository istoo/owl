package usage

import (
	"math"
	"testing"
	"time"
)

// A call whose prompt was all a cache hit (Input and Output 0, CacheRead
// over 0), or one that only wrote cache, is billed on those tokens: it is
// priced, in the ledger and in the requests page, and it is not counted
// as unpriced. Only a call with no tokens at all costs nothing (#61).
func TestCacheOnlyCallIsPriced(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	// openai/m's cache read is $0.50 a million in pageHome's catalog
	Append(Record{Time: now, Agent: "claude", Provider: "openai", Model: "m", CacheRead: 1_000_000, Status: 200})
	Append(Record{Time: now.Add(time.Second), Agent: "claude", Provider: "openai", Model: "m", CacheWrite: 1_000_000, Status: 200})
	// no tokens at all: nothing to price
	Append(Record{Time: now.Add(2 * time.Second), Agent: "claude", Provider: "openai", Model: "m", Status: 200})

	rows, sum, _ := Ledger(All, Filter{})
	if len(rows) != 3 {
		t.Fatalf("ledger has %d rows, want 3", len(rows))
	}
	var priced int
	for _, r := range rows {
		if r.Priced {
			priced++
		}
	}
	if priced != 2 {
		t.Errorf("%d of the rows are priced, want 2 (cache read and cache write)", priced)
	}
	// $0.50 of cache read and $2.50 of cache write
	if math.Abs(sum.Cost-3.0) > 1e-9 {
		t.Errorf("ledger cost %v, want 3", sum.Cost)
	}
	if sum.Unpriced != 0 {
		t.Errorf("%d calls unpriced, want 0", sum.Unpriced)
	}

	page := QueryPage(All, Filter{}, 0, 10)
	if math.Abs(page.Sum.Cost-3.0) > 1e-9 {
		t.Errorf("requests page cost %v, want 3", page.Sum.Cost)
	}
	if page.Sum.Unpriced != 0 {
		t.Errorf("requests page: %d calls unpriced, want 0", page.Sum.Unpriced)
	}
}
