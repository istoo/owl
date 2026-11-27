package claudetui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tapCRLFStub answers a Messages stream whose CRLF is split across chunks:
// the first ends with \r and the next begins with \n. The event's JSON is
// in two data lines, as SSE allows, so a \n\n made of that pair splits it
// where there is no blank line and both halves are dropped.
const tapCRLFStub = `if (!globalThis.Bun) globalThis.Bun = {};
const enc = new TextEncoder();
Bun.FetchSession = class {
  async fetch(input, init) {
    const chunks = [
      "event: message_start\r\ndata: {\"type\":\r",
      "\ndata: \"message_start\",\"message\":{\"id\":\"m1\"}}\r\n\r\n",
    ];
    return new Response(new ReadableStream({
      start(c) { this.i = 0; this.chunks = chunks; this.enc = enc; },
      pull(c) {
        if (this.i < this.chunks.length) c.enqueue(this.enc.encode(this.chunks[this.i++]));
        else c.close();
      },
    }), { headers: { "content-type": "text/event-stream" } });
  }
};
`

// tapCRLFMain makes one main-turn request and waits for the tap to say the
// stream ended, then prints the events it read.
const tapCRLFMain = `const fs = require("fs");
const file = process.argv[process.argv.length - 1];
(async () => {
  const s = new Bun.FetchSession();
  const res = await s.fetch("https://api.anthropic.com/v1/messages", {
    method: "POST",
    headers: { "x-claude-code-request-class": "main", "content-type": "application/json" },
    body: "{}",
  });
  for (let i = 0; i < 300; i++) {
    let t = "";
    try { t = fs.readFileSync(file, "utf8"); } catch {}
    if (t.indexOf("\"end\":\"done\"") >= 0) {
      const evs = t.trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((x) => x.e);
      console.log(JSON.stringify(evs));
      return;
    }
    await new Promise((r) => setTimeout(r, 10));
  }
  console.log("timeout");
})();
`

// M6: a \r at a chunk's end and the \n that begins the next are one line
// ending, not a blank line: the event they sit in stays whole.
func TestTapKeepsACRLFSplitAcrossChunks(t *testing.T) {
	ran := false
	for _, rt := range []string{"bun", "node"} {
		path, err := exec.LookPath(rt)
		if err != nil {
			continue
		}
		ran = true
		t.Run(rt, func(t *testing.T) {
			dir := t.TempDir()
			for name, js := range map[string]string{"stub.js": tapCRLFStub, "main.js": tapCRLFMain, "tap.js": string(tapJS)} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(js), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			tapFile := filepath.Join(dir, "tap.jsonl")
			pre := "--preload"
			if rt == "node" {
				pre = "-r"
			}
			cmd := exec.Command(path, pre, filepath.Join(dir, "stub.js"), pre, filepath.Join(dir, "tap.js"), filepath.Join(dir, "main.js"), tapFile)
			cmd.Env = append(os.Environ(), tapEnv+"="+tapFile)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if strings.Contains(string(out), "timeout") {
				t.Fatalf("the tap never ended: %s", out)
			}
			var got []map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
				t.Fatalf("the tap's events: %v (%s)", err, out)
			}
			if len(got) != 1 {
				t.Fatalf("events %v, want one", got)
			}
			if e, _ := got[0]["e"].(map[string]any); e["type"] != "message_start" {
				t.Fatalf("events %v, want the one message_start", got)
			}
		})
	}
	if !ran {
		t.Skip("neither Bun nor Node is here to run tap.js")
	}
}
