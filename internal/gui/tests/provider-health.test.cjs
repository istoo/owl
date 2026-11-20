// Run with Node's test runner and Playwright on the module path; see README.md.
// P1-2: one button on the Providers page checks every provider that is on,
// four at a time, with the request each vendor already serves (its model
// list), and marks each row with the latency, a refused key or an address
// nothing answers. A stop ends the requests under way, and the results go
// away with the page: they are a look, not a setting. Every language,
// Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const prov = (id, name, extra = {}) => ({
  id, name, icon: "generic", host: id + ".example.com", chat: "https://" + id + ".example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: id + "-chat", name: name + " Chat", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…" + id }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", ...extra,
});
// what each faked check answers; the page only draws what came back
const HEALTH = {
  ok1: { ok: true, kind: "ok", status: 200, ms: 120 },
  ok2: { ok: true, kind: "ok", status: 200, ms: 75 },
  auth: { ok: false, kind: "auth", status: 401, ms: 40, error: "invalid api key" },
  net: { ok: false, kind: "network", status: 0, ms: 0, error: "dial tcp: connection refused" },
  skip: { ok: false, kind: "unsupported", status: 0, ms: 0, error: "this sign-in has no model list owl can ask without an agent's request" },
};
const IDS = ["ok1", "ok2", "auth", "net", "skip"];

