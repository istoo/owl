package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// A DELETE marks its session gone, so a start under way ends the process
// it made rather than keeping it (#H29).
func TestServeRunDeleteMarksTheSessionDeleted(t *testing.T) {
	resetRuns(t)
	s := session("sid-del", "fake", runSpec{Command: "x"}, nil)
	req := httptest.NewRequest(http.MethodDelete, RunPath+"fake", nil)
	req.Header.Set("Mcp-Session-Id", "sid-del")
	rec := httptest.NewRecorder()
	ServeRun(rec, req, "fake")
	s.mu.Lock()
	deleted := s.deleted
	s.mu.Unlock()
	if !deleted {
		t.Fatal("a DELETE left the session running")
	}
}

// A server that finishes starting after its session was ended is not kept:
// ensure sees the session gone, ends the process it started, and answers
// with an error (#H29).
func TestEnsureEndsAServerStartedForADeletedSession(t *testing.T) {
	_, logf, spec := runFixture(t)
	spec.Env["OWL_FAKE_RUN_DELAY"] = "2s"
	s := session("sid-late", "fake", spec, json.RawMessage(`{}`))
	done := make(chan error, 1)
	go func() {
		_, _, err := s.ensure()
		done <- err
	}()
	// the server is started (its initialize is being answered slowly)
	for i := 0; i < 400; i++ {
		if b, err := os.ReadFile(logf); err == nil && len(b) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	s.deleted = true
	s.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ensure kept a server for a deleted session")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("ensure never returned")
	}
	if c := s.running(); c != nil {
		t.Fatalf("a deleted session kept its server: %+v", c)
	}
}
