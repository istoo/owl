package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/istoo/owl/internal/edit"
	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/provider"
)

// ocrOwnConfig is a config.json as `ocr config provider` writes one: a
// preset of the user's on a model of its own, and a setting beside it.
const ocrOwnConfig = `{
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "language": "zh",
  "providers": {
    "anthropic": {"api_key": "sk-ant-own", "model": "claude-sonnet-4-6"}
  }
}
`

// Picking a model of owl's for Open Code Review writes the custom
// provider "owl" (openai protocol, the gateway's /v1, a key naming
// it, the model) and points provider at it; going back puts the user's
// own provider and model back and takes owl's entry out.
func TestOCRWiring(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	a, err := Find(ocrID)
	if err != nil {
		t.Fatal(err)
	}
	path := a.Path
	if filepath.Base(filepath.Dir(path)) != ".opencodereview" || filepath.Base(path) != "config.json" {
		t.Fatalf("path %s", path)
	}
	hanakoWrite(t, path, ocrOwnConfig, 0o600)
	if !a.Detected() {
		t.Fatal("~/.opencodereview doesn't count as Open Code Review there")
	}
	if v := a.Field("model").Get(); v != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("own model reads %q", v)
	}
	if a.Wired() {
		t.Fatal("the user's own anthropic reads as owl's")
	}
	opts := a.Field("model").Options(a.Values())
	if !slices.ContainsFunc(opts, func(o Option) bool { return o.Value == "anthropic/claude-sonnet-4-6" }) ||
		!slices.ContainsFunc(opts, func(o Option) bool { return o.Value == "owl/fake/m1" }) {
		t.Fatalf("options: %+v", opts)
	}

	if err := a.Apply("model", "owl/fake/m1"); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"provider":                      "owl",
		"custom_providers.owl.protocol": "openai",
		"custom_providers.owl.url":      gatewayV1(),
		"custom_providers.owl.api_key":  gateway.TokenFor(ocrID),
		"custom_providers.owl.model":    "fake/m1",
		"language":                      "zh",
		"providers.anthropic.api_key":   "sk-ant-own",
	} {
		if v, _ := edit.GetJSON(path, k); v != want {
			b, _ := os.ReadFile(path)
			t.Fatalf("%s = %q, want %q in:\n%s", k, v, want, b)
		}
	}
	if v := a.Field("model").Get(); v != "owl/fake/m1" {
		t.Fatalf("model reads %q", v)
	}
	if !a.Wired() {
		t.Fatalf("not connected after a pick: %v", a.Values())
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("drift right after a set: %+v", d)
	}
	// another of owl's models: the stash keeps the user's, not owl's
	if err := a.Apply("model", "owl/fake/m2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(path, "custom_providers.owl.model"); v != "fake/m2" {
		t.Fatalf("model %q", v)
	}

	// the url changed by hand: owl says so
	if err := edit.SetJSON(path, edit.KV{Path: "custom_providers.owl.url", Value: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if d := a.Drift(); d == nil {
		t.Fatal("a url that no longer reaches the gateway reads as fine")
	}
	if err := a.Apply("model", "owl/fake/m2"); err != nil {
		t.Fatal(err)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"provider": "anthropic",
		"model":    "claude-sonnet-4-6",
		"language": "zh",
	} {
		if v, _ := edit.GetJSON(path, k); v != want {
			b, _ := os.ReadFile(path)
			t.Fatalf("after Disconnect %s = %q, want %q in:\n%s", k, v, want, b)
		}
	}
	if _, ok := edit.GetJSONRaw(path, "custom_providers"); ok {
		b, _ := os.ReadFile(path)
		t.Fatalf("owl's entry, or the custom_providers made for it, left behind:\n%s", b)
	}
	if a.Wired() {
		t.Fatal("still connected after Disconnect")
	}
}

// A model of the user's own picked while on owl goes the way `ocr
// config model` writes it, and owl's entry goes; a provider OCR hasn't
// set up is turned away rather than written as one it can't reach.
func TestOCROwnPick(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	a, err := Find(ocrID)
	if err != nil {
		t.Fatal(err)
	}
	hanakoWrite(t, a.Path, ocrOwnConfig, 0o600)
	if err := a.Apply("model", "owl/fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "anthropic/claude-opus-4-6"); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"provider":                  "anthropic",
		"model":                     "claude-opus-4-6",
		"providers.anthropic.model": "claude-opus-4-6",
	} {
		if v, _ := edit.GetJSON(a.Path, k); v != want {
			t.Fatalf("%s = %q, want %q", k, v, want)
		}
	}
	if _, ok := edit.GetJSONRaw(a.Path, "custom_providers.owl"); ok {
		t.Fatal("owl's entry left behind")
	}
	if err := a.Apply("model", "nowhere/x"); err == nil {
		t.Fatal("a provider OCR hasn't set up was written")
	}
}

// `ocr` names other programs too: only one under a folder named for Open
// Code Review is its.
func TestOCRNotOurs(t *testing.T) {
	for bin, ours := range map[string]bool{
		"/usr/local/lib/node_modules/@alibaba-group/open-code-review/bin/ocr": true,
		"/home/u/.opencodereview/bin/ocr":                                     true,
		"/usr/bin/ocr":                                                        false,
		"/opt/homebrew/bin/ocr":                                               false,
	} {
		if got := !ocrNotOurs(bin); got != ours {
			t.Errorf("%s: ours %v, want %v", bin, got, ours)
		}
	}
}