function fixture(lang) {
  const providers = {
    providers: [...IDS.map((id) => prov(id, id.toUpperCase())), prov("off", "Off", { off: true })],
    presets: [], excluded: [], gateway: { running: true, window: true },
  };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  let gate = true; // the first run is held so the stop has something to end
  const waiting = [];
  const asked = [], stopped = [];
  const inflight = { n: 0, max: 0 };
  const route = async (r) => {
    const request = r.request(), url = new URL(request.url());
    const json = (data) => r.fulfill({ json: data }).catch(() => {});
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/provider/test") {
      const body = request.postDataJSON();
      asked.push(body.id);
      inflight.n++;
      inflight.max = Math.max(inflight.max, inflight.n);
      try {
        if (gate) await new Promise((res) => waiting.push(res));
        await r.fulfill({ json: HEALTH[body.id] || { ok: true, kind: "ok", status: 200, ms: 50 } }).catch(() => {});
      } finally { inflight.n--; }
      return;
    }
    if (url.pathname === "/api/provider/teststop") {
      stopped.push(request.postDataJSON().run);
      gate = false;
      waiting.splice(0).forEach((res) => res());
      return r.fulfill({ status: 204 }).catch(() => {});
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
  return { route, asked, stopped, inflight, providers };
}

const until = async (fn, why, ms = 8000) => {
  const t0 = Date.now();
  while (!(await fn())) {
    if (Date.now() - t0 > ms) throw new Error("timed out: " + why);
    await new Promise((res) => setTimeout(res, 20));
  }
};
const pill = (page, id) => page.locator(`#providers .row.provider[data-id="${id}"] .pv-health`);
const overflow = (page) => page.evaluate(() => {
  const de = document.documentElement, v = document.querySelector("#view-providers");
  return [de.scrollWidth > de.clientWidth ? "page" : "", v.scrollWidth > v.clientWidth ? "view" : ""].filter(Boolean);
});

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Check all marks every provider, tells a refused key from an unreachable host, and stops", { timeout: 180000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
      assert.deepEqual(errors, []);
    });
    for (const lang of LANGS) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 420, height: 700 }, reducedMotion: "reduce" });
        const page = await ctx.newPage(), f = fixture(lang);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", f.route);
        await page.goto("http://owl.test/?view=providers");
        await page.locator("#providers > .row.provider").nth(IDS.length - 1).waitFor();
        const tr = (s, vars) => page.evaluate(([s, vars]) => t(s, vars), [s, vars]);
        const btn = page.locator("#pvHealth");

        // the entry: an existing text button beside the search box
        assert(await btn.isVisible(), "no Check all on the Providers page");
        assert.equal((await btn.textContent()).trim(), await tr("Check all"));
        assert.equal(await btn.getAttribute("title"), await tr("Check every provider that is on: one small model-list request each, with its saved key and proxy"));
        if (lang !== "en") assert.notEqual(await tr("Check all"), "Check all", "no translation");
        assert.deepEqual(await overflow(page), []);
        const y0 = await page.evaluate(() => scrollY);

        // a click asks the ones on, not the one off, four at a time, and
        // every row says it is under way before the first answer
        await btn.click();
        await until(async () => (await pill(page, "ok1").textContent()) === (await tr("Checking…")), "a row says it is checking");
        assert.equal((await btn.textContent()).trim(), await tr("Stop checking"));
        assert.equal(await btn.getAttribute("aria-busy"), "true");
        for (const id of IDS) assert.equal((await pill(page, id).textContent()).trim(), await tr("Checking…"), id);
        await until(() => f.inflight.max === 4, "four checks at once");
        assert.equal(f.asked.includes("off"), false, "the provider turned off was asked");
        assert.deepEqual(await overflow(page), []);

        // Stop ends them: each request under way is cancelled on the
        // server, and the rows go back to nothing
        await btn.click();
        await until(async () => (await btn.textContent()).trim() === (await tr("Check all")), "the button is back");
        await until(() => f.stopped.length === 4, "four requests stopped");
        assert.equal(f.stopped.length, 4, "stopped " + f.stopped.length + " requests");
        assert.equal(await page.locator("#providers .pv-health").count(), 0, "a stopped check left a result");
        assert.equal(await btn.getAttribute("aria-busy"), "false");
        assert.equal(await page.evaluate(() => scrollY), y0, "the click scrolled the page");

        // a run let through marks each row: latency, a refused key, an
        // address nothing answers, one owl cannot ask
        await btn.click();
        await until(async () => (await page.locator("#providers .pv-health.ok").count()) === 2 &&
          (await page.locator("#providers .pv-health.auth").count()) === 1 &&
          (await page.locator("#providers .pv-health.network").count()) === 1 &&
          (await page.locator("#providers .pv-health.unsupported").count()) === 1, "every row marked");
        assert.equal((await pill(page, "ok1").textContent()).trim(), await tr("{ms} ms", { ms: 120 }));
        assert.equal((await pill(page, "ok2").textContent()).trim(), await tr("{ms} ms", { ms: 75 }));
        assert.equal((await pill(page, "auth").textContent()).trim(), await tr("Key refused"));
        assert.equal((await pill(page, "net").textContent()).trim(), await tr("Unreachable"));
        assert.equal((await pill(page, "skip").textContent()).trim(), await tr("Can't check"));
        assert((await pill(page, "ok1").getAttribute("title")).includes(await tr("Answered in {ms} ms; the key was accepted", { ms: 120 })));
        assert((await pill(page, "auth").getAttribute("title")).includes(await tr("The provider answered {status}: it refused the key. Paste a current key in this provider's settings", { status: 401 })));
        assert((await pill(page, "auth").getAttribute("title")).includes("invalid api key"), "the vendor's words are gone");
        assert((await pill(page, "net").getAttribute("title")).includes(await tr("Couldn't reach {host}. Check the provider's address and proxy", { host: "net.example.com" })));
        assert.deepEqual(await overflow(page), []);
        // four before the stop, all five after it
        const askedTwice = f.asked.filter((id) => IDS.includes(id)).length;
        assert.equal(askedTwice, 4 + IDS.length, "asked " + askedTwice + " checks in two runs");

        // the page draws the list again for a reload or a save: the
        // results are still on it
        await page.evaluate(() => renderProviders());
        await until(async () => (await pill(page, "ok1").textContent()).trim() === (await tr("{ms} ms", { ms: 120 })), "a redraw keeps the result");
        assert.equal(await page.locator("#providers .pv-health").count(), IDS.length);

        // the results are this page's: a reload has none
        await page.reload();
        await page.locator("#providers > .row.provider").nth(IDS.length - 1).waitFor();
        assert.equal(await page.locator("#providers .pv-health").count(), 0, "a reload kept the results");
        assert.equal((await page.locator("#pvHealth").textContent()).trim(), await tr("Check all"));
        await ctx.close();
      });
    }
  });
}