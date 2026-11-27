package budget

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
	"github.com/istoo/owl/internal/usage"
)

func auditHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	Forget()
	t.Cleanup(Forget)
}

// A window's sum read at other prices is not what this call adds to: it
// would mix two price lists. The stale sum is dropped and the next read
// of the window costs the whole window at today's prices.
func TestAppendDropsSumsAtOldPrices(t *testing.T) {
	auditHome(t)
	old := &sums{start: time.Now().Add(-time.Hour), ver: "prices-before"}
	state.Lock()
	state.sums[slot("k")] = []*sums{old}
	state.Unlock()
	Append(usage.Record{Time: time.Now(), Provider: "p", Model: "m", Input: 5, Status: 200, CallerKeyID: "k"})
	if old.calls != 0 {
		t.Fatalf("a sum at old prices counted the call: %+v", old)
	}
	state.Lock()
	kept := state.sums[slot("k")]
	state.Unlock()
	if len(kept) != 0 {
		t.Fatalf("the stale sum is still kept: %+v", kept)
	}
}

// A call the log didn't take is not added to the key's window either: the
// in-memory sums never count a record the next read can't find, and the
// error says the write failed.
func TestAppendNotCountedWhenTheLogFails(t *testing.T) {
	auditHome(t)
	// a folder where usage.jsonl goes: opening it for append fails
	if err := os.MkdirAll(usage.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour)
	s := &sums{start: start, ver: usage.PriceVersion()}
	state.Lock()
	state.sums[slot("k")] = []*sums{s}
	state.Unlock()
	err := Append(usage.Record{Time: time.Now(), Provider: "p", Model: "m", Input: 5, Status: 200, CallerKeyID: "k"})
	if err == nil {
		t.Fatal("a write that failed was reported as written")
	}
	if s.calls != 0 {
		t.Fatalf("a call missing from the log was counted: %+v", s)
	}
}

// A model named without its provider is still priced when a configured
// provider serves it, so a bare model's first concurrent calls reserve a
// cost instead of nothing.
func TestPriceOfBareModel(t *testing.T) {
	auditHome(t)
	in, zero := 2.0, 0.0
	if err := settings.Save(settings.Settings{ModelPrices: map[string]settings.ModelPrice{
		"p/m": {Input: &in, Output: &zero, CacheRead: &zero, CacheWrite: &zero},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "p", Name: "P", Key: "k", Chat: "https://p.example/v1"}); err != nil {
		t.Fatal(err)
	}
	got, ok := priceOf("m")
	if !ok || math.Abs(got-2e-6) > 1e-12 {
		t.Fatalf("a bare model priced %v, %v, want 2e-6", got, ok)
	}
	if got, ok := priceOf("p/m"); !ok || math.Abs(got-2e-6) > 1e-12 {
		t.Fatalf("the named price is %v, %v, want 2e-6", got, ok)
	}
}
