package redact

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// dataPayload is the text SSE clients join the data lines of an event
// into: each line's "data:" taken off, joined by a newline.
func dataPayload(t *testing.T, event string) string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(event, "\n") {
		l = strings.TrimSuffix(l, "\r")
		if !strings.HasPrefix(l, "data:") {
			continue
		}
		body := l[len("data:"):]
		lines = append(lines, strings.TrimPrefix(body, " "))
	}
	return strings.Join(lines, "\n")
}

// A secret put back in a multi-line data: event, or in what a stream left
// at Finish with no blank line after it, goes back escaped for the JSON
// it is in: the quote and the line break in it broke the event's JSON
// when it was put back as plain text (#54).
func TestWriterRestoresEscapedInMultilineAndUnfinishedEvents(t *testing.T) {
	Forget()
	t.Cleanup(Forget)
	const secret = "a\"b\nc\\d"
	p := placeholder("SECRET", secret)

	for _, c := range []struct {
		name  string
		write string
	}{
		{
			name: "multi-line data",
			write: "event: content_block_delta\n" +
				`data: {"type":"content_block_delta","delta":` + "\n" +
				`data: {"type":"text_delta","text":"key ` + p + ` end"}}` + "\n\n",
		},
		{
			name:  "unfinished at Finish",
			write: `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"key ` + p + ` end"}}` + "\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "text/event-stream")
			rw := NewWriter(rec)
			if _, err := rw.Write([]byte(c.write)); err != nil {
				t.Fatal(err)
			}
			rw.Finish()
			out := rec.Body.String()
			payload := dataPayload(t, out)
			if !json.Valid([]byte(payload)) {
				t.Fatalf("the event is no longer JSON: %q", payload)
			}
			var v struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &v); err != nil {
				t.Fatalf("payload: %v: %q", err, payload)
			}
			if want := "key " + secret + " end"; v.Delta.Text != want {
				t.Fatalf("restored %q, want %q", v.Delta.Text, want)
			}
		})
	}
}
