package gui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Every page owl draws says it has a dark theme of its own, so a
// browser that repaints light pages dark (Chrome's Auto Dark Theme,
// Safari's Adaptive Dark Mode) leaves it alone (xjp on X). The shipped
// page is v0.2.27's and predates the declaration: adding it changes how
// browsers paint that page, so the released UI is kept as it shipped and
// only the page owl generates (webTurnedAway) is held to it here.
func TestPagesDeclareBothColorSchemes(t *testing.T) {
	const meta = `<meta name="color-scheme" content="light dark">`
	rec := httptest.NewRecorder()
	webTurnedAway(rec, httptest.NewRequest("GET", "/", nil), "/", "why")
	if !strings.Contains(rec.Body.String(), meta) {
		t.Errorf("owl web's sign-in page has no %s:\n%s", meta, rec.Body.String())
	}
}
