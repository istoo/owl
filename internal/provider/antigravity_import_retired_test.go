package provider

import (
	"context"
	"strings"
	"testing"
)

// An import is a new user's: Antigravity is retired (11-15), so its export
// is refused before the file is checked with Google, with nothing refreshed
// and no account kept. Gemini CLI, which has no import of its own, is
// refused as before and asks Google nothing either.
func TestRetiredAntigravityImportsNothing(t *testing.T) {
	f := &fakeGoogle{quota: `{"buckets":[]}`, models: `{"models":{}}`}
	googleSandbox(t, f)
	cockpit := `[{"email":"ag@example.com","refresh_token":"1//ag"}]`
	_, err := ImportGoogleAccounts(context.Background(), "antigravity", []string{cockpit})
	if err == nil || !strings.Contains(err.Error(), "@magpie-community/opencode-agy-cli-auth") {
		t.Fatalf("import = %v, want it turned away pointing to agy-cli", err)
	}
	if ls := googleSavedLogins("antigravity"); len(ls) != 0 {
		t.Fatalf("an import kept %d accounts, want none", len(ls))
	}

	if _, err := ImportGoogleAccounts(context.Background(), "gemini", []string{cockpit}); err == nil || !strings.Contains(err.Error(), "accounts can't be imported for gemini") {
		t.Fatalf("gemini import = %v, want its own refusal unchanged", err)
	}

	f.mu.Lock()
	asked := len(f.heads)
	f.mu.Unlock()
	if asked != 0 {
		t.Fatalf("Google was asked %d times by imports that were refused", asked)
	}
}
