package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/gwmw"
)

// blockMasker finds a value the user set to block in every text frame.
type blockMasker struct{}

func (blockMasker) JSON(b []byte) ([]byte, int)     { return b, 1 }
func (blockMasker) Text(s string) (string, int)     { return s, 1 }
func (blockMasker) Restore(s string, _ bool) string { return s }
func (blockMasker) Writer(w http.ResponseWriter) (http.ResponseWriter, func()) {
	return w, func() {}
}
func (blockMasker) JSONHits(b []byte) ([]byte, []gwmw.Hit) {
	return b, []gwmw.Hit{{Kind: "email", Action: "block", Len: 12}}
}
func (blockMasker) TextHits(s string) (string, []gwmw.Hit) { return s, nil }
func (blockMasker) Value(string) (string, bool)            { return "", false }

// A realtime frame held back by a blocked value keeps its own error: 400,
// a reason of its own, not a masker that failed (503, "masking failed"),
// which a panic under gwmw.Safely used to make of it.
func TestRealtimeBlockedFrameKeepsItsStatus(t *testing.T) {
	_, err := (&realtimeMask{ctx: context.Background(), m: blockMasker{}}).out([]byte(`{"type":"conversation.item.create"}`))
	if err == nil {
		t.Fatal("a blocked frame went out")
	}
	if !errors.As(err, new(maskBlocked)) {
		t.Fatalf("not a maskBlocked: %v", err)
	}
	if got := maskStatus(err); got != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %v", got, err)
	}
	if strings.Contains(err.Error(), "masking failed") {
		t.Fatalf("a block reads as a masker failure: %v", err)
	}
	// a masker that panics is still a masker that couldn't mask
	_, err = (&realtimeMask{ctx: context.Background(), m: panicky{}}).out([]byte(`{}`))
	if err == nil || maskStatus(err) != http.StatusServiceUnavailable || !strings.Contains(err.Error(), "masking failed") {
		t.Fatalf("a broken masker: %v", err)
	}
	// and a masker's own error keeps its type through Safely
	if _, err := gwmw.Safely(func() []byte { panic(maskBlocked{gwmw.Hit{Kind: "email", Len: 3}}) }); !errors.As(err, new(maskBlocked)) {
		t.Fatalf("an error's type was lost: %v", err)
	}
}
