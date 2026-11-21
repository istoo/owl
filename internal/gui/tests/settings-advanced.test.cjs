// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings' Advanced (the owner: 产品要简单好用): each part shows the rows
// most readers touch and keeps the rest in a fold at its foot, the provider
// editor's own (details.more), closed on a default setup. Nothing is
// removed: a row set to something other than its default opens the fold,
// so a changed setting is never hidden; Settings' search and a link to a
// row open it for the row they name; Colorblind mode, Reduce motion and
// the two telemetry rows are never in it; About no longer lists the
// Gateway URL the Gateway group shows. No backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart, openAdvanced } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function settingsPayload(over) {
  return {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd", textSize: 100,
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/owl", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function server(lang, over) {
  const cur = settingsPayload({ lang, ...over });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false,textSize:100};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", textSize: 100 }, fx: cur.fx });
    if (url.pathname === "/api/settings") return json(cur);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// per part: a row that stays in sight, a row in the fold, and a setting
// that is not its default (its row's id, and the words that find the row)
const PARTS = {
  general: { common: "#colorblindRow", folded: "#birdRow", over: { noBird: true }, find: "Loading bird" },
  usage: { common: "#trayCostRow", folded: "#quotaReadsRow", over: { quotaReads: "asked" }, find: "Allowance reads" },
  network: { common: "#proxySegs", folded: "#busyLoadRow", over: { bareModelIDs: true }, find: "Busy models last" },
  models: { common: "#imagesList .row", folded: "#classifyTimeoutRow", over: { claudePrintMode: true }, find: "Intent classifier timeout" },
  privacy: { common: "#redactList .row", folded: "#redactWordsAnyCaseRow", over: { redactWordsWhole: true, redact: true }, find: "Masked words in any case" },
  about: { common: "#about .row", folded: ".update-every-row", over: { updateChannel: "stable" }, find: "Check every" },
};

