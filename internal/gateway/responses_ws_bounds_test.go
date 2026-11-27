package gateway

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// H2: a warmup (generate false) that carries a conversation past the
// socket's bound is refused as an invalid request, never kept: a chain of
// them with ever more input grew the heap without a provider being asked.
func TestResponsesSocketBoundsTheConversation(t *testing.T) {
	fresh(t)
	was := socketConversationBytes
	socketConversationBytes = 200
	t.Cleanup(func() { socketConversationBytes = was })
	u := &respUp{}
	sockUp(t, u)
	_, ts := sockServer(t)
	c, _, err := dialSock(t, ts, "/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	big := `{"type":"message","role":"user","content":[{"type":"input_text","text":"` + strings.Repeat("x", 400) + `"}]}`
	got := c.turn(create("", big, `"generate":false`))
	e := last(got)
	if e.Get("type").String() != "error" || e.Get("status").Int() != http.StatusBadRequest ||
		e.Get("error.code").String() != "previous_response_too_large" {
		t.Fatalf("an oversized warmup: %v", got)
	}
	if u.calls() != 0 {
		t.Fatalf("a refused warmup went upstream")
	}
	// under the bound, the socket goes on
	if got = c.turn(create("", userHi, `"generate":false`)); last(got).Get("type").String() != "response.completed" {
		t.Fatalf("after: %v", got)
	}
}

// H2: the frames a client sends while a reply is in flight are bounded by
// count and bytes; past them the socket is told and closed, instead of
// holding every frame a flood sends.
func TestResponsesSocketQueueIsBounded(t *testing.T) {
	fresh(t)
	was := socketQueueFrames
	socketQueueFrames = 1
	t.Cleanup(func() { socketQueueFrames = was })
	u := &respUp{}
	u.reply = func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"resp_slow","object":"response","status":"in_progress","output":[]}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_slow","role":"assistant","status":"in_progress","content":[]}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_slow","delta":"wor"}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	sockUp(t, u)
	_, ts := sockServer(t)
	c, _, err := dialSock(t, ts, "/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.send(create("", userHi))
	if got, err := c.until("response.created"); err != nil || last(got).Get("response.id").String() != "resp_slow" {
		t.Fatalf("%v %v", got, err)
	}
	// one may wait; the next is over the queue
	c.send(create("", userMore))
	c.send(create("", userHi))
	got, err := c.until("error")
	if err != nil || last(got).Get("error.code").String() != "too_many_queued_messages" {
		t.Fatalf("%v %v", got, err)
	}
}

// L3: a non-stream reply that isn't JSON whole can't be embedded in an
// event: the socket says so as an error, rather than a frame no client can
// parse.
func TestResponsesSocketRefusesAWholeReplyThatIsNotJSON(t *testing.T) {
	fresh(t)
	u := &respUp{}
	u.reply = func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"response","id":"resp_1","output":[`)
	}
	sockUp(t, u)
	_, ts := sockServer(t)
	c, _, err := dialSock(t, ts, "/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := c.turn(create("", userHi))
	e := last(got)
	if e.Get("type").String() != "error" || e.Get("status").Int() != http.StatusBadGateway {
		t.Fatalf("a reply that isn't JSON: %v", got)
	}
}
