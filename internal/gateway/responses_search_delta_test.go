package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// H7: a tool_search_call takes no function_call_arguments.delta, as closeAs
// sends none of its done either: an orphan delta made a strict client
// refuse the stream.
func TestResponsesToolSearchCallSendsNoArgumentDelta(t *testing.T) {
	for _, c := range []struct {
		name  string
		names map[string]nsTool
		want  bool // the delta is sent
	}{
		{"tool search", map[string]nsTool{"tool_search": {Search: true}}, false},
		{"a function", map[string]nsTool{"read_file": {}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			enc := encoder(provider.Responses, newSSEWriter(rec), &Request{Model: "m", Namespaced: c.names})
			name := "read_file"
			if c.want == false {
				name = "tool_search"
			}
			enc.event(Event{Kind: KToolStart, ID: "c1", Name: name})
			enc.event(Event{Kind: KToolArgs, Text: `{"query":"x"}`})
			enc.finish()
			got := strings.Contains(rec.Body.String(), "response.function_call_arguments.delta")
			if got != c.want {
				t.Fatalf("delta sent = %v, want %v\n%s", got, c.want, rec.Body.String())
			}
		})
	}
}
