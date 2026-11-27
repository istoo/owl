package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// H6: a whole (non-stream) Responses reply held by the namespace filter
// (spaces) goes on through the text-call filter (written) once it is let
// go, in the order the reads take: a call the model wrote into its text is
// made a call, as it is in a stream, and enumTidy after both.
func TestPassthroughWholeReplyGoesThroughSpaceThenWritten(t *testing.T) {
	fresh(t)
	// the request offers a namespace, so spaces holds the whole reply until
	// its flush, as it does for every reply of such a request: no call of
	// its own is needed for that
	reply := `{"id":"resp_1","object":"response","status":"completed","output":[` +
		`{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Let me read.\n<invoke name=\"read_file\">\n<parameter name=\"path\">a.py</parameter>\n</invoke>","annotations":[]}]}]}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"m"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"up/m","stream":false,"input":[{"type":"message","role":"user","content":"read a.py"}],"tools":[` +
		`{"type":"function","name":"read_file","description":"Reads a file.","parameters":{"type":"object","properties":{"path":{"type":"string"}}}},` +
		`{"type":"namespace","name":"collaboration","description":"Agents.","tools":[{"type":"function","name":"spawn_agent","description":"Spawns one.","parameters":{"type":"object","properties":{}}}]}]}`
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Output []map[string]any `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the reply isn't JSON: %v\n%s", err, rec.Body.String())
	}
	read := false
	for _, it := range out.Output {
		if name, _ := it["name"].(string); name == "read_file" {
			read = true
			if args, ok := it["arguments"].(string); !ok || !strings.Contains(args, `"path"`) {
				t.Fatalf("the text call's arguments: %v", it)
			}
		}
	}
	if !read {
		t.Fatalf("the call the model wrote into its text wasn't made one: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<invoke") {
		t.Fatalf("the text call stayed text: %s", rec.Body.String())
	}
}
