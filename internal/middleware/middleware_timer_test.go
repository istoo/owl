package middleware

import (
	"testing"
	"time"

	"github.com/MoeForge/moejs"
)

// A timer from a call that has ended must not stop the call that took the
// runtime's place: arm gives each call a generation, and the old timer's
// expire finds its generation gone.
func TestStaleTimerDoesNotStopTheNextCall(t *testing.T) {
	in := &inst{}
	in.arm(time.Hour)
	in.mu.Lock()
	old := in.gen
	in.mu.Unlock()
	if in.disarm() {
		t.Fatal("the call ended held the timeout it never met")
	}
	in.arm(time.Hour)
	in.expire(old) // the first call's timer, running late
	if in.disarm() {
		t.Fatal("a timer of the call before stopped the next call")
	}
}

// The timer of the call running still stops it, and disarm clears it.
func TestTimerOfTheCallRunningStopsIt(t *testing.T) {
	in := &inst{rt: moejs.NewRuntime(moejs.Options{})}
	in.arm(time.Hour)
	in.mu.Lock()
	gen := in.gen
	in.mu.Unlock()
	in.expire(gen)
	if !in.disarm() {
		t.Fatal("the running call's timer didn't stop it")
	}
}
