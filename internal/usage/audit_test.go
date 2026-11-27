package usage

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// auditObserver records the ledger rows Append hands to the observers.
type auditObserver struct {
	mu   sync.Mutex
	rows []Record
}

func (*auditObserver) Serve() (stop func()) { return func() {} }
func (*auditObserver) Bodies() Bodies       { return NoBodies }
func (*auditObserver) Begin(Request) Watch  { return nil }

func (o *auditObserver) Ledger(r Record) {
	o.mu.Lock()
	o.rows = append(o.rows, r)
	o.mu.Unlock()
}

func (o *auditObserver) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.rows)
}

// A record whose write failed never reached the disk: it is not exported
// as a call and does not count toward Codex's used models or a key's
// spend. Only a written record does.
func TestAppendFailedWriteChangesNothing(t *testing.T) {
	pageHome(t)
	ob := &auditObserver{}
	remove := Observe(ob)
	defer remove()
	// the log's path is a folder, so opening it for append fails
	if err := os.MkdirAll(Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	codexUsedMemo.Lock()
	codexUsedMemo.at = time.Now()
	codexUsedMemo.all = nil
	gen := codexUsedMemo.gen
	codexUsedMemo.Unlock()
	var logs bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldLog)
	rec := Record{Agent: "codex", Provider: "openai", Model: "m", Requested: "fresh", Status: 200, Input: 10,
		BodyIn: "the prompt", Error: "a credential sk-secret"}
	if err := Append(rec); err == nil {
		t.Fatal("a write into a folder succeeded")
	}
	if got := ob.count(); got != 0 {
		t.Fatalf("an unwritten record reached %d observers", got)
	}
	codexUsedMemo.Lock()
	changed := codexUsedMemo.gen != gen
	codexUsedMemo.Unlock()
	if changed {
		t.Fatal("an unwritten Codex call was counted")
	}
	// the failure is seen once, and the line carries no part of the call
	line := logs.String()
	if !strings.Contains(line, "usage: the call wasn't written") {
		t.Fatalf("the failed write wasn't logged: %q", line)
	}
	for _, secret := range []string{"the prompt", "sk-secret", "fresh", "openai"} {
		if strings.Contains(line, secret) {
			t.Fatalf("the log line names the call: %q", line)
		}
	}
	if strings.Count(line, "usage:") != 1 {
		t.Fatalf("the failure was logged more than once: %q", line)
	}
	// the path can be written now: the same call reaches the observers
	if err := os.Remove(Path()); err != nil {
		t.Fatal(err)
	}
	if err := Append(rec); err != nil {
		t.Fatalf("the written record: %v", err)
	}
	if got := ob.count(); got != 1 {
		t.Fatalf("the written record reached %d observers, want 1", got)
	}
}

// lineOf is payload padded with spaces to n bytes, newline last: a JSON
// line whose length is chosen, as a log's lines have.
func lineOf(t *testing.T, payload []byte, n int) []byte {
	t.Helper()
	if n <= len(payload)+1 {
		t.Fatalf("a line of %d can't hold %d bytes", n, len(payload))
	}
	b := append([]byte(nil), payload...)
	b = append(b, bytes.Repeat([]byte(" "), n-len(b)-1)...)
	return append(b, '\n')
}

// LastSeen reads the log's tail from a selected byte: a seek that lands
// exactly at a line's start keeps that record, and one in the middle of a
// line drops only the fragment.
func TestLastSeenAtTheTail(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	claude, err := json.Marshal(Record{Time: now, Agent: "claude", Provider: "p", Model: "m", Status: 200})
	if err != nil {
		t.Fatal(err)
	}
	other, err := json.Marshal(Record{Time: now.Add(-time.Hour), Agent: "other", Provider: "p", Model: "m", Status: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		file []byte
	}{
		// the seek lands at claude's line start: it is whole and kept
		{"line start", append(lineOf(t, other, 200), lineOf(t, claude, lastSeenTail)...)},
		// the seek lands in the padding before other's newline: only that
		// fragment is dropped, and claude's line after it is read
		{"mid line", append(lineOf(t, other, 200), lineOf(t, claude, lastSeenTail-11)...)},
	} {
		t.Run(c.name, func(t *testing.T) {
			pageHome(t)
			if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(Path(), c.file, 0o600); err != nil {
				t.Fatal(err)
			}
			if got := LastSeen("claude"); !got.Equal(now) {
				t.Fatalf("last claude call %v, want %v", got, now)
			}
		})
	}
}
