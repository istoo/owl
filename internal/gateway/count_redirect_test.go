package gateway

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/provider"
)

// M3: a token count a mask route redirects to another group is counted
// on the caller as it came (countTokens' plain), with the target group's
// accounts applied — not on the request the group it named had already
// held. The old group's hold goes, and the key's own accounts, models and
// limit stay, so the target account it may use isn't lost. Synthetic
// identities and groups: no provider is asked.
func TestMaskRouteRedirectCountsOnTheTargetGroupsAccounts(t *testing.T) {
	who := access.Identity{
		KeyID: "k", KeyName: "K",
		Accounts: []string{"p/a", "p/b", "q/c"},
		Models:   []string{"p/*"},
		Limit:    &access.Limit{Period: "day", Tokens: 1000},
	}
	r := httptest.NewRequest("POST", "/v1/messages/count_tokens", nil)
	r = r.WithContext(access.WithIdentity(r.Context(), who))
	plain := r // countTokens takes this before any group's hold
	held := heldByGroup(r, provider.Group{ID: "old", Name: "Old", Accounts: []string{"p/a", "q/c"}})
	if old := access.Caller(held.Context()); old.Group != "Old" || !slices.Contains(old.Accounts, "p/a") {
		t.Fatalf("the old group didn't hold the request: %+v", old)
	}
	// What the fix doesn't do: apply the target to the held request. The
	// old group's p/a is all of p the caller still holds, so the target's
	// p/b is lost, though the key may use it.
	newGroup := provider.Group{ID: "new", Name: "New", Accounts: []string{"p/b"}}
	bug := access.Caller(heldByGroup(held, newGroup).Context())
	if slices.Contains(bug.Accounts, "p/b") {
		t.Fatalf("holding the old group still allows p/b, so this test proves nothing: %v", bug.Accounts)
	}
	// countTokens' r = plain, then heldByGroupAt(r, to): the target applies
	// to the caller, so its account is kept.
	got := access.Caller(heldByGroup(plain, newGroup).Context())
	if !slices.Contains(got.Accounts, "p/b") {
		t.Fatalf("the target group's account is lost: %v", got.Accounts)
	}
	if got.Group != "New" || slices.Contains(got.Accounts, "p/a") {
		t.Fatalf("the target group's hold isn't the one applied: %+v", got)
	}
	// and the caller's own restrictions came through untouched
	if got.KeyID != "k" || got.KeyName != "K" || !slices.Equal(got.Models, who.Models) || got.Limit != who.Limit {
		t.Fatalf("the caller's models, key or limit changed: %+v", got)
	}
}
