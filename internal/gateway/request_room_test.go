package gateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/access"
)

// L1: a body whose length nobody declared takes bodyRoom's room as it is
// read, not once it is all held. The first ask is small, the last covers
// the body.
func TestReadBodyTakesRoomWhileItArrives(t *testing.T) {
	fresh(t)
	body := strings.Repeat("x", (1<<20)+512)
	r := httptest.NewRequest("POST", "/v1/chat/completions", io.MultiReader(strings.NewReader(body)))
	r.ContentLength = -1 // nobody declared a length
	var calls []int64
	w := httptest.NewRecorder()
	got, status, err := readBoundedRequestBodyRoom(w, r, requestLimits{body: 4 << 20}, func(n int64) error {
		calls = append(calls, n)
		return nil
	})
	if err != nil || status != 0 {
		t.Fatalf("a body under the limit: %d %v", status, err)
	}
	if len(got) != len(body) {
		t.Fatalf("read %d bytes of %d", len(got), len(body))
	}
	if len(calls) < 2 {
		t.Fatalf("room was asked %d times (%v), want it as the body arrived", len(calls), calls)
	}
	if calls[0] >= int64(len(body)) {
		t.Fatalf("the first ask already covered the whole body: %v", calls)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] < calls[i-1] {
			t.Fatalf("room asks went backwards: %v", calls)
		}
	}
	if last := calls[len(calls)-1]; last < int64(len(body)) {
		t.Fatalf("the last ask %d is under the body's %d", last, len(body))
	}
}

// A body that finds no room as it grows is owl's 503, not the client's
// 413: the request may be as small as it said.
func TestReadBodyRoomFullIsTheGateway(t *testing.T) {
	fresh(t)
	full := &bodyFull{n: 2 << 20, held: 1 << 20, room: 1 << 20, why: "the test's"}
	body := strings.Repeat("x", 2<<20)
	r := httptest.NewRequest("POST", "/v1/chat/completions", io.MultiReader(strings.NewReader(body)))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	_, status, err := readBoundedRequestBodyRoom(w, r, requestLimits{body: 4 << 20}, func(int64) error { return full })
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status %d %v, want 503", status, err)
	}
	var got *bodyFull
	if !errors.As(err, &got) || got != full {
		t.Fatalf("err %v, want the room's refusal", err)
	}
}

// A legal body of some size is read as ever with a room: the mechanism
// doesn't cut what the route allows.
func TestReadBodyRoomKeepsALegalLargeBody(t *testing.T) {
	fresh(t)
	body := strings.Repeat("y", 2<<20)
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	got, status, err := readBoundedRequestBodyRoom(w, r, requestLimits{body: 8 << 20}, func(int64) error { return nil })
	if err != nil || status != 0 || string(got) != body {
		t.Fatalf("%d %v, read %d of %d", status, err, len(got), len(body))
	}
}

// keyLimited reads a body it must see (a cost limit, a spend of some
// models, an unknown or compressed length) at the limit of the route
// that will serve it, not the gateway's 512 MiB: a small endpoint's body
// is refused at its own size, before the body is all in memory (L1).
func TestKeyLimitedReadsASmallRouteAtItsOwnLimit(t *testing.T) {
	fresh(t)
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	// 5 MiB, unanswered (ContentLength unknown), to the 4 MiB admin API
	body := strings.Repeat("z", 5<<20)
	r := httptest.NewRequest("POST", AdminPath+"settings", strings.NewReader(body))
	r.ContentLength = -1
	r = r.WithContext(access.WithIdentity(r.Context(), access.Identity{
		KeyID: "k", KeyName: "K", Limit: &access.Limit{Period: "day", Cost: 1},
	}))
	w := httptest.NewRecorder()
	keyLimited(next).ServeHTTP(w, r)
	if reached {
		t.Fatal("the small route's handler was reached with a body over its limit")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "4 MB") {
		t.Fatalf("the 413 doesn't name the route's limit: %s", w.Body.String())
	}
}

// keyBodyLimit is each route's own limit, so keyLimited reads no more
// than the route will (L1): the management API, System One, usage
// ingest, the chat page's uploads, Codex's path, and the gateway's
// default for everything else.
func TestKeyBodyLimitFollowsTheRoute(t *testing.T) {
	for _, c := range []struct {
		path string
		want int64
	}{
		{AdminPath + "settings", adminBodyMost},
		{"/v1/systemone", maxSystemOneBody},
		{"/v1/owl/usage/requests", ingestMaxBody},
		{ChatPagePath + "/anthropic/v1/messages", 256 << 20},
		{ChatPagePath + "/anthropic/v1/files", chatFileMax + (1 << 20)},
		{ChatPagePath + "/v1/tools/asr", 30 << 20},
		{CodexPath + "/responses", codexBodyLimit()},
		{"/v1/chat/completions", defaultBodyLimit},
		{"/v1/owl/quotas", defaultBodyLimit},
	} {
		if got := keyBodyLimit(c.path); got != c.want {
			t.Errorf("keyBodyLimit(%q) = %d, want %d", c.path, got, c.want)
		}
	}
}
