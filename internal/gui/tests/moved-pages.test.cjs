// Run with Node's test runner and Playwright on the module path; see README.md.
// The Gateway and Masking pages are parts of Settings now: the Gateway group
// on top of Network and sharing (with All models under Providers' list) and
// What was masked on top of Privacy. The old links open them there; Share on
// local network is one switch, not two; switched off in Plugins › Built-in,
// each part leaves with its plugin; and the gateway keys' timer runs only
// while the group is in sight: not on another tab, not off Settings, and
// again when it is back.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const assets = path.resolve(__dirname, "../assets");

// the keys' fixture, with the plugin switches the page boots with
async function openGateway(engine, lang, t, { featuresOff = [], where, width = 1100, keys } = {}) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(6000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const seen = { keys: 0 };
  const events = [];
  await page.clock.install();
  await page.route("**/*", fixture(lang, "light", events, { lan: true, keys }));
  // counted: what reads the keys while the timer runs
  await page.route("**/api/caller-keys", (route) => { if (route.request().method() === "GET") seen.keys++; return route.fallback(); });
  if (featuresOff.length) {
    await page.route("**/boot.js", (route) => route.fulfill({ contentType: "text/javascript",
      body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: false, featuresOff })};` }));
  }
  await page.goto("http://owl.test/" + where);
  return { page, errors, seen, events };
}

const spent = () => ({ period: "hours", hours: 5, start: new Date(Date.now() - 60e3).toISOString(), reset: new Date(Date.now() + 5 * 36e5).toISOString(),
  calls: 1, tokens: 1, cost: 1, tokensLeft: 0, costLeft: 29, costLimit: 30, spent: false });
const keyWithLimit = () => {
  const used = spent();
  return [{ id: "pool", name: "Pool", masked: "sk-owl-key-…6fcf81", limit: { period: "hours", hours: 5, cost: 30 }, used, usedAll: [used] }];
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the old Gateway and Masking links open their parts of Settings`, async (t) => {
      const gw = await openGateway(engine, lang, t, { where: "?view=gateway" });
      await gw.page.locator("#gateway .name").waitFor();
      assert.equal(await gw.page.evaluate(() => location.search), "?view=settings&tab=network", "the address says where it is now");
      assert.equal(await gw.page.locator("#setTab-network").getAttribute("aria-selected"), "true");
      assert.equal(await gw.page.locator("#view-settings").isVisible(), true);
      assert.equal(await gw.page.locator("#nav button[data-view=gateway], #nav button[data-view=masking]").count(), 0, "no page of their own in the sidebar");
      // the group is on top of the tab, its heading first
      const order = await gw.page.evaluate(() => {
        const at = (s) => document.querySelector(s)?.getBoundingClientRect().top;
        return [at("#gateway"), at("#portList"), at("#lanList"), at("#gatewayKeysBlock"), at("#foldConnect"), at("#foldCalls"), at("#proxySegs")];
      });
      assert.deepEqual(order, [...order].sort((a, b) => a - b), "status, address, sharing, keys, Connect, recent requests, then the rest");
      assert.deepEqual(gw.errors, []);

      const mk = await openGateway(engine, lang, t, { where: "?view=masking" });
      await mk.page.locator("#maskCard").waitFor();
      assert.equal(await mk.page.evaluate(() => location.search), "?view=settings&tab=privacy");
      assert.equal(await mk.page.locator("#setTab-privacy").getAttribute("aria-selected"), "true");
      assert.equal(await mk.page.locator("#maskList").isVisible(), true);
      assert.equal(await mk.page.locator("#maskAlertRow").count(), 1, "the alert is the part's");
      assert.deepEqual(mk.errors, []);
    });

    test(`${engine} ${lang}: Network and sharing has one Share on local network switch`, async (t) => {
      const { page, errors } = await openGateway(engine, lang, t, { where: "?view=settings&tab=network" });
      await page.locator("#lanList .segs").first().waitFor();
      await page.locator("#gateway .name").waitFor();
      const share = await page.evaluate(() => [...document.querySelectorAll("#setPage-network .name")]
        .filter((n) => n.offsetParent && n.textContent.trim() === window.t("Share on local network")).length);
      assert.equal(share, 1, "one row named Share on local network");
      assert.equal(await page.locator("#setPage-network .gw-share").count(), 0, "the card's second switch is gone");
      assert.equal(await page.locator("#gwDock .segs").count(), 0, "the group's card has no sharing choice of its own");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: switched off in Plugins › Built-in, the parts go with their plugin`, async (t) => {
      const off = await openGateway(engine, lang, t, { where: "?view=settings&tab=network", featuresOff: ["gateway"] });
      await off.page.locator("#portList .row").first().waitFor();
      for (const sel of ["#gwDock", "#gwDockMore", "#gateway", "#foldConnect", "#foldCalls"]) assert.equal(await off.page.locator(sel).isVisible(), false, sel + " is hidden");
      assert.equal(await off.page.locator("#portList .row").first().isVisible(), true, "the tab's own rows stay");
      // the gateway's own models list is on Providers: it goes with it
      await off.page.evaluate(() => show("providers"));
      await off.page.locator("#view-providers").waitFor();
      assert.equal(await off.page.locator("#gwModelsHead").isVisible(), false);
      assert.deepEqual(off.errors, []);

      const redact = await openGateway(engine, lang, t, { where: "?view=settings&tab=privacy", featuresOff: ["redact"] });
      await redact.page.locator("#redactList").waitFor({ state: "attached" });
      for (const sel of ["#maskDock", "#maskCard", "#maskList", "#maskAlertRow"]) assert.equal(await redact.page.locator(sel).isVisible(), false, sel + " is hidden");
      assert.equal(await redact.page.locator("#redactList").isVisible(), true, "the tab's own rows stay");
      // and a link to the page left is not a way to it
      assert.deepEqual(redact.errors, []);

      const on = await openGateway(engine, lang, t, { where: "?view=settings&tab=network" });
      await on.page.locator("#gateway .name").waitFor();
      for (const sel of ["#gwDock", "#gwDockMore", "#foldConnect"]) assert.equal(await on.page.locator(sel).isVisible(), true, sel + " is shown");
      await on.page.evaluate(() => show("providers"));
      assert.equal(await on.page.locator("#gwModelsHead").isVisible(), true);
      assert.deepEqual(on.errors, []);
    });

    test(`${engine} ${lang}: All models is folded under Providers' list until it is opened`, async (t) => {
      const { page, errors } = await openGateway(engine, lang, t, { where: "?view=providers" });
      await page.locator("#foldModels").waitFor();
      await page.locator("#modelsCount").filter({ hasText: "(" }).waitFor();
      assert.equal(await page.locator("#foldModels").getAttribute("aria-expanded"), "false");
      assert.equal(await page.locator("#gwModels").isVisible(), false);
      assert.equal(await page.locator("#gwModels .row.model").count(), 0, "nothing drawn while folded");
      await page.locator("#foldModels").click();
      assert.equal(await page.locator("#gwModels").isVisible(), true);
      assert.equal(await page.locator("#foldModels").getAttribute("aria-expanded"), "true");
      assert(await page.locator("#gwModels .row.model").count() > 0, "rows once opened");
      assert.deepEqual(errors, []);
    });
  }

  // Settings reads the providers for the group without drawing Providers'
  // list (loadProviders drew it for any page that wasn't a plugin's page,
  // which a thin or slow read could break); opening Providers then draws it
  // from what was read, at once, and again when the new read comes in.
  test(`${engine}: Providers' list is drawn at once on returning from the Gateway group, and again by the new read`, async (t) => {
    const { page, errors } = await openGateway(engine, "en", t, { where: "?view=settings&tab=network" });
    await page.locator("#gateway .name").waitFor();
    assert.equal(await page.locator("#providers > *").count(), 0, "Settings drew no list of providers");
    let release;
    const gate = new Promise((r) => { release = r; });
    let held = false;
    await page.route("**/api/providers", async (route) => { if (held) await gate; return route.fallback(); });
    held = true;
    await page.evaluate(() => show("providers"));
    await page.locator("#providers .row").first().waitFor({ timeout: 2000 }); // before the read held above is answered
    assert.equal(await page.locator("#providers .row").count(), 1, "the list read for the group is on the page at once");
    held = false;
    release();
    await page.locator("#providers .row").first().waitFor();
    assert.deepEqual(errors, []);
  });

  // The riskiest part of moving the page: its timer used to follow the
  // page's hidden attribute, and now follows where Settings is.
  test(`${engine}: the keys are read again only while the Gateway group is in sight`, async (t) => {
    const { page, errors, seen } = await openGateway(engine, "en", t, { where: "?view=settings&tab=network", keys: keyWithLimit() });
    await page.locator('#gatewayKeys .acc[data-key="pool"]').waitFor();
    const reads = async (ms) => { const before = seen.keys; await page.clock.runFor(ms); await page.waitForTimeout(150); return seen.keys - before; };
    assert((await reads(16e3)) >= 1, "in sight, the timer reads the keys");

    // another tab of Settings: nothing is read, however long
    await page.locator("#setTab-general").click();
    await page.waitForFunction(() => !document.querySelector("#gwDock").offsetParent);
    assert.equal(await reads(61e3), 0, "on another tab of Settings the keys are left alone");

    // back on the tab: read again, and the timer is back
    await page.locator("#setTab-network").click();
    await page.locator('#gatewayKeys .acc[data-key="pool"]').waitFor();
    assert((await reads(16e3)) >= 1, "back on the tab, the timer reads again");

    // away from Settings: the same
    await page.evaluate(() => show("agents"));
    await page.waitForFunction(() => document.querySelector("#view-settings").hidden);
    assert.equal(await reads(61e3), 0, "off Settings the keys are left alone");
    await page.evaluate(() => show("settings"));
    await page.waitForFunction(() => !!document.querySelector("#gwDock").offsetParent);
    assert((await reads(16e3)) >= 1, "Settings opened again on the tab, the timer is back");

    // and the page put away (the window hidden)
    await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, get: () => true }); document.dispatchEvent(new Event("visibilitychange")); });
    assert.equal(await reads(61e3), 0, "a hidden window reads nothing");
    assert.deepEqual(errors, []);
  });
}

test("every page the sidebar had is still a page, and the two moved ones are not", async () => {
  const html = await fs.readFile(path.join(assets, "index.html"), "utf8");
  assert(!/data-view="gateway"/.test(html) && !/data-view="masking"/.test(html));
  for (const v of ["agents", "sessions", "library", "providers", "routing", "usage", "plugins"]) assert(html.includes(`data-view="${v}"`), v);
});