const fold = (page, id) => page.locator(`#setPage-${id} > details.set-adv`);
const isOpen = (page, id) => fold(page, id).evaluate((d) => d.open);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: Settings' Advanced`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const open = async (over, url = "/") => {
        const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.errors = [];
        page.on("pageerror", (e) => page.errors.push(e.message));
        await page.route("**/*", server(lang, over));
        await page.goto("http://owl.test" + url);
        await page.locator("#prefs").click();
        await page.locator("#themeSegs .opt").first().waitFor();
        return page;
      };
      const word = (page, k) => page.evaluate(([l, k]) => (l === "en" ? k : I18N[l][k]), [lang, k]);

      await t.test("a default setup shows each part's common rows and folds the rest", async () => {
        const page = await open({});
        const advanced = await word(page, "Advanced");
        assert(advanced && advanced !== "Advanced" || lang === "en", "Advanced has its translation");
        for (const [id, p] of Object.entries(PARTS)) {
          await openPart(page, id);
          await fold(page, id).waitFor({ state: "attached" });
          await page.locator(`#setPage-${id} ${p.common}`).first().waitFor();
          assert.equal(await isOpen(page, id), false, id + ": Advanced starts shut");
          assert.equal(await fold(page, id).locator(":scope > summary").textContent(), advanced, id + ": its name");
          assert.equal(await page.locator(`#setPage-${id} ${p.common}`).first().isVisible(), true, id + ": the common row shows");
          assert.equal(await page.locator(p.folded).first().isVisible(), false, id + ": the folded row is out of sight");
          assert.equal(await page.locator(`#setPage-${id} > details.set-adv ${p.folded}`).count(), 1, id + ": and in the fold");
          // pressed, it opens and shows it
          await openAdvanced(page, id);
          assert.equal(await page.locator(p.folded).first().isVisible(), true, id + ": opened, the row shows");
        }
        assert.deepEqual(page.errors, []);
      });

      await t.test("a row set to something other than its default is not hidden", async () => {
        for (const [id, p] of Object.entries(PARTS)) {
          const page = await open(p.over);
          await openPart(page, id);
          await page.locator(`#setPage-${id} ${p.common}`).first().waitFor();
          await page.waitForFunction((id) => document.querySelector(`#setPage-${id} > details.set-adv`)?.open, id);
          assert.equal(await page.locator(p.folded).first().isVisible(), true, id + ": its fold opened with the setting");
          // another part, with nothing set, stays shut
          const other = Object.keys(PARTS).find((k) => k !== id);
          await openPart(page, other);
          assert.equal(await isOpen(page, other), false, other + ": shut beside " + id);
          assert.deepEqual(page.errors, []);
        }
      });

      await t.test("shut by hand, it stays shut through a redraw", async () => {
        const page = await open({ noBird: true });
        await openPart(page, "general");
        await page.waitForFunction(() => document.querySelector("#setPage-general > details.set-adv")?.open);
        await fold(page, "general").locator(":scope > summary").press("Enter");
        await page.waitForFunction(() => !document.querySelector("#setPage-general > details.set-adv").open);
        await page.evaluate(() => renderSettings());
        await page.waitForTimeout(100);
        assert.equal(await isOpen(page, "general"), false);
      });

      await t.test("Settings' search finds a folded row and shows it; cleared, the fold shuts", async () => {
        const page = await open({});
        for (const [id, p] of Object.entries(PARTS)) {
          // the row's name in the language shown
          await page.locator("#setQ").fill(await word(page, p.find));
          await page.waitForFunction(([id, sel]) => {
            const r = document.querySelector(sel);
            return r && r.getClientRects().length && !r.closest(".set-miss") && document.querySelector(`#setPage-${id} > details.set-adv`).open;
          }, [id, p.folded]);
          assert.equal(await page.locator(p.folded).first().isVisible(), true, id + ": found");
          await page.locator("#setQ").fill("");
          await page.waitForFunction((id) => !document.querySelector(`#setPage-${id} > details.set-adv`).open, id);
          assert.equal(await page.locator(p.folded).first().isVisible(), false, id + ": put away again");
        }
        assert.deepEqual(page.errors, []);
      });

      await t.test("a link to a row in the fold opens it", async () => {
        const page = await open({});
        await page.evaluate(() => openSettingsAt(true, "models", "#replyModelRow"));
        await page.waitForFunction(() => document.querySelector("#setPage-models > details.set-adv")?.open);
        assert.equal(await page.locator("#replyModelRow").isVisible(), true);
        assert.deepEqual(page.errors, []);
      });

      await t.test("Colorblind mode, Reduce motion and the telemetry rows are never folded", async () => {
        const page = await open({});
        await openPart(page, "general");
        for (const id of ["#colorblindRow", "#reduceMotionRow"]) {
          assert.equal(await page.locator(`#setPage-general > details.set-adv ${id}`).count(), 0);
          assert.equal(await page.locator(id).isVisible(), true);
        }
        await openPart(page, "privacy");
        await page.locator("#shareList .row").first().waitFor();
        for (const name of ["Count me as a user", "Share the agents, providers and models I use", "Hide accounts"]) {
          assert.equal(await page.locator("#shareList .row", { has: page.locator(".name", { hasText: await word(page, name) }) }).first().isVisible(), true, name);
        }
        assert.equal(await page.locator("#setPage-privacy > details.set-adv #shareList").count(), 0);
        assert.deepEqual(page.errors, []);
      });

      await t.test("About no longer lists the Gateway URL", async () => {
        const page = await open({});
        await openPart(page, "about");
        await page.locator("#about .row").first().waitFor();
        await openAdvanced(page, "about");
        const names = await page.locator("#about .row .name").allTextContents();
        const gw = await word(page, "Gateway URL");
        assert(gw, "the Gateway group's own words are translated");
        assert(!names.some((n) => n.trim() === gw), "no Gateway URL row: " + names.join(","));
        assert.equal(await page.locator("#about").evaluate((a) => a.textContent.includes("127.0.0.1:3425")), false);
        assert.equal(await page.locator("#setPage-network #gwDock").count(), 1, "the Gateway group is where the address is");
        assert.deepEqual(page.errors, []);
      });
    });
  }
}
