package browserext

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// runNode runs script in node with the extension's folder as argv[1].
func runNode(t *testing.T, script string) {
	t.Helper()
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
	out, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", script, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Logf("%s", out)
	}
}

// act's dialog watcher is a promise that only settles when a dialog opens,
// so once the action it raced with ends it must stop: it used to poll every
// 100ms for the life of the worker, holding the tab (#49).
func TestActStopsPollingWhenTheActionEnds(t *testing.T) {
	runNode(t, actPollTestJS)
}

const actPollTestJS = `
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
const dir = process.argv[1];
const source = fs.readFileSync(path.join(dir, "background.js"), "utf8");
const from = source.indexOf("async function act(");
const to = source.indexOf("// point is where to click");
assert.ok(from >= 0 && to > from, "act is in background.js");
let sleeps = 0;
const ctx = vm.createContext({
  console, setTimeout, clearTimeout,
  dialogOpen: () => { throw new Error("dialog open"); },
  chrome: {
    tabs: {
      query: async () => [],
      get: async () => ({ url: "https://example.com/" }),
      update: async () => {},
      onCreated: { addListener() {}, removeListener() {} },
    },
    windows: { getLastFocused: async () => null, update: async () => {} },
  },
  look: async () => null,
  human: { pace: async () => {}, done() {} },
  sleep: (ms) => { sleeps++; return new Promise((r) => setTimeout(r, Math.min(ms, 10))); },
  fault: (code, msg) => Object.assign(new Error(msg), { code }),
  settle: async () => {},
  notices: () => [],
  text: (s) => s,
  group: async () => {},
  sessions: new Map(),
  saveSessions: () => {},
  state: () => ({}),
  attach: async () => {},
  waitLoad: async () => {},
  page: async () => "(page)",
});
vm.runInContext(source.slice(from, to), ctx);
const act = vm.runInContext("act", ctx);
const t = { dialog: null, tabId: 1 };
const out = await act(t, "it clicked.", async () => "did", null, false);
assert.ok(out.content[0].includes("did"), "the action's say is in the answer: " + out.content[0]);
const at = sleeps;
await new Promise((r) => setTimeout(r, 150));
assert.equal(sleeps, at, "the dialog poll was still sleeping after the action ended (" + (sleeps - at) + " more)");
console.log("act poll stopped");
process.exit(0);
`

// A manual reconnect closes the old WebSocket and opens a new one. The old
// socket's close arrives afterwards; it must not clear the new
// connection's status or schedule another connect over it (#50).
func TestReconnectIgnoresTheOldSocketsClose(t *testing.T) {
	runNode(t, reconnectTestJS)
}

const reconnectTestJS = `
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
const dir = process.argv[1];
const source = fs.readFileSync(path.join(dir, "background.js"), "utf8");
const from = source.indexOf("let ws = null;");
const to = source.indexOf("// ---- sessions:");
assert.ok(from >= 0 && to > from, "the connection code is in background.js");

class FakeWS {
  static all = [];
  constructor(url) { this.url = url; this.readyState = 0; this.sent = []; FakeWS.all.push(this); }
  send(v) { if (this.readyState !== 1) throw new Error("not open"); this.sent.push(v); }
  close() { this.readyState = 3; queueMicrotask(() => { if (this.onclose) this.onclose(); }); }
  open() { this.readyState = 1; if (this.onopen) this.onopen(); }
}
const statuses = [];
const listeners = {};
const chrome = {
  storage: {
    local: { get: async () => ({}) },
    session: { set: async (v) => { if (v && v.status) statuses.push(v.status); } },
    onChanged: { addListener() {} },
  },
  action: { setBadgeText: async () => {}, setBadgeBackgroundColor: async () => {} },
  alarms: { create() {}, onAlarm: { addListener() {} } },
  runtime: {
    onStartup: { addListener() {} },
    onInstalled: { addListener() {} },
    onMessage: { addListener(f) { listeners.message = f; } },
  },
};
const ctx = vm.createContext({
  VERSION: "0.1.0", GATEWAY: "ws://127.0.0.1:9", KEY: "",
  navigator: { userAgent: "Chrome/140" }, WebSocket: FakeWS, chrome,
  setInterval: () => 0, setTimeout, clearTimeout, queueMicrotask, console,
});
vm.runInContext(source.slice(from, to), ctx);
const tick = () => new Promise((r) => setImmediate(r));
await tick(); await tick();
assert.equal(FakeWS.all.length, 1, "the first connection wasn't made");
FakeWS.all[0].open();
assert.equal(statuses.at(-1).connected, true, "the first connection isn't reported");

const before = statuses.length;
listeners.message({ type: "reconnect" }, null, () => {});
await tick(); await tick(); await tick();
assert.equal(FakeWS.all.length, 2, "a manual reconnect didn't open a new connection");
assert.equal(FakeWS.all[0].onclose, null, "the replaced socket's close is still listened to");
const after = statuses.slice(before);
assert.ok(!after.some((s) => s.connected === false), "the old socket's close reported a disconnect over the new connection: " + JSON.stringify(after));
FakeWS.all[1].open();
assert.equal(statuses.at(-1).connected, true, "the new connection isn't reported");

// a second reconnect works the same, so no state was left behind
listeners.message({ type: "reconnect" }, null, () => {});
await tick(); await tick(); await tick();
assert.equal(FakeWS.all.length, 3, "a second manual reconnect didn't open a connection");
FakeWS.all[2].open();
assert.equal(statuses.at(-1).connected, true, "the third connection isn't reported");

console.log("reconnect ok");
process.exit(0);
`
