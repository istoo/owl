// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel's overview (panelhome.js): a connected agent's model is a
// picker in its own row, the same picker the window's row opens. Picking a
// model or a routing group writes through the window's own /api/set, the
// row shows the new value, and the effect is said as in the window (the
// notice the server returns). The name still opens the Agents detail.
// Both engines, every language; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const NOTICE = "New sessions take it; switch the tiers in {cmd}.";
const MODELS = [
  { value: "claude-opus-5.5", label: "Claude Opus 5.5", icon: "claude-color", ref: "claude/claude-opus-5.5", group: "Claude" },
  { value: "claude-sonnet-5", label: "Claude Sonnet 5", icon: "claude-color", ref: "claude/claude-sonnet-5", group: "Claude" },
  { value: "g:steady", label: "Never stuck", group: "Routing groups" },
];

function world() {
  return { model: "claude-sonnet-5", sets: [], discovery: [{ fingerprint: "f1", source: "Claude Code" }] };
}

function agents(w) {
  return [
    { id: "claude", name: "Claude Code", path: "/fixture/claude", icon: "claudecode-color", wired: true, version: "2.0.0",
      fields: [{ key: "model", label: "model", value: w.model, options: MODELS }] },
    { id: "codex", name: "Codex", path: "/fixture/codex", icon: "codex-color", wired: false,
      fields: [{ key: "model", label: "model", value: "", options: MODELS }] },
  ];
}

function server(lang, w) {
  const settings = { lang, theme: "light", tray: "panel", currency: "usd", guideHidden: true };
  const state = () => ({ agents: agents(w), profiles: [], settings, served: 20 });
  return async (route) => {
    const req = route.request(), url = new URL(req.url()), p = url.pathname;
    const json = (d) => route.fulfill({ json: d });
    if (p === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (p === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (p === "/api/set" && req.method() === "POST") {
      const body = req.postDataJSON();
      w.sets.push(body);
      w.model = body.value;
      return json({ ...state(), notice: NOTICE.replace("{cmd}", "/model") });
    }
    if (p === "/api/state") return json(state());
    if (p === "/api/settings") return json(settings);
    if (p === "/api/importapps/discovery") return json(w.discovery);
    if (p === "/api/usage/quotas") return json([]);
    if (p === "/api/usage/requests") return json({ period: "today", rows: [], calls: 0, errors: 0, series: [], by: { agent: [] } });
    if (p === "/api/usage") return json({ models: [], accounts: [] });
    if (p === "/api/groups") return json({ groups: [], models: [] });
    if (p === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (p === "/api/gateway/trace") return json({ mine: true, now: new Date().toISOString(), seq: 0, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    if (p === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (p === "/api/plugins") return json({ plugins: [] });
    if (p.startsWith("/api/")) return json({});
    const file = path.join(assets, p === "/" ? "index.html" : p);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function open(t, engine, lang) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width: 420, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(8000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const w = world();
  await page.route("**/*", server(lang, w));
  await page.goto("http://owl.test/?mode=panel");
  await page.locator("#pHome .ph-agent").first().waitFor();
  return { page, errors, w };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: a connected agent's model is picked in its overview row`, async (t) => {
      const { page, errors, w } = await open(t, engine, lang);
      const row = page.locator('#pHome .ph-agent[data-key="a:claude"]');
      const picker = row.locator(".ph-agent-model");
      assert.equal((await picker.textContent()).trim(), "Claude Sonnet 5", "the row says the model it starts on");
      assert.equal(await picker.getAttribute("aria-haspopup"), null, "a plain button opens the picker, not a native menu");
      await picker.click();

      const pop = page.locator("#pop");
      await pop.waitFor({ state: "visible" });
      const opt = (label) => page.locator("#list li").filter({ has: page.locator(".option-words .v", { hasText: label }) });
      await opt("Never stuck").waitFor();
      await opt("Never stuck").click();

      await page.waitForFunction(() => document.querySelector('#pHome .ph-agent[data-key="a:claude"] .ph-agent-model')?.textContent.trim() === "Never stuck");
      assert.deepEqual(w.sets, [{ agent: "claude", field: "model", value: "g:steady" }], "the window's own set route, with the group's value");
      assert.equal(await pop.isHidden(), true, "the picker closes on a pick");
      // the effect, in the server's own words, translated as the window says it
      const want = await page.evaluate((s) => tNotice(s), NOTICE.replace("{cmd}", "/model"));
      await page.waitForFunction((s) => document.querySelector("#status")?.textContent.includes(s), want);
      assert.match(await page.locator("#status").textContent(), new RegExp(want.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
      // the row itself still opens the Agents detail
      await row.locator(".ph-agent-go").click();
      await page.waitForFunction(() => document.body.dataset.ptab === "agents");
      assert.equal(await page.locator('#agents .row.agent[data-id="claude"]').getAttribute("aria-expanded"), "true");
      assert.deepEqual(errors, []);
      await page.context().close();
    });
  }
}