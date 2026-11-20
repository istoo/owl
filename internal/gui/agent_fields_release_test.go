package gui

import (
	"testing"

	"github.com/istoo/owl/internal/agent"
)

// v0.2.27's Agents page draws every field of an agent as a row; a field
// added after it (Codex's compaction model and applies to, Claude Code's
// advisor, dsh's compaction model, omp's advisor review, Pi's protocol,
// Kimi Code's subagent pool, Claude Desktop's gateway) is left out of its
// state. The fields it knew are kept.
func TestAgentFieldsPastReleaseLeftOut(t *testing.T) {
	a := &agent.Agent{ID: "codex"}
	for _, k := range []string{"model", "effort", "compact_model", "scope", "advisor", "advisor_review", "protocol", "subagent_pool", "gateway"} {
		key := k
		a.Fields = append(a.Fields, agent.Field{Key: key, Options: func(map[string]string) []agent.Option { return nil }})
	}
	got := fieldsOf(a, map[string]string{}, false)
	var keys []string
	for _, f := range got {
		keys = append(keys, f.Key)
	}
	want := []string{"model", "effort"}
	if len(keys) != len(want) {
		t.Fatalf("fields %v; want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("fields %v; want %v", keys, want)
		}
	}
}
