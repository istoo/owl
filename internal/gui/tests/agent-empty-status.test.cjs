// Run with Node's test runner and Playwright on the module path; see README.md.
// With no model source at all, every Agent card said "Add a key or a
// subscription first; then there are models to connect", cut short, the same
// step the guide above gives with its button (best-in-class P0-3). The card
// now says its state only; the reason is in its hover. Chromium and WebKit,
// five languages; the API is faked. OWL_GUI_ASSETS points at an older asset
// folder to see it fail there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.OWL_GUI_ASSETS ? path.resolve(process.env.OWL_GUI_ASSETS) : path.resolve(__dirname, "../assets");
const models = [
  { id: "a/big", name: "Big", providerName: "A", icon: "generic" },
  { id: "b/big", name: "Big", providerName: "B", icon: "generic" },
];
const info = (ids) => ids.map((id) => ({ id, ready: true }));
const grp = (id, name, members) => ({ id, name, members, routing: "order" });
const templates = [
  { kind: "smart", group: grp("smart-split", "Hard to strong, easy to cheap", ["a/big", "b/big"]) },
  { kind: "thrifty", group: grp("cheap-first", "Cheap first", ["a/big", "b/big"]) },
  { kind: "steady", group: grp("never-stuck", "Never stuck", ["a/big", "b/big"]) },
  { kind: "plan", group: grp("plan-first", "Plan first", []), why: "no provider of yours is billed by a plan: add a subscription or token plan", need: "plan" },
  { kind: "fast", group: grp("fastest-first", "Fastest first", ["a/big", "b/big"]) },
  { kind: "even", group: grp("spread-evenly", "Spread evenly", ["a/big", "b/big"]) },
];

const status = { en: "Not connected · no models yet", zh: "未接入 · 还没有模型", "zh-TW": "未接入 · 還沒有模型", ja: "未接続 · まだモデルがありません", de: "Nicht verbunden · noch keine Modelle" };
const oldSaid = { en: "Add a key or a subscription first", zh: "先添加一个 key", "zh-TW": "先新增一個 key", ja: "まずキーか", de: "Zuerst einen Schlüssel" };

function serve(lang, view) {
  const state = {
    agents: [1, 2, 3, 4].map((i) => ({ id: "ag" + i, name: "Agent " + i, icon: "generic", path: "/fixture/ag" + i, fields: [{ key: "model", label: "model", value: "", options: [] }] })),
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models, pools: [], deciders: [], found: true, groups: [], templates });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(engine, lang, width, view, t) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 1000 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
  await page.route("**/*", serve(lang));
  await page.goto("http://owl.test/?view=" + view);
  return { page, errors };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(status)) {
    test(`${engine} ${lang}: an Agent card with no model source says its state, not the guide's step`, async (t) => {
      const { page, errors } = await open(engine, lang, 1280, "agents", t);
      const sts = page.locator(".row.agent .ag-st-t, .agent .ag-st-t");
      await sts.nth(3).waitFor();
      await page.waitForTimeout(300);
      const texts = await sts.evaluateAll((es) => es.map((e) => ({ text: e.textContent.trim(), cut: e.scrollWidth > e.clientWidth })));
      assert.equal(texts.length, 4);
      for (const x of texts) {
        assert.equal(x.text, status[lang]);
        assert.equal(x.cut, false, "the state is cut short");
        assert(!x.text.includes(oldSaid[lang]), "the card still says the guide's step");
      }
      // the guide's one button is the only "Add a provider" on the page
      assert.equal(await page.locator("button.ag-add").count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}
