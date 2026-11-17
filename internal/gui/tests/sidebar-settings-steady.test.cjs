// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings at the sidebar's foot stays where it was clicked (jen_celeus on
// X): its tabs used to be listed under it, so the foot grew upward and the
// button rose with it, out from under the pointer. The list now opens
// above the button, growing from it, and folds back the same way; the
// button never moves, while the list unrolls or after, in a wide window, a
// narrow one, a short one and one too short for the whole list.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.OWL_ASSETS || path.resolve(__dirname, "../assets"));

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false,sideGateway:{url:"http://127.0.0.1:3425",running:true,boot:true}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { url: "http://127.0.0.1:3425", running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// every frame for a while, the furthest Settings' button got from where it was
const drift = (page, ms) => page.evaluate((ms) => new Promise((done) => {
  const b = document.querySelector("#prefs"), at = b.getBoundingClientRect();
  let most = 0;
  const end = performance.now() + ms;
  const look = () => {
    const r = b.getBoundingClientRect();
    most = Math.max(most, Math.abs(r.top - at.top), Math.abs(r.bottom - at.bottom), Math.abs(r.left - at.left), Math.abs(r.right - at.right));
    if (performance.now() < end) requestAnimationFrame(look); else done(most);
  };
  requestAnimationFrame(look);
}), ms);

const sizes = [
  { name: "wide", width: 1200, height: 760 },
  { name: "narrow", width: 760, height: 760, side: true },
  { name: "short", width: 1200, height: 460 },
  // too short for the page: the button stays in its bar
  { name: "tiny", width: 1200, height: 340 },
];
const langs = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: Settings stays where it was clicked`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const size of sizes) for (const lang of langs) await t.test(`${size.name} ${lang}`, async () => {
      const context = await browser.newContext({ viewport: { width: size.width, height: size.height } });
      if (size.side) await context.addInitScript(() => { try { localStorage.setItem("owl.navLayout", "side"); } catch {} });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://owl.test/");
      await page.waitForSelector("#nav button.on");
      assert.ok(await page.evaluate(() => document.body.classList.contains("side-nav")), "the sidebar shows");
      // owl keeps Settings in the status bar under the page, its tabs on the
      // page's own strip (app.js layoutNav; no list in the sidebar)
      const prefs = page.locator("#sideStatus #prefs");
      const before = await prefs.boundingBox();

      // clicked, Settings opens and the button stays under the pointer
      await page.mouse.click(before.x + before.width / 2, before.y + before.height / 2);
      const opening = await drift(page, 700);
      const tabs = page.locator("#setTabs [role=tab]");
      await tabs.first().waitFor();
      assert.equal(await tabs.evaluateAll((bs) => bs.filter((b) => !b.hidden).length), 8, "Settings' tabs are on its strip");
      const after = await prefs.boundingBox();
      assert.ok(opening < 1, `the button moved ${opening}px while Settings opened`);
      assert.ok(Math.abs(after.y - before.y) < 1 && Math.abs(after.x - before.x) < 1, `the button stays put: ${JSON.stringify(before)} then ${JSON.stringify(after)}`);
      assert.ok(await page.locator("#prefs").evaluate((b) => b.classList.contains("on")), "Settings is open");
      assert.ok(await page.locator("#setTabs").isVisible(), "its strip is in sight");

      // a tab picked there: the button still where it was
      await page.locator("#setTab-network").click();
      await page.waitForFunction(() => document.querySelector("#setTab-network").classList.contains("on"));
      assert.ok((await drift(page, 300)) < 1, "picking a tab leaves the button put");
      assert.ok(Math.abs((await prefs.boundingBox()).y - before.y) < 1);

      // another page: Settings' strip leaves and the button never moved
      await page.locator('#nav button[data-view="agents"]').click();
      const closing = await drift(page, 600);
      await page.waitForFunction(() => !document.querySelector("#setTabs")?.offsetParent);
      assert.ok(closing < 1, `the button moved ${closing}px while Settings left`);
      assert.ok(Math.abs((await prefs.boundingBox()).y - before.y) < 1, "the button is where it began");

      assert.deepEqual(errors, []);
      await context.close();
    });
  });
}
