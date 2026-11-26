package plugin

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A tool or a search the agent cancelled stops with the call: the plugin's
// execution context gets an abort that follows the caller's signal, where
// runTool and runSearch used to take only owl's own timeout (#45).
func TestHostToolAndSearchFollowTheCallersAbort(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH")
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", hostAbortTestJS, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("host.js's cancellation: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Logf("%s", out)
	}
}

const hostAbortTestJS = `
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
const dir = process.argv[1];
const source = fs.readFileSync(path.join(dir, "host.js"), "utf8");
const section = (from, to) => {
  const a = source.indexOf(from);
  const b = source.indexOf(to, a);
  assert.ok(a >= 0 && b > a, "production section " + from);
  return source.slice(a, b);
};
const tick = () => new Promise((r) => setImmediate(r));
const waitFor = async (ok, label) => {
  for (let i = 0; i < 100 && !ok(); i++) await tick();
  assert.ok(ok(), label);
};

// runTool: the tool's ctx.abort follows the caller's signal at once
{
  let seen;
  const ctx = vm.createContext({
    AbortSignal, Promise, setTimeout, clearTimeout, Buffer, console,
    directory: "/tmp",
    toolsOf: () => new Map([["t", { args: undefined, execute: (_args, c) => { seen = c; return new Promise(() => {}) } }]]),
    parsedArgs: (_n, _d, a) => a,
    pluginALS: { run: (_s, fn) => fn() },
  });
  vm.runInContext(section("const took =", "// ---- web search APIs"), ctx);
  const runTool = vm.runInContext("runTool", ctx);
  const ctl = new AbortController();
  const p = runTool({ spec: "s", name: "t", args: {}, ms: 60_000 }, ctl.signal);
  p.catch(() => {});
  await waitFor(() => seen, "the tool's execute ran");
  assert.equal(seen.abort.aborted, false, "aborted before the caller was");
  ctl.abort();
  assert.equal(seen.abort.aborted, true, "the tool's ctx.abort doesn't follow the caller");
  await assert.rejects(p, /cancelled/, "the tool call didn't end with the caller");
}

// runSearch: the same for a search API
{
  let seen;
  const ctx = vm.createContext({
    AbortSignal, Promise, setTimeout, clearTimeout, Buffer, console,
    directory: "/tmp",
    searchesOf: () => new Map([["s", { execute: (_a, c) => { seen = c; return new Promise(() => {}) } }]]),
    searchAuthOf: () => "",
  });
  vm.runInContext(section("async function runSearch", "function authOf"), ctx);
  const runSearch = vm.runInContext("runSearch", ctx);
  const ctl = new AbortController();
  const p = runSearch({ spec: "s", id: "s", query: "q", count: 1, ms: 60_000 }, ctl.signal);
  p.catch(() => {});
  await waitFor(() => seen, "the search's execute ran");
  assert.equal(seen.abort.aborted, false, "aborted before the caller was");
  ctl.abort();
  assert.equal(seen.abort.aborted, true, "the search's ctx.abort doesn't follow the caller");
  await assert.rejects(p, /cancelled|no answer/, "the search call didn't end with the caller");
}

console.log("host abort ok");
process.exit(0);
`
