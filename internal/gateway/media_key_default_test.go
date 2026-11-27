package gateway

import (
	"strings"
	"testing"

	"github.com/istoo/owl/internal/access"
)

// H3: a request that omits model and falls back to owl's default image
// model is judged as one that names it: a key held to other models (or to
// its group's accounts) is refused, and a key held to none goes on.
func TestImagesDefaultModelIsHeldToTheKey(t *testing.T) {
	s, _ := easeled(t)
	keys, secrets := newCaller(t, "Held", "Free")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"elsewhere/*"}}); err != nil {
		t.Fatal(err)
	}
	code, obj, raw := asKey(t, s.Handler(), secrets[0], "POST", "/v1/images/generations", `{"prompt":"a owl"}`)
	if code != 403 || !strings.Contains(errorOf(obj), "may not use") {
		t.Fatalf("a held key's default image model: %d %s", code, raw)
	}
	if code, _, raw = asKey(t, s.Handler(), secrets[1], "POST", "/v1/images/generations", `{"prompt":"a owl"}`); code != 200 {
		t.Fatalf("a key held to none: %d %s", code, raw)
	}
}

// H3: the same for the default video model.
func TestVideosDefaultModelIsHeldToTheKey(t *testing.T) {
	grokSignedIn(t)
	up := newGrokMedia(t)
	s := New()
	keys, secrets := newCaller(t, "Held", "Free")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"elsewhere/*"}}); err != nil {
		t.Fatal(err)
	}
	code, obj, raw := asKey(t, s.Handler(), secrets[0], "POST", "/v1/videos", `{"prompt":"a kite"}`)
	if code != 403 || !strings.Contains(errorOf(obj), "may not use") {
		t.Fatalf("a held key's default video model: %d %s", code, raw)
	}
	if len(up.paths) != 0 {
		t.Fatalf("the vendor was asked: %v", up.paths)
	}
	if code, _, raw = asKey(t, s.Handler(), secrets[1], "POST", "/v1/videos", `{"prompt":"a kite"}`); code != 200 {
		t.Fatalf("a key held to none: %d %s", code, raw)
	}
}
