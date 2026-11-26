package claudebridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Two calls bringing new tools at once are both answered once Claude Code
// lists the tools: the second used to replace the first's waiter, and the
// first call then waited out listWait with no one to wake it (#16).
func TestConcurrentRetoolsBothAnswered(t *testing.T) {
	var n atomic.Int32
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "loaded"}},
			"tools": []map[string]any{
				{"name": "ToolSearch", "inputSchema": map[string]any{"type": "object"}},
				{"name": fmt.Sprintf("WebFetch%d", i), "inputSchema": map[string]any{"type": "object"}},
			},
		})
	}))
	defer cb.Close()
	dir := t.TempDir()
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"ToolSearch","inputSchema":{"type":"object"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	go func() { _ = RunMCP([]string{cb.URL + "/_owl/claude-mcp/tok", toolsPath}); outW.Close() }()

	lines := make(chan map[string]any, 32)
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var m map[string]any
			_ = json.Unmarshal(sc.Bytes(), &m)
			lines <- m
		}
		close(lines)
	}()
	send := func(s string) { _, _ = inW.Write([]byte(s + "\n")) }
	next := func() map[string]any {
		t.Helper()
		select {
		case m := <-lines:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no message from the helper")
			return nil
		}
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	next()
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ToolSearch","arguments":{},"_meta":{"claudecode/toolUseId":"toolu_1"}}}`)
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ToolSearch","arguments":{},"_meta":{"claudecode/toolUseId":"toolu_2"}}}`)
	// both are waiting for the list before their answers
	for i := 0; i < 2; i++ {
		if m := next(); m["method"] != "notifications/tools/list_changed" {
			t.Fatalf("message %d = %v, want a tools/list_changed", i, m)
		}
	}
	send(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)

	answered := map[float64]bool{}
	by := time.After(2 * time.Second)
	for len(answered) < 2 {
		select {
		case m := <-lines:
			if id, ok := m["id"].(float64); ok && (id == 2 || id == 3) {
				if m["result"] == nil {
					t.Fatalf("call %v answered %v", id, m)
				}
				answered[id] = true
			}
		case <-by:
			t.Fatalf("only %v answered within 2s: a retool's waiter was lost", answered)
		}
	}
	_ = inW.Close()
}

// A callback URL that isn't one is an RPC error, not a nil request
// dereferenced (#16).
func TestMCPCallBackURLInvalidIsARPCError(t *testing.T) {
	dir := t.TempDir()
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"read","inputSchema":{"type":"object"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	ended := make(chan error, 1)
	go func() { ended <- RunMCP([]string{"http://[::1", toolsPath}); outW.Close() }()
	lines := make(chan map[string]any, 8)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var m map[string]any
			_ = json.Unmarshal(sc.Bytes(), &m)
			lines <- m
		}
		close(lines)
	}()
	next := func() map[string]any {
		t.Helper()
		select {
		case m := <-lines:
			return m
		case <-ended:
			t.Fatal("the helper ended instead of answering")
			return nil
		case <-time.After(3 * time.Second):
			t.Fatal("no message from the helper")
			return nil
		}
	}
	_, _ = inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"))
	next()
	_, _ = inW.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read","arguments":{}}}` + "\n"))
	m := next()
	rpcErr, _ := m["error"].(map[string]any)
	if rpcErr == nil || rpcErr["code"] != float64(-32000) {
		t.Fatalf("an invalid callback URL answered %v, want a -32000 error", m)
	}
	_ = inW.Close()
}
