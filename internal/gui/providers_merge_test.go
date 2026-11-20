package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/provider"
)

// v0.2.27's provider editor fills a model's merge input from m.merge ||
// m.id, and its backend sent merge for every model, its own id included.
// /api/providers keeps that: a model with a merge rule carries it, and a
// plain id carries itself, both the values that backend gave.
func TestProvidersMergeAsReleased(t *testing.T) {
	sandboxHome(t)
	models := []string{"claude-opus-4-5-20251101", "alpha-1"}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	if w.Code != 200 {
		t.Fatalf("GET /api/providers: %d %s", w.Code, w.Body)
	}
	var got struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID    string `json:"id"`
				Merge string `json:"merge"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	merge := map[string]string{}
	for _, p := range got.Providers {
		if p.ID == "relay" {
			for _, m := range p.Models {
				merge[m.ID] = m.Merge
			}
		}
	}
	for _, id := range models {
		want := catalog.SameModel(id) // v0.2.27's provider.MergeName(id)
		if then, ok := merge[id]; !ok || then != want {
			t.Errorf("%s: merge %q (present %v), want v0.2.27's %q", id, then, ok, want)
		}
	}
	if merge["claude-opus-4-5-20251101"] != "claude-opus-4-5" {
		t.Errorf("dated id merged by %q, want claude-opus-4-5", merge["claude-opus-4-5-20251101"])
	}
}
