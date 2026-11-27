package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/provider"
)

// M2: a routing group set for Codex's subagents is judged as a request to
// it is (groupGiven). A key held to some models that names the group may
// use it; before, the group was resolved as a model, found none, and the
// subagent was kept on the lead's model.
func TestCodexSubagentGroupTargetAllowed(t *testing.T) {
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m2"},
		Responses: "http://127.0.0.1:9/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "workers", Name: "Workers", Members: []string{"fake/m2"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetCodexSubagentModel("group/workers"); err != nil {
		t.Fatal(err)
	}
	keys, _ := newCaller(t, "Held")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"group/workers"}}); err != nil {
		t.Fatal(err)
	}
	who := access.Identity{KeyID: keys[0].ID, KeyName: "Held", Models: []string{"group/workers"}}
	r := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{}`))
	r = r.WithContext(access.WithIdentity(r.Context(), who))
	r.Header.Set("User-Agent", "codex_cli_rs/0.159.0 (Mac OS 26.2.0; arm64)")
	pick := New().codexSubagentPick(r, "codex", "collab_spawn", "fake/m1", []byte(`{}`), true)
	if pick == nil || pick.Kept != "" || pick.To != "group/workers" {
		t.Fatalf("the key may use the group, pick %+v", pick)
	}
	// a group bound to another key is kept (groupBarred), as before
	if err := provider.SaveGroup(provider.Group{ID: "workers", Name: "Workers", Members: []string{"fake/m2"}, Routing: provider.Ordered, Keys: []string{"another-key"}}); err != nil {
		t.Fatal(err)
	}
	pick = New().codexSubagentPick(r, "codex", "collab_spawn", "fake/m1", []byte(`{}`), true)
	if pick == nil || pick.Kept != "the gateway key may not use it" {
		t.Fatalf("a group bound to another key, pick %+v", pick)
	}
}
