package usage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/provider"
)

// The block pricer prices each call by every name Pricer does: a relay's
// two keys at their own rates (KeyPriceRates) and a service tier's price
// (TierSent/TierServed) are read from the row, not left out of its shapes'
// cache key. Two calls that differ only by key or tier, in one block, cost
// what the ledger's Pricer says, each its own (#55).
func TestBlockPricerPricesKeyRateAndTier(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{`+
		`"gpt-6.1-sol":{"id":"gpt-6.1-sol","cost":{"input":2,"output":10,"cache_read":0.1,"cache_write":2.5},`+
		`"experimental":{"modes":{"fast":{"cost":{"input":4,"output":20,"cache_read":0.2,"cache_write":5},"provider":{"body":{"service_tier":"priority"}}}}}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	half, premium := provider.KeyID("sk-half"), provider.KeyID("sk-premium")
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "sk-half", Chat: "https://relay.example/v1",
		Keys:          []provider.KeyAccount{{Name: "Premium", Key: "sk-premium"}, {Name: "Plain", Key: "sk-plain"}},
		KeyPriceRates: map[string]float64{half: 0.5, premium: 1.5}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	recs := []Record{
		{Time: now, Provider: "relay", Model: "gpt-6.1-sol", ProviderKeyID: half, Input: 1000, Output: 100, Status: 200},
		{Time: now, Provider: "relay", Model: "gpt-6.1-sol", ProviderKeyID: premium, Input: 1000, Output: 100, Status: 200},
		{Time: now, Provider: "codex", Model: "gpt-6.1-sol", TierSent: "priority", Input: 1000, Output: 100, Status: 200},
		{Time: now, Provider: "codex", Model: "gpt-6.1-sol", Input: 1000, Output: 100, Status: 200},
	}
	var raw rowChunk
	for i, r := range recs {
		raw.add(Row{Record: r}, "", int64(i), false)
	}
	c, _ := newBlockPricer(provider.Renames{}, nil, Pricer()).priceBlock(raw.pack())
	cost := NewCoster()
	for i, r := range recs {
		want, ok := cost(r)
		if !ok {
			t.Fatalf("call %d: the ledger prices it at nothing", i)
		}
		got := c.row(i)
		if !got.Priced || math.Abs(got.Cost-want) > 1e-9 {
			t.Errorf("call %d (%q key, tier %q): block cost %v (priced %v), ledger %v", i, r.ProviderKeyID, r.TierSent, got.Cost, got.Priced, want)
		}
	}
	// and they are apart: half < standard < premium < fast
	if !(c.row(0).Cost < c.row(3).Cost && c.row(3).Cost < c.row(1).Cost && c.row(1).Cost < c.row(2).Cost) {
		t.Errorf("costs %v, %v, %v, %v, want half < standard < premium < fast",
			c.row(0).Cost, c.row(1).Cost, c.row(2).Cost, c.row(3).Cost)
	}
}
