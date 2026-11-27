package browserext

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A detach gives up only for a call that attaches again while its focus
// change is out. A call that was already busy when the BUSY_MS timer
// fired is not one: the tab is let go as it always was (#M36c).
func TestDetachTellsANewAttachFromABusyCall(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH")
	}
	dir, err := filepath.Abs("ext")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", detachRaceJS, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const detachRaceJS = `
import { pathToFileURL } from "node:url";
import path from "node:path";
const dir = process.argv[1];
let detaches = 0;
const focus = [];
let release = null;
globalThis.chrome = {
  debugger: {
    attach: async () => {},
    detach: async () => { detaches++; },
    sendCommand: async (to, method, params) => {
      if (method === "Emulation.setFocusEmulationEnabled") {
        focus.push(params.enabled);
        if (params.enabled === false) return new Promise((r) => { release = r; });
      }
      return {};
    },
    onEvent: { addListener() {} },
    onDetach: { addListener() {} },
  },
  tabs: { get: async () => ({ url: "https://example.com/" }) },
};
const cdp = await import(pathToFileURL(path.join(dir, "cdp.js")).href);
const fail = (m) => { console.error(m); process.exit(1); };

// the BUSY_MS timer fires while a call is still running: detach still
// lets the tab go, as before
await cdp.attach(1);
const t = cdp.state(1);
t.busy = true;
const busy = cdp.detach(1);
release();
await busy;
if (detaches !== 1 || t.attached) fail("a busy call kept the tab after the BUSY timer: " + detaches + " " + t.attached);
if (focus[focus.length - 1] !== false) fail("the focus emulation was turned back on for a busy call: " + focus);

// a call attaches again while detach is turning the focus off: detach
// gives up and turns the focus back on
await cdp.attach(1);
t.busy = false;
const d = cdp.detach(1);
const a = cdp.attach(1);
release();
await d;
await a;
if (detaches !== 1) fail("the debugger was detached under a call that attached again: " + detaches);
if (!t.attached) fail("the tab isn't attached after the call");
if (focus[focus.length - 1] !== true) fail("the focus emulation wasn't turned back on: " + focus);

// with no call arriving, the detach goes through
t.busy = false;
const quiet = cdp.detach(1);
release();
await quiet;
if (detaches !== 2 || t.attached) fail("a quiet detach didn't let go: " + detaches + " " + t.attached);
process.exit(0);
`
