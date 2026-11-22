const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

// Gateway keys are left out of Settings > Network while owl isn't shared, and a
// tunnel to loopback (Cloudflare Tunnel) is refused until it is. DD on
// Discord looked for the keys to put a tunnel behind one and found none:
// the page says where they come from, and sharing is switched on right
// there, in the same tab (the Local-network sharing switch). Shared, the line gives way to the keys.
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const locale of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${locale} ${width}: Settings > Network says where gateway keys come from while not shared`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        await page.addInitScript(() => { try { localStorage.setItem("owl.gwConnectFolded", "0"); } catch {} }); // Connect opened, as a user who reads it
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(locale, "dark", []));
        await page.goto("http://owl.test/?view=settings&tab=network");
        await page.locator("#connect .snip").waitFor();

        const hint = page.locator("#gatewayKeysOff");
        await hint.waitFor({ state: "visible" });
        assert.equal(await page.locator("#gatewayKeysBlock").isVisible(), false);
        const text = await hint.textContent();
        assert(text.includes("Cloudflare Tunnel"), text);
        if (locale !== "en") assert(!text.includes("other computers"), "translated: " + text);
        // the line fits the page
        const fits = await hint.evaluate((p) => p.getBoundingClientRect().right <= document.documentElement.clientWidth + 1);
        assert(fits, "the hint overflows at " + width);

        // shared from the card: the keys show and the line is gone
        const y = [await page.evaluate(() => scrollY), await page.locator("#view-settings").evaluate((v) => v.scrollTop)]; // Settings scrolls in #view-settings, the window not at all
        await page.locator("#lanList .segs button").last().click();
        await page.locator("#gatewayKeysBlock").waitFor({ state: "visible" });
        assert.equal(await hint.isVisible(), false);
        assert.deepEqual(y, [await page.evaluate(() => scrollY), await page.locator("#view-settings").evaluate((v) => v.scrollTop)], "sharing moved the page");
        assert.deepEqual(errors, []);
      });
    }
  }
}
