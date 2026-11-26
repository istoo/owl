package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/budget"
	"github.com/istoo/owl/internal/provider"
)

// A Gemini call keeps its model in the path, not the body: a key's limit
// of some models holds it as it holds a chat's, and another model of the
// key goes on. Counting tokens is exempt from the reservation, as
// /count_tokens is, whatever the key's limit.
func TestKeyLimitReadsGeminiModelFromPath(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	up := httptest.NewServer(cachingVendor{wire: "gemini"})
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Gemini: up.URL + "/v1beta", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Capped", "Counted")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Tokens: 50, Models: []string{"fake/m1"}})
	setLimit(t, keys[1].ID, &access.Limit{Period: "day", Tokens: 50})
	h := New().Handler()
	body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
	if w := keyCall(h, secrets[0], "/v1beta/models/fake/m1:generateContent", body); w.Code != 200 {
		t.Fatalf("first call: %d %s", w.Code, w.Body.String())
	}
	// its 52 in and 7 out spent the limit of fake/m1
	if w := keyCall(h, secrets[0], "/v1beta/models/fake/m1:generateContent", body); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a spent model went on: %d %s", w.Code, w.Body.String())
	}
	if w := keyCall(h, secrets[0], "/v1beta/models/fake/m2:generateContent", body); w.Code != 200 {
		t.Fatalf("another model was held: %d %s", w.Code, w.Body.String())
	}

	// the whole key's limit is spent too, and counting tokens costs none
	if w := keyCall(h, secrets[1], "/v1beta/models/fake/m1:generateContent", body); w.Code != 200 {
		t.Fatalf("the whole-key limit's first call: %d %s", w.Code, w.Body.String())
	}
	if w := keyCall(h, secrets[1], "/v1beta/models/fake/m1:generateContent", body); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a spent key went on: %d %s", w.Code, w.Body.String())
	}
	if w := keyCall(h, secrets[1], "/v1beta/models/fake/m1:countTokens", `{"contents":[]}`); w.Code == http.StatusTooManyRequests {
		t.Fatalf("counting tokens was held: %d %s", w.Code, w.Body.String())
	}
}
