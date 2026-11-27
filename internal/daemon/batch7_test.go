package daemon

import (
	"testing"
	"time"
)

// unload waits for launchd to let the agent go, but not forever: a job
// that stays loaded is gone on to after the wait (#L14).
func TestWaitUnloadedPollsThenGivesUp(t *testing.T) {
	oldWait, oldPoll := unloadWait, unloadPoll
	t.Cleanup(func() { unloadWait, unloadPoll = oldWait, oldPoll })
	unloadWait, unloadPoll = 200*time.Millisecond, time.Millisecond

	calls := 0
	waitUnloaded(func() bool {
		calls++
		return calls < 3
	})
	if calls < 3 {
		t.Fatalf("stopped asking after %d calls, though it was still loaded", calls)
	}

	calls = 0
	start := time.Now()
	waitUnloaded(func() bool {
		calls++
		return true // never unloads
	})
	if time.Since(start) < unloadWait {
		t.Fatalf("gave up after %v, before the wait passed", time.Since(start))
	}
	if calls < 2 {
		t.Fatalf("asked only %d times", calls)
	}
}
