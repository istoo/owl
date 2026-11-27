package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// askWriter is the writer an exchange writes its request to: each write
// goes down a channel, safe to read as it comes.
type askWriter struct {
	h  http.Header
	ch chan string
}

func (w *askWriter) Header() http.Header {
	if w.h == nil {
		w.h = http.Header{}
	}
	return w.h
}
func (w *askWriter) WriteHeader(int) {}
func (w *askWriter) Write(b []byte) (int, error) {
	w.ch <- string(b)
	return len(b), nil
}

// M11: an answer to a request owl made of a client is taken only from the
// caller, server and session the request went to; another one can't
// answer for it.
func TestMCPAnswerIsBoundToItsCaller(t *testing.T) {
	w := &askWriter{ch: make(chan string, 4)}
	x := &mcpExchange{w: w, canStream: true, key: "server\x00key\x00session"}
	ctx := context.WithValue(context.Background(), mcpExchangeKey{}, x)
	got := make(chan mcpMessage, 1)
	go func() {
		m, err := askClient(ctx, "roots/list", nil, 3*time.Second)
		if err == nil {
			got <- m
		}
	}()
	// the request owl wrote to the client, its id
	var id json.RawMessage
	select {
	case body := <-w.ch:
		if i := strings.Index(body, "data: "); i >= 0 {
			line := strings.TrimSpace(strings.SplitN(body[i+6:], "\n", 2)[0])
			var m struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal([]byte(line), &m) == nil {
				id = m.ID
			}
		}
	case <-time.After(2 * time.Second):
	}
	if id == nil {
		t.Fatal("no request reached the client")
	}
	answer := mcpMessage{ID: id, Result: json.RawMessage(`{"roots":[]}`)}
	clientAnswered(answer, "server\x00other\x00session")
	select {
	case m := <-got:
		t.Fatalf("another caller answered it: %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
	clientAnswered(answer, "server\x00key\x00session")
	select {
	case m := <-got:
		if string(m.ID) != string(id) {
			t.Fatalf("answer %s, want %s", m.ID, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the caller's own answer wasn't taken")
	}
}
