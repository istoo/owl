package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/filelock"
	"github.com/istoo/owl/internal/plugin"
	"github.com/istoo/owl/internal/testenv"
)

// An account whose sign-in the vendor refuses while the move reads the
// plugin's models (a models hook throwing signIn: expired, as Zed's does
// on a refused model token) moves along marked, as the built-in would
// have marked it on its own model read, and doesn't stop the move.
func TestMoveRefusedWhileListing(t *testing.T) {
	bun := testenv.Bun(t)
	claudeHome(t)
	t.Setenv("OWL_BUN", bun)
	t.Cleanup(plugin.Settle)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	abs, _ := filepath.Abs("../plugin/testdata/fake/index.js")
	if _, err := plugin.Add(ctx, abs); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}
	fakeMover(t)
	movers["fakeco"].pkg = abs
	loginsMu.Lock()
	if err := writeLogins([]savedLogin{fakeLogin("a@fake", "r-a", true, true), fakeLogin("g@fake", "r-models-gone", false, true)}); err != nil {
		t.Fatal(err)
	}
	loginsMu.Unlock()
	if err := Move(ctx, "fakeco"); err != nil {
		t.Fatalf("a sign-in refused while listing stopped the move: %v", err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		lapsed := map[string]bool{}
		for _, l := range pluginLogins(mustPlugin(t)) {
			lapsed[l.User] = l.Lapsed != ""
		}
		if lapsed["g@fake"] && !lapsed["a@fake"] {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lapsed after the move: %v, want g@fake alone", lapsed)
		}
	}
}

// An account whose plugin says only in words that its sign-in has expired
// (Grok's, past its time with no CLI to renew it) moves along untried and
// unmarked, as a built-in Grok account never was marked; it used to stop
// the whole move. A plugin failing an account for any other reason still
// does (TestMove's r-dead).
func TestMoveExpiredInWords(t *testing.T) {
	bun := testenv.Bun(t)
	claudeHome(t)
	t.Setenv("OWL_BUN", bun)
	t.Cleanup(plugin.Settle)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	abs, _ := filepath.Abs("../plugin/testdata/fake/index.js")
	if _, err := plugin.Add(ctx, abs); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}
	fakeMover(t)
	movers["fakeco"].pkg = abs
	loginsMu.Lock()
	if err := writeLogins([]savedLogin{fakeLogin("a@fake", "r-a", true, true), fakeLogin("x@fake", "r-models-expired", false, true)}); err != nil {
		t.Fatal(err)
	}
	loginsMu.Unlock()
	if err := Move(ctx, "fakeco"); err != nil {
		t.Fatalf("an expired sign-in stopped the move: %v", err)
	}
	users := map[string]bool{}
	for _, l := range pluginLogins(mustPlugin(t)) {
		users[l.User] = true
		if l.Lapsed != "" {
			t.Fatalf("%s marked lapsed: %q", l.User, l.Lapsed)
		}
	}
	if !users["a@fake"] || !users["x@fake"] {
		t.Fatalf("moved %v", users)
	}
}

// A move's lock is the system's on the file, not a file of a PID: one a
// owl was holding when it died is let go with it, and none is left to
// misread as held for a quarter of an hour (#H12).
func TestMoveLockIsTheSystems(t *testing.T) {
	claudeHome(t)
	p := migrationsPath() + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	// another owl holds it: a second move is refused
	other := filelock.Mutex{Path: func() string { return p }}
	if err := other.LockStrictTimeout(0); err != nil {
		t.Skip("this file system has no locks to test")
	}
	old := movesWait
	movesWait = 10 * time.Millisecond
	t.Cleanup(func() { movesWait = old })
	if _, err := lockMoves(); err == nil {
		t.Fatal("a live lock was taken")
	}
	other.Unlock()
	unlock, err := lockMoves()
	if err != nil {
		t.Fatalf("a lock let go by its owl: %v", err)
	}
	unlock()
	// and the lock file left behind holds nothing by itself
	if err := os.WriteFile(p, []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err = lockMoves()
	if err != nil {
		t.Fatalf("a file left with no lock on it: %v", err)
	}
	unlock()
}
