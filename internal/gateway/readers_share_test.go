package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// allocBytes is what one call of f allocates, after a warm-up.
func allocBytes(f func(), n int) uint64 {
	f()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range n {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(n)
}

// sameBytes returns two bodies of about n bytes: one conversation in a
// single message, and the same bytes in many messages. A reader that
// shares the body it reads allocates about as much for one as the other;
// one that copies each message (decodeJSON's copy of a value under a
// sixteenth of the body) allocates the many-message body over again.
func sameBytes(n, many int, message func(body int) string) (one, lots []byte) {
	one = []byte(`{"model":"m","messages":[` + message(n) + `]}`)
	var b strings.Builder
	b.WriteString(`{"model":"m","messages":[`)
	for i := range many {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(message(n / many))
	}
	b.WriteString(`]}`)
	return one, []byte(b.String())
}

// The readers that keep nothing they decode share the body's bytes:
// decoding a value under a sixteenth of the body copied it, and a
// conversation of many short messages paid that copy for every message.
// Each case compares the same bytes in one message against many, so the
// bound is the reader's own at that size, not a number this machine
// happened to print.
func TestReadersDoNotCopyEachMessage(t *testing.T) {
	const n = 512 << 10
	message := func(body int) string {
		half := body / 2
		txt, _ := json.Marshal(strings.Repeat("x", half))
		return `{"role":"user","content":[` +
			`{"type":"thinking","thinking":` + string(txt) + `,"signature":"sig"},` +
			`{"type":"text","text":` + string(txt) + `}]}`
	}
	one, lots := sameBytes(n, 128, message)
	// held, as a request's body is while it is read: a value a sixteenth of
	// the body or more is shared already, so what is left to fix is the
	// copy of each message under it
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, one)
	holdBody(ctx, lots)
	for _, tc := range []struct {
		name string
		f    func(body []byte)
	}{
		{"withoutThinkingBlocks", func(body []byte) {
			withoutThinkingBlocks(body, func(m, i int, b []byte) bool { return false })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			perOne := allocBytes(func() { tc.f(one) }, 3)
			perLots := allocBytes(func() { tc.f(lots) }, 3)
			t.Logf("one message %d bytes: %d allocated; many: %d", len(one), perOne, perLots)
			// a copy of every message is the many-message body again; the
			// reader's own output is the same either way
			if perLots > perOne+uint64(len(lots))/8 {
				t.Errorf("many short messages allocated %d for %d bytes, one message %d: each message was copied",
					perLots, len(lots), perOne)
			}
		})
	}

	t.Run("clientToolUseIDs", func(t *testing.T) {
		const calls = 256
		// a message's content is a call and a tool's words, so a copy of
		// the message is much larger than what reading it costs
		text, _ := json.Marshal(strings.Repeat("t", 2048))
		block := func(i int) string {
			return fmt.Sprintf(`{"type":"tool_use","id":"toolu_%04d","name":"Read","input":{"file_path":"/a/b.go"}},{"type":"text","text":%s}`, i, text)
		}
		var big strings.Builder
		big.WriteString(`{"model":"m","messages":[{"role":"assistant","content":[`)
		for i := range calls {
			if i > 0 {
				big.WriteByte(',')
			}
			big.WriteString(block(i))
		}
		big.WriteString(`]}]}`)
		var many strings.Builder
		many.WriteString(`{"model":"m","messages":[`)
		for i := range calls {
			if i > 0 {
				many.WriteByte(',')
			}
			fmt.Fprintf(&many, `{"role":"assistant","content":[%s]}`, block(i))
		}
		many.WriteString(`]}`)
		oneBody, lotsBody := []byte(big.String()), []byte(many.String())
		c, stop := context.WithCancel(context.Background())
		defer stop()
		holdBody(c, oneBody)
		holdBody(c, lotsBody)
		perOne := allocBytes(func() { clientToolUseIDs(oneBody) }, 3)
		perLots := allocBytes(func() { clientToolUseIDs(lotsBody) }, 3)
		t.Logf("one message %d bytes: %d allocated; many: %d", len(oneBody), perOne, perLots)
		if perLots > perOne+uint64(len(lotsBody))/8 {
			t.Errorf("many short messages allocated %d for %d bytes, one message %d: each message was copied",
				perLots, len(lotsBody), perOne)
		}
	})

	t.Run("walkImages", func(t *testing.T) {
		img := func() string {
			return `{"type":"text","text":"` + strings.Repeat("y", 2048) + `"},` +
				`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + allocBenchImage + `"}}`
		}
		const turns = 24
		var big strings.Builder
		big.WriteString(`{"model":"m","messages":[{"role":"user","content":[`)
		for i := range turns {
			if i > 0 {
				big.WriteByte(',')
			}
			big.WriteString(img())
		}
		big.WriteString(`]}]}`)
		var many strings.Builder
		many.WriteString(`{"model":"m","messages":[`)
		for i := range turns {
			if i > 0 {
				many.WriteByte(',')
			}
			fmt.Fprintf(&many, `{"role":"user","content":[%s]}`, img())
		}
		many.WriteString(`]}`)
		oneBody, lotsBody := []byte(big.String()), []byte(many.String())
		c, stop := context.WithCancel(context.Background())
		defer stop()
		holdBody(c, oneBody)
		holdBody(c, lotsBody)
		keep := func(body []byte) {
			if _, err := walkImages(provider.Anthropic, body, func(imageAt) (json.RawMessage, error) { return nil, nil }); err != nil {
				t.Fatal(err)
			}
		}
		perOne := allocBytes(func() { keep(oneBody) }, 3)
		perLots := allocBytes(func() { keep(lotsBody) }, 3)
		t.Logf("one turn %d bytes: %d allocated; many: %d", len(oneBody), perOne, perLots)
		if perLots > perOne+uint64(len(lotsBody))/8 {
			t.Errorf("many short turns allocated %d for %d bytes, one turn %d: each turn was copied",
				perLots, len(lotsBody), perOne)
		}
	})
}

// What the readers share they also leave as it was: each returns what it
// built, and the body it read is unchanged (guardHeldBodies checks the
// same across every route's held bodies).
func TestReadersLeaveTheBodyTheyShare(t *testing.T) {
	// leading and trailing space, as a body may come
	body := []byte(" \n" + `{"model":"m","system":"S","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"mail {{EMAIL_abcdefgh}}"}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/a"}}]},` +
		`{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}` +
		`]}`)
	keep := bytes.Clone(body)
	withoutThinkingBlocks(body, func(m, i int, b []byte) bool { return true })
	clientToolUseIDs(body)
	if _, err := walkImages(provider.Anthropic, body, func(imageAt) (json.RawMessage, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	withPlaceholderNote(provider.Anthropic, body)
	if !bytes.Equal(body, keep) {
		t.Fatalf("a reader wrote the body it read:\n%s\n%s", body, keep)
	}
}
