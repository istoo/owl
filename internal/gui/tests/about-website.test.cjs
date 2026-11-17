const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");
const { openPart } = require("./reader.cjs");

// Settings › About's Community row: owl has no website of its own, so where
// upstream names usemagpie.ai this row names the code and the issues, on
// GitHub, and opens github.com/istoo/owl, in every language and at the tray
// panel's width.
const words = {
  en: "Community", zh: "社区", "zh-TW": "社群", ja: "コミュニティ", de: "Community",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: About links the project on GitHub`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, "light", []));
        await page.goto("http://owl.test/");
        await page.locator("#prefs").click();
        await openPart(page, "about");
        const row = page.locator("#about .row", { has: page.locator(".who", { hasText: words[lang] }) });
        await row.waitFor();
        const repo = row.locator("button.github");
        assert.equal(await repo.getAttribute("title"), "github.com/istoo/owl");
        const opened = page.waitForRequest((r) => r.url().includes("/api/open"));
        await repo.click();
        assert.equal(JSON.parse((await opened).postData()).url, "https://github.com/istoo/owl");
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "About wider than the window");
        assert.deepEqual(errors, []);
      });
    }
  }
}