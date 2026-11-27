package agentplug

import (
	"testing"
	"time"
)

// Reload takes the same lock the stamp is read under: a reload between
// the stamp and the load would drop the set the stamp named and the load
// would put it back (#4133).
func TestReloadWaitsForTheStamp(t *testing.T) {
	old := stamp
	started, release := make(chan struct{}), make(chan struct{})
	stamp = func() string {
		close(started)
		<-release
		return old()
	}
	t.Cleanup(func() {
		stamp = old
		Reload()
	})
	cur.Store(nil)
	nextCheck.Store(0)
	listed := make(chan struct{})
	go func() {
		List()
		close(listed)
	}()
	<-started
	reloaded := make(chan struct{})
	go func() {
		Reload()
		close(reloaded)
	}()
	select {
	case <-reloaded:
		t.Fatal("Reload ran while the stamp was being read outside its lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-listed
	<-reloaded
}
