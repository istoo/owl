// Run with Node's test runner and Playwright on the module path; see README.md.
// P1-1: a connected agent's card says which provider, model and account its
// last request through owl went to, and what it has spent today, without
// opening Routing or Usage. A request answered by another provider first
// says where it was rerouted from and why; an agent nothing went through
// today says so rather than showing $0 or nothing; an agent not connected
// through owl has no such line. The tray panel's overview and its Agents
// tab say the same, from the same state. Every language, both engines, the
// window at 1280 and 420px; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const model = (v) => ({ value: v, label: v, ref: v });
const agent = (id, name, wired, live) => ({
  id, name, icon: "generic", path: "/fixture/" + id, wired,
  fields: [{ key: "model", label: "model", value: "vendor/m1", options: [model("vendor/m1")] }],
  live,
});
const LIVE = {
  at: new Date(Date.now() - 4 * 60e3).toISOString(),
  route: 17, provider: "Vendor", model: "m1", account: "me@example.com",
};
// today's spend as the page or panel that already reads usage says it
// (window.agentToday); the live line does not read the usage summary itself
const TODAY = {
  cost: 0.123, calls: 2, unpriced: 0,
  by: { agent: [{ id: "claude", name: "Claude Code", cost: 0.123, calls: 2, unpriced: 0 }] },
};
// the window's own read of today (pagehead.js), which the live line reuses
// in a wide window instead of asking the requests page again
const PAGE_TODAY = {
  agents: [{ name: "Claude Code", calls: 2, input: 10, output: 5, cost: 0.123, unpriced: 0 }],
};
const state = () => ({
  agents: [
    agent("claude", "Claude Code", true, LIVE),
    agent("codex", "Codex", true, {
      ...LIVE, provider: "Other", model: "m2",
      rerouted: { from: "Claude (me@example.com)", why: "usage limit reached: every account is spent" },
    }),
    agent("pi", "Pi", true, undefined),
    agent("gemini", "Gemini CLI", false, undefined),
  ],
  profiles: [],
});

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ ...state(), settings: { lang, theme: "light" } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage/requests") return json(TODAY);
    if (url.pathname === "/api/usage") return json(PAGE_TODAY);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/settings") return json({ lang, theme: "light" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const row = (id) => `.row.agent[data-id="${id}"]`;
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: a connected agent says who its last request went to`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      const rest = server(lang);
      let lightReads = 0; // the requests page's reads: none in a wide window
      await page.route("**/*", (route) => {
        if (new URL(route.request().url()).pathname === "/api/usage/requests") lightReads++;
        return rest(route);
      });
      await page.goto("http://owl.test/?view=agents");
      // the wide window reuses pagehead's own read of today: its line has
      // the figure, and the requests page is not asked a second time
      await page.locator(`${row("claude")} .ag-today`).filter({ hasText: "$0.123" }).waitFor();
      await page.locator(`${row("claude")} .ag-live`).filter({ hasText: "Vendor/m1" }).waitFor();
      assert.equal(lightReads, 0, "the wide window asked the requests page beside pagehead's read");
      const tr = (k) => page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang]?.[k]), [k, lang]);
      const want = async (k, vars) => {
        let s = await tr(k);
        for (const [n, v] of Object.entries(vars || {})) s = s.split(`{${n}}`).join(v);
        return s;
      };
      const line = async (id) => page.locator(`${row(id)} .ag-live`).textContent();
      // the provider and model, the account, and today's figure, in the row's room
      const claude = await line("claude");
      assert.ok(claude.includes(await want("Via {model}", { model: "Vendor/m1" })), claude);
      assert.ok(claude.includes("me@example.com"), claude);
      // pagehead's line on the card has the figure; the live line leaves it
      // out rather than say it twice
      assert.ok(!claude.includes(await want("{cost} today", { cost: "$0.123" })), "the live line says today's figure twice: " + claude);
      assert.ok(!claude.includes((await tr("Rerouted from {from}")).split("{from}")[0].trim()), "a straight request says nothing of rerouting");
      // a rerouted one: where from, and why
      const codex = await line("codex");
      assert.ok(codex.includes(await want("Via {model}", { model: "Other/m2" })), codex);
      assert.ok(codex.includes(await want("Rerouted from {from}: {why}", { from: "Claude (me@example.com)", why: "usage limit reached: every account is spent" })), codex);
      // when it was made is the line's title, never taking the row's room
      const title = await page.locator(`${row("codex")} .ag-live`).getAttribute("title");
      assert.ok(title.startsWith((await tr("Last request {when}")).split("{when}")[0].trim()), title);
      assert.ok(title.includes(codex), title);
      // nothing went through owl today: said, not $0 and not blank
      assert.equal((await line("pi")).trim(), (await tr("No requests through owl today")).trim());
      // not connected through owl: no line at all
      assert.equal(await page.locator(`${row("gemini")} .ag-live`).count(), 0);
      // every line translated
      for (const k of ["No requests through owl today", "Last request {when}", "Via {model}", "Rerouted from {from}: {why}", "Rerouted from {from}"]) assert.ok(await tr(k), `${lang} has ${k}`);
      assert.deepEqual(errors, []);
      const wide = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
      assert.ok(wide <= 0, `the page scrolls sideways by ${wide}px`);

      // the narrow window: the line is cut, its words whole in the title; the
      // card keeps pagehead's own today line, so the figure is not read again
      await page.setViewportSize({ width: 420, height: 760 });
      assert.ok((await line("claude")).includes("Vendor/m1"));
      assert.ok((await page.locator(row("claude")).textContent()).includes("$0.123"));
      assert.ok((await page.locator(`${row("claude")} .ag-live`).getAttribute("title")).includes(await line("claude")));
      const narrow = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
      assert.ok(narrow <= 0, `the narrow page scrolls sideways by ${narrow}px`);
    });

    test(`${engine}: a narrow window reads today's spend once, from the requests page`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 420, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      const rest = server("en");
      let lightReads = 0;
      await page.route("**/*", (route) => {
        if (new URL(route.request().url()).pathname === "/api/usage/requests") lightReads++;
        return rest(route);
      });
      await page.goto("http://owl.test/?view=agents");
      await page.locator(`${row("claude")} .ag-live`).filter({ hasText: "$0.123" }).waitFor();
      // no pagehead line here: this figure is the light read's, once
      assert.equal(await page.locator(`${row("claude")} .ag-today`).count(), 0, "a narrow window has pagehead's line");
      assert.equal(lightReads, 1, "the narrow window asked the requests page " + lightReads + " times");
      const line = await page.locator(`${row("claude")} .ag-live`).textContent();
      assert.ok(line.includes("Vendor/m1") && line.includes("$0.123"), line);
      assert.deepEqual(errors, []);
    });

    test(`${engine}: a wide today read sent before midnight is not drawn as the new day's`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: "reduce", timezoneId: "Asia/Shanghai" });
      const page = await context.newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.clock.install({ time: new Date("2026-11-20T23:59:50+08:00") });
      let release, arrived;
      const held = new Promise((r) => (release = r));
      const sent = new Promise((r) => (arrived = r));
      let asked = 0;
      const rest = server("en");
      await page.route("**/*", async (route) => {
        if (new URL(route.request().url()).pathname !== "/api/usage") return rest(route);
        asked++;
        if (asked === 1) {
          arrived();
          await held;
          // sent at 23:59:50, answered after midnight: its data is the 20th's
          return route.fulfill({ json: { since: "2026-11-20T00:00:00+08:00", agents: [{ name: "Claude Code", calls: 2, input: 10, output: 5, cost: 0.123 }] } });
        }
        return route.fulfill({ json: { since: "2026-11-21T00:00:00+08:00", agents: [{ name: "Claude Code", calls: 1, input: 10, output: 5, cost: 0.5 }] } });
      });
      await page.goto("http://owl.test/?view=agents");
      await page.clock.runFor(500); // the page's own drawing, still 23:59:50
      await sent; // the request went out before midnight
      await page.clock.fastForward("00:00:20");
      release(); // and its answer lands after it
      await page.waitForTimeout(300);
      await page.clock.runFor(100); // its handlers' frames
      assert.equal(asked, 1, "the answer of the 20th was followed by another ask inside the minute");
      assert.equal(await page.locator(`${row("claude")} .ag-today`).count(), 0, "yesterday's call was drawn as today's");
      const card = await page.locator(row("claude")).textContent();
      assert.ok(!card.includes("$0.123"), "yesterday's spend on today's card: " + card);
      assert.deepEqual(errors, []);
    });

    test(`${engine}: a narrow spend read sent before midnight is not drawn as the new day's`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const context = await browser.newContext({ viewport: { width: 420, height: 760 }, reducedMotion: "reduce", timezoneId: "Asia/Shanghai" });
      const page = await context.newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.clock.install({ time: new Date("2026-11-20T23:59:50+08:00") });
      let release, arrived;
      const held = new Promise((r) => (release = r));
      const sent = new Promise((r) => (arrived = r));
      let asked = 0;
      const rest = server("en");
      await page.route("**/*", async (route) => {
        if (new URL(route.request().url()).pathname !== "/api/usage/requests") return rest(route);
        asked++;
        if (asked === 1) {
          arrived();
          await held;
          // its first bucket is the 20th's own window, though it landed after midnight
          return route.fulfill({ json: { ...TODAY, series: [{ time: "2026-11-20T00:00:00+08:00" }] } });
        }
        return route.fulfill({
          json: {
            ...TODAY, cost: 0.5,
            by: { agent: [{ id: "claude", name: "Claude Code", cost: 0.5, calls: 1, unpriced: 0 }] },
            series: [{ time: "2026-11-21T00:00:00+08:00" }],
          },
        });
      });
      await page.goto("http://owl.test/?view=agents");
      await page.clock.runFor(500); // the page's own drawing, still 23:59:50
      await page.locator(`${row("claude")} .ag-live`).waitFor();
      await sent;
      await page.clock.fastForward("00:00:20");
      release();
      await page.waitForTimeout(300);
      await page.clock.runFor(100); // its handlers' frames
      // the answer of the 20th is not today's, so the line has no figure
      const line = await page.locator(`${row("claude")} .ag-live`).textContent();
      assert.ok(!line.includes("$"), "yesterday's spend under today's name: " + line);
      // a return within the minute is throttled; after it, asked again, and
      // that answer is the 21st's
      await page.evaluate(() => window.agentLive.readSpend(true));
      await page.waitForTimeout(100);
      assert.equal(asked, 1, "a return within a minute asked again");
      await page.clock.fastForward("00:01:01");
      await page.evaluate(() => window.agentLive.readSpend(true));
      await page.locator(`${row("claude")} .ag-live`).filter({ hasText: "$0.500" }).waitFor();
      assert.equal(asked, 2, "the second ask counted " + asked);
      assert.deepEqual(errors, []);
    });

    test(`${engine}: no spend read off the Agents view or with nothing connected`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 420, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      let reads = 0;
      const count = (route) => {
        if (new URL(route.request().url()).pathname === "/api/usage/requests") reads++;
        return false;
      };
      const rest = server("en");
      await page.route("**/*", (route) => (count(route) ? route : rest(route)));
      await page.goto("http://owl.test/?view=providers");
      await page.waitForTimeout(500);
      assert.equal(reads, 0, "a Providers view asked for the agents' spend");
      // the Agents view with no connected agent has no line to fill
      const rest2 = server("en");
      await page.route("**/*", (route) => {
        count(route);
        if (new URL(route.request().url()).pathname === "/api/state") {
          return route.fulfill({ json: { ...state(), agents: state().agents.map((a) => ({ ...a, wired: false })), settings: { lang: "en", theme: "light" } } });
        }
        return rest2(route);
      });
      await page.goto("http://owl.test/?view=agents");
      await page.waitForTimeout(500);
      assert.equal(reads, 0, "an Agents view with nothing connected asked for spend");
    });

    test(`${engine}: pagehead's today line keeps the figure, the live line says it once`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      const rest = server("en");
      await page.route("**/*", (route) => {
        const url = new URL(route.request().url());
        // the window's own read of today (pagehead.js), which the live line
        // reuses instead of reading the requests page
        if (url.pathname === "/api/usage") return route.fulfill({ json: { agents: [{ name: "Claude Code", calls: 2, input: 10, output: 5, cost: 0.123 }] } });
        return rest(route);
      });
      await page.goto("http://owl.test/?view=agents");
      const card = page.locator(row("claude"));
      await card.locator(".ag-today").waitFor();
      await card.locator(".ag-live").filter({ hasText: "Vendor/m1" }).waitFor();
      const today = await card.locator(".ag-today").textContent();
      assert.ok(today.includes("$0.123"), "pagehead's line says today's spend: " + today);
      const live = await card.locator(".ag-live").textContent();
      assert.ok(!live.includes("$0.123"), "the live line says it a second time: " + live);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: the panel's overview and Agents tab say the same`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 500, height: 900 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://owl.test/?mode=panel");
      await page.locator('#pHome .ph-agent[data-key="a:claude"] .ph-agent-live').waitFor();
      const tr = (k) => page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang]?.[k]), [k, lang]);
      const overview = await page.locator('#pHome .ph-agent[data-key="a:claude"] .ph-agent-live').textContent();
      assert.ok(overview.includes("Vendor/m1") && overview.includes("me@example.com"), overview);
      assert.ok(!overview.includes("$0.123"), "the overview's cost stays its own figure at the right: " + overview);
      const noRec = await page.locator('#pHome .ph-agent[data-key="a:pi"] .ph-agent-live').textContent();
      assert.equal(noRec.trim(), (await tr("No requests through owl today")).trim());
      assert.equal(await page.locator('#pHome .ph-agent[data-key="a:gemini"] .ph-agent-live').count(), 0);
      // the panel's Agents tab: the same line on the rows
      await page.locator('#pHome [data-open="agents"]').click();
      await page.locator(`${row("claude")} .ag-live`).filter({ hasText: "$0.123" }).waitFor();
      assert.ok((await page.locator(`${row("claude")} .ag-live`).textContent()).includes("Vendor/m1"));
      assert.ok((await page.locator(`${row("codex")} .ag-live`).textContent()).includes("Other/m2"));
      const wide = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
      assert.ok(wide <= 0, `the panel scrolls sideways by ${wide}px`);
      assert.deepEqual(errors, []);
    });
  }
}