package middleware

import (
	"net/http/httptest"
	"testing"
)

// M7: an event that comes without the blank line ending it goes through
// onEvent at finish, as one the stream ended does; it isn't written around
// the hook.
func TestStreamFinishFiltersTheLastEvent(t *testing.T) {
	use(t, file(t, `export function onEvent(ev) { ev.seen = true; return ev; }`))
	r := Begin(info())
	defer r.End()
	rec := httptest.NewRecorder()
	w, done := r.Wrap(rec)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	w.Write([]byte("data: {\"a\":1}\n"))
	done()
	if got, want := rec.Body.String(), "data: {\"a\":1,\"seen\":true}\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
