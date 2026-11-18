package provider

import (
	"slices"
	"testing"

	"github.com/istoo/owl/internal/settings"
)

// A provider deleted is gone from every list that names it: a visibility
// holding its id, which would keep a provider added under that id out of
// an agent's lists (azure-wus3, litellm on the user's machine were; ark
// and neolix kept `owl visible` from saving at all), and the models picked
// or taken out of it.
func TestDeleteProviderForgetsItsVisibility(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, id := range []string{"relay", "late"} {
		if err := Save(Provider{ID: id, Name: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x", "y"}}); err != nil {
			t.Fatal(err)
		}
	}
	s := settings.Load()
	s.Visible = map[string][]string{"codex": {"relay", "ghost"}, "pi": {"relay"}}
	s.HiddenModels = map[string][]string{"codex": {"relay/x", "late/y", "group/gg"}}
	s.PickedModels = map[string][]string{"pi": {"relay/x", "late/y"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := Delete("relay"); err != nil {
		t.Fatal(err)
	}
	s = settings.Load()
	if v := s.Visible["codex"]; !slices.Equal(v, []string{"ghost"}) {
		t.Fatalf("codex's visibility: %v", v)
	}
	if v, ok := s.Visible["pi"]; ok {
		t.Fatalf("pi is still narrowed to a deleted provider: %v", v)
	}
	if h := s.HiddenModels["codex"]; !slices.Equal(h, []string{"late/y", "group/gg"}) {
		t.Fatalf("codex's taken out: %v", h)
	}
	if p := s.PickedModels["pi"]; !slices.Equal(p, []string{"late/y"}) {
		t.Fatalf("pi's picks: %v", p)
	}
}

// A group deleted is gone from every list that names it, its name and its
// group/<id> ref alike, and its entries from the models picked or taken
// out. A found group's deletion is a hiding, so what names it stays.
func TestDeleteGroupForgetsItsVisibility(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := Save(Provider{ID: "relay", Name: "relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveGroup(Group{ID: "gg", Name: "GG", Members: []string{"relay/x"}}); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	s.Visible = map[string][]string{"codex": {"gg", "group/gg", "relay"}}
	s.HiddenModels = map[string][]string{"codex": {"group/gg", "relay/y"}}
	s.PickedModels = map[string][]string{"pi": {"group/gg"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := DeleteGroup("gg"); err != nil {
		t.Fatal(err)
	}
	s = settings.Load()
	if v := s.Visible["codex"]; !slices.Equal(v, []string{"relay"}) {
		t.Fatalf("visibility: %v", v)
	}
	if h := s.HiddenModels["codex"]; !slices.Equal(h, []string{"relay/y"}) {
		t.Fatalf("taken out: %v", h)
	}
	if p, ok := s.PickedModels["pi"]; !ok || len(p) != 0 {
		t.Fatalf("picks: %v (kept is the only-picked switch)", p)
	}
}

// A visibility's names that name nothing any more are passed by when it is
// read: they narrow nothing and raise no error, so an older settings file
// still serves the providers it does name (the command that replaces the
// list takes them out: owl visible).
func TestVisibleStaleNamesIgnored(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := Save(Provider{ID: "relay", Name: "relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	s.Visible = map[string][]string{"codex": {"relay", "ghost"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	shown, kept := CatalogFor("codex")
	if !slices.ContainsFunc(shown, func(e Entry) bool { return e.ID == "relay/m1" }) || len(kept) != 0 {
		t.Fatalf("shown %v, kept %v", shown, kept)
	}
	// the only name left names nothing: every model is kept, and nothing
	// fails
	s = settings.Load()
	s.Visible = map[string][]string{"codex": {"ghost"}}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	shown, kept = CatalogFor("codex")
	if len(shown) != 0 || !slices.ContainsFunc(kept, func(e Entry) bool { return e.ID == "relay/m1" }) {
		t.Fatalf("shown %v, kept %v", shown, kept)
	}
}
