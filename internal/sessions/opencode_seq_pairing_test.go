package sessions

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// OpenCode 2 keeps a message's order in seq, not in time_created: two
// prompts and their replies made in one millisecond still pair each reply
// with its own prompt, not by id or time, which bound a reply to the
// wrong turn (#64).
func TestOpenCodeV2PairsUserBySeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	t.Setenv("OPENCODE_DB", path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, parent_id TEXT, directory TEXT, title TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT)`,
		`INSERT INTO kv VALUES ('migration.v1-v2', '{"phase":"completed"}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Add(-5 * time.Second).UnixMilli()
	if _, err := db.Exec(`INSERT INTO session_v2 VALUES ('s', NULL, 'private', 'private', ?, ?)`, now, now+5000); err != nil {
		t.Fatal(err)
	}
	// every message shares time_created, so only seq tells them apart
	insert := func(id, kind string, seq int, doc map[string]any) {
		t.Helper()
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO session_message VALUES (?, 's', ?, ?, ?, ?, ?)`, id, kind, seq, now, now+int64(seq+1), string(b)); err != nil {
			t.Fatal(err)
		}
	}
	// all four share time_created. A user id sorts before its reply's, so a
	// trace builder sorting by (time, id) still puts each prompt first,
	// and only seq can bind a reply to the right prompt: by time and id
	// both replies would take the last prompt.
	insert("p1", "user", 0, map[string]any{"role": "user", "text": "Q1", "time": map[string]int64{"created": now}})
	insert("q1", "assistant", 1, map[string]any{"role": "assistant", "model": map[string]string{"id": "m", "providerID": "p"},
		"time":    map[string]int64{"created": now, "completed": now + 1000},
		"tokens":  map[string]any{"input": 10, "output": 1, "reasoning": 0, "cache": map[string]int{"read": 0, "write": 0}},
		"content": []any{map[string]string{"type": "text", "text": "A1"}}})
	insert("p2", "user", 2, map[string]any{"role": "user", "text": "Q2", "time": map[string]int64{"created": now}})
	insert("q2", "assistant", 3, map[string]any{"role": "assistant", "model": map[string]string{"id": "m", "providerID": "p"},
		"time":    map[string]int64{"created": now, "completed": now + 1000},
		"tokens":  map[string]any{"input": 10, "output": 1, "reasoning": 0, "cache": map[string]int{"read": 0, "write": 0}},
		"content": []any{map[string]string{"type": "text", "text": "A2"}}})

	r := NewTraceReader(ms(now).Add(-time.Second))
	budget := int64(8 << 20)
	roots, models, _ := spanKinds(r.pollOpenCode(true, &budget))
	if len(roots) != 2 || len(models) != 2 {
		t.Fatalf("roots %d, models %d: %+v", len(roots), len(models), roots)
	}
	rootByID := map[string]TraceSpan{}
	for _, root := range roots {
		rootByID[root.ID] = root
	}
	got := map[string]string{}
	for _, m := range models {
		root, ok := rootByID[m.Parent]
		if !ok {
			t.Fatalf("a reply's parent %q is no root: %+v", m.Parent, models)
		}
		got[root.Input] = m.Output
	}
	for q, a := range map[string]string{"Q1": "A1", "Q2": "A2"} {
		if !strings.Contains(got[q], a) {
			t.Errorf("%s paired with %q, want %s (all: %v)", q, got[q], a, got)
		}
	}
}
