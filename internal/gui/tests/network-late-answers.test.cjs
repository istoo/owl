// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Network draws the gateway group (its state, the browser page's)
// from answers that can come in late, rows that grow the page above the
// Advanced fold. A press on a row in the fold, made before they were in,
// moved the page: the page keeps what was pressed where it was, scrolling
// to do it (cors-origins, bare-ids on CI, where the answers were slower
// than the test). The fold now stays out of sight until both are in, so
// nothing in it can be pressed under rows that are still to come. Here the
// two answers take 900ms; the press is made as soon as the fold is there,
// and the page must not have moved a pixel at the end. Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, openAdvanced, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const LATE = 2500;

function server(posts) {
  const s = {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "/config/owl", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [],
    lan: false, lanURLs: [], corsOrigins: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data, status = 200) => route.fulfill({ json: data, status });
    const late = (data) => new Promise((ok) => setTimeout(ok, LATE)).then(() => json(data));
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light"};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" }, fx: s.fx });
    if (url.pathname === "/api/settings") return json(s);
    if (url.pathname === "/api/settings/cors") {
      posts.push(route.request().postDataJSON().origins);
      s.corsOrigins = posts.at(-1);
      return json(s);
    }
    if (url.pathname === "/api/web-page") return late({ port: 4999, mode: "" });
    if (url.pathname === "/api/providers") return late({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a press in Network's Advanced fold is not moved by answers that come in late", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const posts = [], errors = [];
    const page = await (await browser.newContext({ viewport: { width: 440, height: 520 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(12000);
    page.setDefaultNavigationTimeout(60000);
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server(posts));
    await watchSettingsReads(page);
    await page.goto("http://127.0.0.1:4999/?view=settings&tab=network");
    await settingsDrawn(page);
    await openAdvanced(page, "network");
    const view = page.locator("#view-settings");
    const row = page.locator("#corsList .rule-row");
    await row.waitFor();
    await view.evaluate((v) => { v.style.paddingBottom = "600px"; }); // room under the part, as cors-origins has
    await reach(page, row.locator("input"));
    await row.locator("input").fill("http://localhost:3000");
    const scroll = () => view.evaluate((v) => new Promise((ok) => requestAnimationFrame(() => requestAnimationFrame(() => ok(v.scrollTop)))));
    const before = await scroll();
    assert(before > 0, "the part is scrolled to");
    await row.locator("button", { hasText: "Add" }).click();
    await page.locator("#corsList .row:not(.rule-row) .name", { hasText: "http://localhost:3000" }).waitFor();
    await page.waitForTimeout(LATE);
    assert.deepEqual(posts, [["http://localhost:3000"]]);
    assert.equal(await scroll(), before, "the answers coming in moved the page");
    assert.deepEqual(errors, []);
  });
}
