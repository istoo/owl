package library

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A session is keyed by the server it belongs to and its id: two servers
// given the same id keep separate sessions, so neither overwrites the
// other (#39).
func TestRunSessionsKeyedByServer(t *testing.T) {
	resetRuns(t)
	spec := runSpec{Command: "x"}
	a := session("same", "A", spec, nil)
	b := session("same", "B", spec, nil)
	if a == b {
		t.Fatal("two servers' sessions are one")
	}
	if again := session("same", "A", spec, nil); again != a {
		t.Fatal("the same server's session isn't the same")
	}
}

// A DELETE aimed at one server cannot end another server's session, even
// when the id is the same.
func TestRunDeleteCannotKillAnotherServersSession(t *testing.T) {
	resetRuns(t)
	s := session("sid-b", "B", runSpec{Command: "x"}, nil)
	req := httptest.NewRequest(http.MethodDelete, RunPath+"A", nil)
	req.Header.Set("Mcp-Session-Id", "sid-b")
	rec := httptest.NewRecorder()
	ServeRun(rec, req, "A")
	runs.Lock()
	kept := runs.sessions[runKey{name: "B", sid: "sid-b"}]
	runs.Unlock()
	if kept != s {
		t.Fatal("a DELETE to A ended B's session")
	}
}

// deliver reads the spec under the same lock runGiven writes it under: a
// server's notifications while a sync changes its spec is no data race.
func TestRunDeliverReadsSpecUnderLock(t *testing.T) {
	resetRuns(t)
	s := session("sid", "fake", runSpec{Command: "a"}, nil)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.deliver(nil, nil, nil, "notifications/tools/list_changed")
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		s.mu.Lock()
		s.spec = runSpec{Command: fmt.Sprint(i)}
		s.mu.Unlock()
	}
	close(stop)
	wg.Wait()
}
