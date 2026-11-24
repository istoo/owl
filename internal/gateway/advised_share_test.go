package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unsafe"
)

// stringInBody says s is the body's own bytes: a string read from it that
// was not copied.
func stringInBody(body []byte, s string) bool {
	if len(s) == 0 {
		return false
	}
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	lo := uintptr(unsafe.Pointer(unsafe.SliceData(body)))
	return p >= lo && p+uintptr(len(s)) <= lo+uintptr(len(body))
}

// bytesInBody is stringInBody for a raw value.
func bytesInBody(body, raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	p := uintptr(unsafe.Pointer(unsafe.SliceData(raw)))
	lo := uintptr(unsafe.Pointer(unsafe.SliceData(body)))
	return p >= lo && p+uintptr(len(raw)) <= lo+uintptr(len(body))
}

// advisedShareBody is a Messages request as an agent sends it: a system
// prompt, a user's words and a picture, an assistant's thinking, a tool
// call and its result, and a tool, each under a sixteenth of the body.
func advisedShareBody() []byte {
	// the system prompt is Claude Code's, tens of KB: the body is past
	// validMemoMin, so holdBody holds it
	return []byte(`{"model":"fake/m1","max_tokens":10,"system":"` + strings.Repeat("the system prompt. ", 2000) + `","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"what does this file do"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"let me read it","signature":"sig-1"},{"type":"tool_use","id":"toolu_read","name":"Read File","input":{"path":"/a/b.go"}},{"type":"text","text":"reading it"}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"the file's words"}]}` +
		`],"tools":[{"name":"Read File","description":"reads a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]}`)
}

// The request's parse shares the body's bytes: every message's content,
// tool call arguments, tool results and schemas are the body's own, so
// reading a conversation of many short messages allocates its own
// structures, not a copy of the conversation. A masked body is read the
// same way, though no holdBody covers it: it is built once and only read.
func TestAdvisedParseSharesTheBody(t *testing.T) {
	body := advisedShareBody()
	for _, held := range []bool{true, false} {
		name := "masked"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			ctx, done := context.WithCancel(context.Background())
			if held {
				holdBody(ctx, body)
			}
			req, err := readAdvised(body, false, false, false)
			done()
			if err != nil {
				t.Fatal(err)
			}
			if !stringInBody(body, req.Model) {
				t.Error("the model is a copy, not the body's bytes")
			}
			if len(req.systemRaw) != 1 || !bytesInBody(body, req.systemRaw[0]) {
				t.Error("the system blocks are copies")
			}
			if len(req.Tools) != 1 || !bytesInBody(body, req.Tools[0].Schema) {
				t.Error("the tool's schema is a copy")
			}
			if !stringInBody(body, req.Tools[0].Name) || !stringInBody(body, req.Tools[0].Description) {
				t.Error("the tool's name or description is a copy")
			}
			var text, thinking, args, result, image bool
			for _, m := range req.Messages {
				for _, p := range m.Parts {
					switch p.Kind {
					case Text:
						text = text || stringInBody(body, p.Text)
					case Thinking:
						thinking = thinking || stringInBody(body, p.Text) || bytesInBody(body, p.Sealed)
					case ToolCall:
						args = args || bytesInBody(body, p.Args)
					case ToolResult:
						result = result || stringInBody(body, p.Text)
					case Image:
						image = image || stringInBody(body, p.Data)
					}
				}
			}
			for _, c := range []struct {
				name string
				ok   bool
			}{
				{"a message's text", text}, {"its thinking", thinking},
				{"a tool call's arguments", args}, {"a tool result", result},
				{"an image's bytes", image},
			} {
				if !c.ok {
					t.Errorf("%s is a copy of the body, not the body's own bytes", c.name)
				}
			}
		})
	}
}

