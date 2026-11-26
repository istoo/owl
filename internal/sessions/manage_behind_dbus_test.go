package sessions

import (
	"sync"
	"testing"
	"time"
)

// A read behind the Sessions answer opens the databases as every other
// refresh entry does, and counts as a user of them until it is done: the
// last user closes them, so one leaving mid-read took the others' away
// (#57).
func TestReadBehindCountsAsADatabaseUser(t *testing.T) {
	setup(t)
	old := quickBytes
	quickBytes = 0 // every file is read behind
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	parseHook = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	opened := false
	open := func() {
		if !opened {
			opened = true
			close(release)
		}
	}
	t.Cleanup(func() { open(); behindRuns.Wait(); parseHook, quickBytes = nil, old })

	claude := func([]AgentCount, map[string]map[string]bool) string { return "claude" }
	got := make(chan ManageView, 1)
	go func() { got <- Manage(map[string]bool{"claude": true, "codex": true}, claude) }()
	select {
	case <-got:
	case <-time.After(30 * time.Second):
		open()
		<-got
		t.Fatal("Manage waited for the whole read")
	}
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		open()
		behindRuns.Wait()
		t.Fatal("the read behind never parsed a file")
	}
	// the background read is under way and blocks in parseHook with mu let
	// go, so this reads the count it holds
	mu.Lock()
	n := dbUsers
	mu.Unlock()
	if n == 0 {
		open()
		behindRuns.Wait()
		t.Fatal("the read behind holds no database user")
	}
	open()
	behindRuns.Wait()
	mu.Lock()
	n = dbUsers
	mu.Unlock()
	if n != 0 {
		t.Fatalf("%d database users left after the read", n)
	}
}
