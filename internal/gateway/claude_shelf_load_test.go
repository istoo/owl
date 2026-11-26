package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two turns finding nothing on the shelf at once both go on from the file:
// one used to return when it saw the other had looked, before that one had
// filled b.shelf, and told the whole conversation again (#18).
func TestClaudeShelfLoadsForEveryTurnAsking(t *testing.T) {
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const sid = "s1"
	if err := os.WriteFile(filepath.Join(projects, sid+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := shelfEntry{Session: sid, Sessions: []string{projects}, Key: "k1", At: time.Now()}
	data, err := json.Marshal(shelfFile{Version: shelfFileVersion, Saved: time.Now(), Entries: []shelfEntry{e}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(shelfPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	shelfRead.Delete(shelfPath())
	if err := os.WriteFile(shelfPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	b := newSubscriptionBridge()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.loadShelf()
			b.mu.Lock()
			n := len(b.shelf)
			b.mu.Unlock()
			if n == 0 {
				t.Error("a turn found the shelf empty: it told the whole conversation again")
			}
		}()
	}
	wg.Wait()
}

// A tool call reaching a run that has ended is answered 410 and leaves
// nothing behind: its pending waiter and its b.calls entry would never be
// taken, and the call would block until the client gave up (#11).
func TestMCPCallToAnEndedRunLeavesNothing(t *testing.T) {
	b := newSubscriptionBridge()
	run := &subscriptionRun{bridge: b, token: "tok", pending: map[string]chan mcpToolResult{}, early: map[string]mcpToolResult{}}
	b.mu.Lock()
	b.runs["tok"] = run
	b.mu.Unlock()
	// finish has closed the run and taken its pending waiters; removeRun
	// has not yet taken it out of b.runs, the window mcpCall ran into
	run.mu.Lock()
	run.closed = true
	run.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/_owl/claude-mcp/tok", strings.NewReader(`{"tool_call_id":"c1","name":"read","arguments":{}}`))
	req.SetPathValue("token", "tok")
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		b.mcpCall(rr, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mcpCall blocked on a run that had ended")
	}
	if rr.Code != http.StatusGone {
		t.Errorf("status %d, want 410", rr.Code)
	}
	run.mu.Lock()
	pending := len(run.pending)
	run.mu.Unlock()
	if pending != 0 {
		t.Errorf("run.pending kept %d waiter(s) after the call was refused", pending)
	}
	b.mu.Lock()
	calls := len(b.calls)
	b.mu.Unlock()
	if calls != 0 {
		t.Errorf("b.calls kept %d entry(ies) after the call was refused", calls)
	}
}
