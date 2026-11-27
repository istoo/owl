package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// H5: a relayed tools/list whose last event comes without the blank line
// that ends it is still filtered by finish: the tools the key may not use
// never reach it, as in an event the stream ended.
func TestMCPToolsFilterFinishesAFinalEvent(t *testing.T) {
	for _, tail := range []string{
		`data: {"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"secret2"}]}}`,
		"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"secret2\"}]}}\n",
	} {
		rec := httptest.NewRecorder()
		f := &mcpToolsFilter{w: rec, keep: func(name string) bool { return name == "allowed" }}
		f.Header().Set("Content-Type", "text/event-stream")
		f.WriteHeader(200)
		f.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"allowed\"},{\"name\":\"secret\"}]}}\n\n"))
		f.Write([]byte(tail))
		f.finish()
		got := rec.Body.String()
		if strings.Contains(got, "secret") {
			t.Fatalf("a forbidden tool went out in the final event: %q", got)
		}
		if !strings.Contains(got, `"name":"allowed"`) {
			t.Fatalf("the allowed tool was lost: %q", got)
		}
	}
}

// A stream whose last event is whole but not yet ended keeps its bytes
// when nothing is cut: finish must not lose it.
func TestMCPToolsFilterKeepsAnUnchangedFinalEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	f := &mcpToolsFilter{w: rec, keep: func(string) bool { return true }}
	f.Header().Set("Content-Type", "text/event-stream")
	f.WriteHeader(200)
	tail := `data: {"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"a"}]}}`
	f.Write([]byte(tail))
	f.finish()
	if got := rec.Body.String(); got != tail {
		t.Fatalf("final event %q, want it as it came", got)
	}
}
