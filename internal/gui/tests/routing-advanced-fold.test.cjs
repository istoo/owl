// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page's two settings made once (Find groups on their own,
// Names in agents' lists) sit under Advanced, after the groups, so the
// templates and the user's groups come first on the page. Advanced starts
// folded; a value the user set (found groups off, a provider left out of
// them, the names not On) opens it, so nothing set is hidden. At 1280x860,
// Chromium and WebKit, in English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = [
  { id: "a/m", name: "m", providerName: "A", icon: "generic" },
  { id: "b/m", name: "m", providerName: "B", icon: "generic" },
];
const info = (ids) => ids.map((id) => ({ id, ready: true }));
const found = { id: "auto-m", name: "Model M", members: ["a/m", "b/m"], auto: true, ready: true, memberInfo: info(["a/m", "b/m"]) };
const templates = [
  { kind: "thrifty", group: { id: "cheap-first", name: "Cheap first", members: ["a/m", "b/m"], routing: "order" } },
  { kind: "steady", group: { id: "never-stuck", name: "Never stuck", members: ["a/m", "b/m"], routing: "order" } },
];
const words = { en: "Advanced", zh: "高级" };

function serve(lang, st) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/claude", fields: [] }], profiles: [], settings: { lang, theme: "light", ...(st.settings || {}) } };
  const groups = { models, pools: [], deciders: [], found: st.found !== false, skip: st.skip || [], skippable: [{ id: "a", name: "A" }], groups: [found], templates };
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
    if (url.pathname === "/api/groups") return json(groups);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(t, engine, lang, st) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 860 }, reducedMotion: "reduce" })).newPage();
  t.after(() => browser.close());
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  await page.route("**/*", serve(lang, st));
  await page.goto("http://owl.test/?view=routing");
  await page.locator(".rt-gtpl-card").first().waitFor();
  return { page, errors };
}
const top = (page, sel) => page.locator(sel).first().evaluate((e) => e.getBoundingClientRect().top);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: templates come before the two settings, which sit folded under Advanced`, async (t) => {
      const { page, errors } = await open(t, engine, lang, {});
      const adv = page.locator(".rt-gadv");
      assert.equal((await adv.locator("> summary").textContent()).trim(), words[lang]);
      assert.equal(await adv.evaluate((e) => e.open), false, "Advanced starts folded");
      assert.equal(await page.locator(".rt-gfound").isVisible(), false);
      assert.equal(await page.locator(".rt-gnames").isVisible(), false);
      // the page's order: the groups' head, the templates and groups, then Advanced
      assert.ok(await top(page, ".rt-gsec > .row-head") < await top(page, ".rt-gtpl-card"));
      assert.ok(await top(page, ".rt-gtpl-card") < await top(page, ".rt-group"));
      assert.ok(await top(page, ".rt-group") < await top(page, ".rt-gadv"));
      // and a template is in sight on the first screen: Advanced's fold does not push it off
      assert.ok(await top(page, ".rt-gtpl-card") < 860);
      await adv.locator("> summary").click();
      assert.equal(await page.locator(".rt-gfound").isVisible(), true);
      assert.equal(await page.locator(".rt-gnames").isVisible(), true);
      assert.deepEqual(errors, []);
    });
    for (const [name, st] of [["found groups off", { found: false }], ["a provider left out", { skip: ["a"] }],
      ["names not On", { settings: { plainOwnNames: true } }], ["names Off", { settings: { plainNames: true } }]]) {
      test(`${engine} ${lang}: ${name} is not hidden`, async (t) => {
        const { page, errors } = await open(t, engine, lang, st);
        assert.equal(await page.locator(".rt-gadv").evaluate((e) => e.open), true, "Advanced opens on a value the user set");
        assert.equal(await page.locator(".rt-gfound").isVisible(), true);
        assert.equal(await page.locator(".rt-gnames").isVisible(), true);
        assert.deepEqual(errors, []);
      });
    }
  }
}
