// Run with Node's test runner and Playwright on the module path; see README.md.
// The Plugins page asks npm for every name it lists, and the server answers
// for 100 at most: 127 names left the last 27 with no version and no
// downloads for good. The page asks in batches, each of them whole. Until
// npm answers, a waiting button is named (a spinner alone has no name for
// a screen reader). Every language, both engines, wide and narrow; the API
// is faked here, answering as the server does (the first 100 names only).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const N = 127, CAP = 100; // the server answers for CAP names at most (internal/gui/plugins.go)
const pkgs = Array.from({ length: N }, (_, i) => "@magpie-community/gateway-p" + String(i).padStart(3, "0"));
const listings = pkgs.map((p, i) => ({ package: p, name: "P" + i, kind: "middleware", community: true, summary: { en: "Plugin " + i } }));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const state = { bun: true, bunVersion: "1.4.2", plugins: [] };
const CHECKING = { en: "Checking…", zh: "检查中…", "zh-TW": "檢查中…", ja: "確認中…", de: "Wird geprüft…" };

function server(lang, asks, gate) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.host !== "owl.test") return route.fulfill({ status: 404, body: "" });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/plugins/npm") {
      // as the server does: the first CAP names, the rest dropped
      const names = (url.searchParams.get("names") || "").split(",").filter(Boolean).slice(0, CAP);
      asks.push(names);
      await gate;
      return json({ npm: Object.fromEntries(names.map((n) => [n, { version: "1.0." + pkgs.indexOf(n), weekly: 10 + pkgs.indexOf(n) }])) });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname)) return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": more names than the server answers for are all asked, and a waiting button has a name", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(CHECKING)) for (const width of [980, 420]) {
      await t.test(lang + " " + width, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 820 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asks = [];
        page.on("pageerror", (e) => errors.push(e.message));
        let open;
        const gate = new Promise((r) => (open = r));
        await page.route("**/*", server(lang, asks, gate));
        await page.goto("http://owl.test/?view=plugins");
        await page.locator("#view-plugins .pm-card .pm-act").first().waitFor();
        for (let i = 0; i < 100 && !asks.length; i++) await sleep(50);
        // npm hasn't answered: every waiting button says what it waits for
        const waiting = await page.$$eval("#view-plugins .pm-act.busy", (bs) => bs.map((b) => ({ busy: b.getAttribute("aria-busy"), label: b.getAttribute("aria-label") })));
        assert.ok(waiting.length > 0, "buttons are waiting on npm");
        for (const b of waiting) {
          assert.equal(b.label, CHECKING[lang], "a waiting button is named");
          assert.equal(b.busy, "true");
        }
        open();
        // every name is asked, in asks the server can answer whole
        for (let i = 0; i < 100 && new Set(asks.flat()).size < N; i++) await sleep(50);
        assert.ok(asks.every((a) => a.length <= CAP));
        assert.equal(new Set(asks.flat()).size, N, "all " + N + " names asked: " + asks.map((a) => a.length));
        // none left waiting: even the last one has its version and Install
        await page.waitForFunction(() => !document.querySelector("#view-plugins .pm-act.busy"));
        const last = page.locator(`#view-plugins .pm-card[data-pkg="${pkgs[N - 1]}"]`);
        await last.waitFor();
        assert.match(await last.locator(".pm-ver").innerText(), /1\.0\.126/, "the last name has its version");
        assert.equal(await last.locator(".pm-act").isDisabled(), false);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}