// A conversation of many short messages is read without copying each:
// the many-message parse allocates its own structures for the messages,
// not the conversation again. With decodeJSON's copy of a value under a
// sixteenth of the body, a 266 KB body of 128 messages allocated the
// conversation over again — well past the half the bound allows.
func TestAdvisedParseDoesNotCopyEachMessage(t *testing.T) {
	const n = 512 << 10
	message := func(body int) string {
		txt, _ := json.Marshal(strings.Repeat("x", body/2))
		return `{"role":"user","content":` + string(txt) + `}`
	}
	one, lots := sameBytes(n, 128, message)
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, one)
	holdBody(ctx, lots)
	parse := func(body []byte) {
		if _, err := readAnthropic(body, false, false); err != nil {
			t.Fatal(err)
		}
	}
	perOne := allocBytes(func() { parse(one) }, 3)
	perLots := allocBytes(func() { parse(lots) }, 3)
	t.Logf("one message %d bytes: %d allocated; many: %d", len(one), perOne, perLots)
	if perLots > perOne+uint64(len(lots))/2 {
		t.Errorf("many short messages allocated %d for %d bytes, one message %d: each message was copied",
			perLots, len(lots), perOne)
	}
}

// What a Claude subscription's run keeps of a request past that request
// is its own: the tools' names, the results' ids (kept as the run's last
// results and in the lost calls), and an early result's words. Each was
// the request body's bytes, which the run would then keep alive until
// its conversation's next turn.
func TestClaudeRunKeepsNoRequestBodyBytes(t *testing.T) {
	body := []byte(`{"model":"fake/m1","max_tokens":10,"messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"toolu_sent","name":"Read File","input":{"path":"/a"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_sent","content":"the first result's words"}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_early","content":"the second result's words"}]}` +
		`],"tools":[{"name":"Read File","description":"reads a file","input_schema":{"type":"object"}}]}`)
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, body)
	req, err := readAdvised(body, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for name := range runToolNames(req.Tools) {
		if stringInBody(body, name) {
			t.Errorf("the run's tool name %q is the request body's bytes", name)
		}
	}
	for b, name := range bridgeNames(req.Tools) {
		if stringInBody(body, b) || stringInBody(body, name) {
			t.Errorf("the run's bridged name %q → %q is the request body's bytes", b, name)
		}
	}
	var results []Part
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Kind == ToolResult {
				results = append(results, p)
			}
		}
	}
	if len(results) != 2 || !stringInBody(body, results[1].Text) {
		t.Fatalf("the fixture's results are not the body's bytes: %+v", results)
	}
	run := &subscriptionRun{bridge: &subscriptionBridge{}, pending: map[string]chan mcpToolResult{}, early: map[string]mcpToolResult{}}
	run.pending[results[0].CallID] = make(chan mcpToolResult, 1)
	if _, err := run.continueWith(results, "", nil); err != nil {
		t.Fatal(err)
	}
	if len(run.results) != 2 {
		t.Fatalf("the run kept %d results: %v", len(run.results), run.results)
	}
	for _, id := range run.results {
		if stringInBody(body, id) {
			t.Errorf("the run's result id %q is the request body's bytes", id)
		}
	}
	if len(run.early) != 1 {
		t.Fatalf("the run kept %d early results", len(run.early))
	}
	for id, res := range run.early {
		if stringInBody(body, id) {
			t.Errorf("the early call's id %q is the request body's bytes", id)
		}
		for _, c := range res.Content {
			for _, v := range c {
				if s, ok := v.(string); ok && stringInBody(body, s) {
					t.Errorf("an early result's words %q are the request body's bytes", s)
				}
			}
		}
	}
}

// A run's adviceReq is the request it is answering, whose values are the
// body's bytes: when the request's context ends, so does the run's hold
// on it. An advisor call that arrives after that is answered saying the
// advisor is unavailable, never waited on for a conversation that is
// gone.
func TestClaudeAdvisorRequestLetGoWithItsContext(t *testing.T) {
	c := newTUICalls(t)
	ctx, cancel := context.WithCancel(context.Background())
	q := &Request{Advisor: "adv/pro"}
	c.run.adviseOn(ctx, q, New().adviceRound)
	c.run.mu.Lock()
	kept := c.run.adviceReq
	c.run.mu.Unlock()
	if kept != q {
		t.Fatal("the run did not keep the request it was asked to advise")
	}
	cancel()
	waitFor(t, "the advisor's request let go with its context", func() bool {
		c.run.mu.Lock()
		defer c.run.mu.Unlock()
		return c.run.adviceReq == nil
	})
	res := c.adviceCall("toolu_late")
	if !res.IsError || len(res.Content) == 0 {
		t.Fatalf("a late advisor call was answered %+v", res)
	}
	if text, _ := res.Content[0]["text"].(string); !strings.Contains(text, "unavailable") {
		t.Fatalf("a late advisor call was answered %q, want the advisor unavailable", text)
	}
}
