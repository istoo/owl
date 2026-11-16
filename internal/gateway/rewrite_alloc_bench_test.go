package gateway

import (
	"strings"
	"testing"
)

func benchRewriteBodies(b *testing.B) []struct {
	name string
	body []byte
} {
	b.Helper()
	var out []struct {
		name string
		body []byte
	}
	for _, c := range []struct {
		name  string
		turns int
	}{{"200KB", 30}, {"1MB", 150}, {"4MB", 600}} {
		out = append(out, struct {
			name string
			body []byte
		}{c.name, claudeCodeRequest(c.turns)})
	}
	return out
}

// BenchmarkWithFields measures one top-level rewrite: a field set on a
// request, every other field as it was.
func BenchmarkWithFields(b *testing.B) {
	for _, c := range benchRewriteBodies(b) {
		name, body := c.name, c.body
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				out := withFields(body, map[string]any{"stream": true})
				if len(out) == 0 {
					b.Fatal("empty")
				}
			}
		})
	}
}

// BenchmarkWithFieldsTwo measures two fields set at once, as a caller that
// already joined its changes does.
func BenchmarkWithFieldsTwo(b *testing.B) {
	body := claudeCodeRequest(150)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		out := withFields(body, map[string]any{"stream": true, "service_tier": "priority"})
		if len(out) == 0 {
			b.Fatal("empty")
		}
	}
}

// benchTurnEffortBody is a request with per-turn effort on every assistant
// message, which withoutTurnEffort takes out.
func benchTurnEffortBody(turns int) []byte {
	body := string(claudeCodeRequest(turns))
	return []byte(strings.ReplaceAll(body, `"role":"assistant","content"`, `"role":"assistant","output_config":{"effort":"high"},"content"`))
}

// BenchmarkWithoutTurnEffort measures the rewrite a provider that refused
// per-turn effort gets on every request: the field taken out of each
// message, then the whole body written back.
func BenchmarkWithoutTurnEffort(b *testing.B) {
	for name, turns := range map[string]int{"200KB": 30, "1MB": 150, "4MB": 600} {
		body := benchTurnEffortBody(turns)
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				out := withoutTurnEffort(body)
				if len(out) >= len(body) {
					b.Fatalf("%d -> %d", len(body), len(out))
				}
			}
		})
	}
}
