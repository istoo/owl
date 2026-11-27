package gateway

import (
	"testing"
	"time"
)

// M10: a session made by Claude Code's interactive mode is shelved as
// one, so the turn that takes it back doesn't resume it as a -p run.
func TestShelveKeepsTheTUI(t *testing.T) {
	b := idleTestBridge()
	run := &subscriptionRun{bridge: b, sessionID: "sid", sessions: []string{"proj"}, idleKey: "key", idleAt: time.Now(), tui: true}
	b.mu.Lock()
	b.shelve(run)
	s := b.shelf["key"]
	b.mu.Unlock()
	if s == nil {
		t.Fatal("the run wasn't shelved")
	}
	if !s.tui {
		t.Fatal("a TUI session was shelved as a -p one")
	}
	if s.timer != nil {
		s.timer.Stop()
	}
}
