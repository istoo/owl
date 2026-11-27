package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/plugin"
	"github.com/istoo/owl/internal/testenv"
)

// A pasted code's Finish runs on the sign-in's own lifetime: canceled while
// the plugin's callback is in flight, the call is aborted and the callback
// doesn't save afterwards. The plugin waits on a file gate, so the cancel
// lands inside the callback, not before it started or after it answered
// (#H9/M23). The plugin is local and the gate is a file: nothing here
// reaches a vendor.
func TestPastedPluginCodeCancelAbortsFinish(t *testing.T) {
	bun := testenv.Bun(t)
	home := claudeHome(t)
	t.Setenv("OWL_BUN", bun)
	t.Cleanup(plugin.Settle)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	abs, _ := filepath.Abs("testdata/signinpause/index.js")
	if _, err := plugin.Add(ctx, abs); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}

	st, err := StartPluginSignIn("signinpause", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "waiting" || !st.PasteCode {
		t.Fatalf("StartPluginSignIn = %+v", st)
	}

	waitFile := func(name string) {
		t.Helper()
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(filepath.Join(home, name)); err == nil {
				return
			}
		}
		t.Fatalf("the plugin never wrote %s", name)
	}

	// the pasted code is claimed and its Finish waits inside the plugin
	done := make(chan error, 1)
	go func() { done <- SubmitSignInCallback(st.ID, "good") }()
	waitFile("callback")

	// the user gives up while the callback runs: the sign-in's lifetime
	// reaches the call, and the host aborts the callback with it
	CancelSignIn(st.ID)
	waitFile("aborted")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a canceled paste reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Finish didn't come back after the cancel")
	}

	// the gate opens only now: the callback has ended, so nothing is kept
	if err := os.WriteFile(filepath.Join(home, "answer"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}
	if plugin.SignedIn("signinpause") {
		b, _ := os.ReadFile(plugin.AuthPath())
		t.Fatalf("a canceled paste was saved: %s", b)
	}
	if got, _ := SignInStatus(st.ID); got.State != "canceled" {
		t.Fatalf("after the cancel: %+v", got)
	}
}
