package budget

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/settings"
	"github.com/istoo/owl/internal/usage"
)

// A window's counted prefix is kept until the log or its cursor changes;
// a price the user edits changes what those same calls cost, so AllOf
// counts the prefix again rather than keep the old price's total (#56).
func TestAllOfRecountsAfterAPriceChange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	Forget()
	t.Cleanup(Forget)

	price := func(perMillion float64) {
		t.Helper()
		s := settings.Load()
		in, zero := perMillion, 0.0
		s.ModelPrices = map[string]settings.ModelPrice{"p/m": {Input: &in, Output: &zero, CacheRead: &zero, CacheWrite: &zero}}
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	price(2)
	if _, err := access.Update("add-key", access.Change{Name: "priced"}); err != nil {
		t.Fatal(err)
	}
	ks, err := access.List()
	if err != nil || len(ks) != 1 {
		t.Fatalf("keys %+v: %v", ks, err)
	}
	k := ks[0]
	if _, err := access.Update("limit-key", access.Change{Key: k.ID, Limit: &access.Limit{Period: "day", Tokens: 1 << 50}}); err != nil {
		t.Fatal(err)
	}
	ks, err = access.List()
	if err != nil || len(ks) != 1 {
		t.Fatalf("keys %+v: %v", ks, err)
	}
	k = ks[0]

	// more than one log block of calls, so the prefix is kept and reused
	now := time.Now()
	now = time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.Local)
	for i := 0; i < 600; i++ {
		usage.Append(usage.Record{Time: now.Add(-time.Duration(i) * time.Millisecond), Provider: "p", Model: "m", Input: 1_000_000, Status: 200, CallerKeyID: k.ID})
	}
	cost := func() float64 {
		t.Helper()
		all := AllOf([]access.Key{k}, now)
		if len(all) != 1 || len(all[0]) != 1 {
			t.Fatalf("statuses %+v", all)
		}
		return all[0][0].Cost
	}
	// 600 calls of a million input at $2 a million
	if got := cost(); math.Abs(got-1200) > 1e-6 {
		t.Fatalf("at $2 a million, cost $%v, want $1200", got)
	}
	price(4)
	if got := cost(); math.Abs(got-2400) > 1e-6 {
		t.Fatalf("after the price rose to $4, cost $%v, want $2400", got)
	}
}
