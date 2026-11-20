package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// An unlisted preset has no row in the add sheet, but a provider already
// made from it keeps it: its editor, Duplicate and key plans read the
// preset from the same list.
func TestUnlistedPresetsOnlyOnceAdded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	unlisted := []string{"huaweicloud", "gmicloud", "kilo", "302ai", provider.GPUStackPreset}
	for _, id := range unlisted {
		if pr := provider.Preset(id); pr == nil || !pr.Unlisted {
			t.Fatalf("%s: no unlisted preset: %+v", id, pr)
		}
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	offered := func() map[string]bool {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		var s struct {
			Presets []struct {
				ID    string `json:"id"`
				Added bool   `json:"added"`
			} `json:"presets"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatalf("providers: %v %s", err, w.Body)
		}
		out := map[string]bool{}
		for _, p := range s.Presets {
			out[p.ID] = p.Added
		}
		return out
	}

	got := offered()
	for _, id := range unlisted {
		if _, ok := got[id]; ok {
			t.Errorf("%s is offered with nothing made from it", id)
		}
	}
	if _, ok := got["openrouter"]; !ok {
		t.Fatalf("a listed preset is missing: %v", got)
	}

	if err := provider.Save(provider.Provider{ID: "gmi", Name: "GMI Cloud", Preset: "gmicloud", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	got = offered()
	if added, ok := got["gmicloud"]; !ok || !added {
		t.Fatalf("gmicloud, made into a provider: offered %v, added %v", ok, added)
	}
	for _, id := range unlisted {
		if id == "gmicloud" {
			continue
		}
		if _, ok := got[id]; ok {
			t.Errorf("%s is offered with nothing made from it", id)
		}
	}
}
