package settingsapi

import (
	"os"
	"testing"
	"time"

	"github.com/istoo/owl/internal/filelock"
	"github.com/istoo/owl/internal/settings"
)

// Apply merges its body with what the file holds when it saves, under the
// file's lock: a field another process changed while Apply waited for the
// lock is kept, not written over from the read before (#H43).
func TestApplyMergesWithWhatTheFileHoldsUnderTheLock(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := settings.Save(settings.Settings{Theme: "light", Name: "old"}); err != nil {
		t.Fatal(err)
	}
	unlock, held := filelock.Lock(settings.Path()+".lock", time.Second)
	if !held {
		t.Fatal("the test could not take the file's lock")
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := Apply([]byte(`{"theme":"dark"}`), false)
		done <- err
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Apply saved while another owl held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// another owl's change, made while Apply waits
	if err := os.WriteFile(settings.Path(), []byte(`{"theme":"light","name":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Apply never went on after the lock was let go")
	}
	got := settings.Load()
	if got.Theme != "dark" {
		t.Fatalf("theme = %q, want dark", got.Theme)
	}
	if got.Name != "changed" {
		t.Fatalf("name = %q: Apply wrote over the other owl's change", got.Name)
	}
}
