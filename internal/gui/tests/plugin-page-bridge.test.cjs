// Run with Node's test runner and Playwright on the module path; see README.md.
// The bridge a plugin's page uses for what it can't do itself, through a
// real iframe in both engines and a local fixture: a script added after the
// page loaded is fetched by the window and run from data:, the page's own
// file alone; a picture it shares goes to the server, within the same type,
// size, dimensions and rate the server holds; and an asset outside the
// page's folder or its service is refused. No attack is reproduced: the
// boundaries are checked at their edge.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const bridge = path.resolve(__dirname, "../pagebridge.js");
const TOK = "e".repeat(32);
const SPEC = "/x/bridge";
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");

const page = () => ({
  spec: SPEC, name: "bridge", key: "plugin-bridge", title: "Bridge",
  reads: [], url: `/_owl/plugin/${TOK}/index.html`, icon: "",
});

// what pluginpage.go's pagePolicy sends the page's own files, with data:
// for the script the bridge puts there; the window's own address
const policy = () => {
  const dir = `owl.test/_owl/plugin/${TOK}/`;
  const own = `https://${dir} http://${dir}`;
  return [
    "sandbox allow-scripts", "default-src 'none'", `script-src ${own} data:`, `style-src ${own} 'unsafe-inline'`,
    `img-src ${own} data:`, `font-src ${own}`, `media-src ${own}`, "connect-src 'none'", "frame-src 'none'",
    "worker-src 'none'", "object-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-ancestors 'self'",
  ].join("; ");
};

// RUN runs after the bridge, as a page's own script does: it shares a
// picture, adds a script the window must fetch, and asks for files the
// page may not have. Every answer goes back in one message.
const RUN = `(async () => {
  const png = Uint8Array.from(atob(${JSON.stringify(PNG.toString("base64"))}), (c) => c.charCodeAt(0));
  const out = {};
  const blob = () => new Blob([png], { type: "image/png" });
  try { await owl.image.copy(blob()); out.copy = "ok"; } catch (e) { out.copy = e.message; }
  await new Promise((r) => setTimeout(r, 700));
  try { await owl.image.save(blob(), "chart"); out.save = "ok"; } catch (e) { out.save = e.message; }
  await new Promise((r) => setTimeout(r, 700));
  try { await owl.image.save(blob(), "chart-2"); out.save2 = "ok"; } catch (e) { out.save2 = e.message; }
  try { await owl.image.save(blob(), "chart-3"); out.rate = "given"; } catch (e) { out.rate = e.message; }
  try { await owl.image.save(new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" }), "bad"); out.bad = "given"; } catch (e) { out.bad = e.message; }
  // a script added now is fetched by the window and run from data:
  const s = document.createElement("script");
  s.src = "extra.js";
  document.head.appendChild(s);
  await new Promise((r) => setTimeout(r, 800));
  out.extra = !!window.__extra;
  const ask = (url) => new Promise((res) => {
    const id = 9000 + Math.floor(Math.random() * 1000);
    addEventListener("message", (e) => { if (e.data?.id === id) res(e.data.error || "given"); });
    parent.postMessage({ owl: 1, id, method: "asset", params: { url } }, "*");
  });
  out.api = await ask(location.origin + "/api/state");
  out.service = await ask(new URL("_owl/service/x", location.href).href);
  parent.postMessage({ bridgeProbe: out }, "*");
})();`;

const INDEX = `<!doctype html><meta charset="utf-8"><script src="_owl/bridge.js"></script><script src="run.js"></script><p id="p">bridge</p>`;

function server(seen) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugin-pages") return json([page()]);
    if (url.pathname === "/api/plugin-panels") return json([]);
    if (url.pathname === "/api/plugin-pages/image") { seen.images.push(req.postDataJSON()); return json({ path: "/tmp/chart.png" }); }
    if (url.pathname.startsWith("/api/")) return json({});
    const m = url.pathname.match(/^\/_owl\/plugin\/([0-9a-f]{32})\/(.+)$/);
    if (m && m[1] === TOK) {
      const rel = m[2];
      const headers = { "Content-Security-Policy": policy(), "X-Content-Type-Options": "nosniff", "Cache-Control": "no-cache" };
      let body, type;
      // the page came as one document: the bridge says so, as
      // pageinline.go prepends it, so its later loads go through the window
      if (rel === "_owl/bridge.js") { body = "globalThis.__owlInlined=1;\n" + await fs.readFile(bridge, "utf8"); type = "text/javascript"; }
      else if (rel === "index.html") { body = INDEX; type = "text/html"; }
      else if (rel === "run.js") { body = RUN; type = "text/javascript"; }
      else if (rel === "extra.js") { body = "window.__extra = 1;"; type = "text/javascript"; }
      else return route.fulfill({ status: 404, body: "" });
      return route.fulfill({ status: 200, headers: { ...headers, "Content-Type": type }, body });
    }
    if (url.host !== "owl.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    try { await route.fulfill({ body: await fs.readFile(file), contentType: { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png" }[path.extname(file)] }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a page's script, picture and the edges of both`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1100, height: 700 }, reducedMotion: "reduce" });
    const p = await context.newPage();
    p.setDefaultTimeout(10000);
    p.setDefaultNavigationTimeout(60000);
    const errors = [];
    p.on("pageerror", (e) => errors.push(e.message));
    const seen = { images: [] };
    await p.route("**/*", server(seen));
    await p.goto("http://owl.test/");

    const got = p.evaluate(() => new Promise((res) => addEventListener("message", (e) => { if (e.data?.bridgeProbe) res(e.data.bridgeProbe); })));
    await p.locator("#nav button[data-plugin-page]").waitFor({ state: "attached" });
    await p.evaluate(() => show("plugin-bridge"));
    const probe = await got;

    // a picture it shares goes to the server, and the page's own script is
    // fetched by the window and run
    assert.equal(probe.copy, "ok", probe.copy);
    assert.equal(probe.save, "ok", probe.save);
    assert.equal(probe.save2, "ok", probe.save2);
    assert.equal(probe.extra, true, "the added script ran");
    assert.ok(seen.images.length >= 3, `the server saw ${seen.images.length} pictures`);
    assert.equal(seen.images[0].action, "copy");
    assert.equal(seen.images[1].action, "save");
    assert.equal(seen.images[1].name, "chart");

    // the edges: the same rate, the same PNG, and only the page's folder
    assert.match(probe.rate, /twice a second/, probe.rate);
    assert.match(probe.bad, /PNG/, probe.bad);
    assert.match(probe.api, /isn't the page's/, probe.api);
    assert.match(probe.service, /isn't the page's/, probe.service);
    assert.deepEqual(errors, []);
    await context.close();
  });
}