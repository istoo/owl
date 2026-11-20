// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel's overview (panelhome.js): no icon rail, a header saying the
// gateway runs, and today's spend, the subscriptions' tightest allowance and
// the connected agents' models and costs. A card's › opens the full page the
// panel had as a tab, Escape comes back, and a menu bar cell still lands on
// its allowance. Every string is translated, and nothing runs sideways.
// Both engines, every language, light and dark; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const opt = (value, label, icon) => ({ value, label, icon, ref: "x/" + value });
const agent = (id, name, icon, wired, model) => ({
  id, name, path: "/fixture/" + id, icon, wired, version: "1.0.0",
  fields: [{ key: "model", label: "model", value: model, options: model ? [opt(model, model, icon)] : [opt("m", "m", "generic")] }],
});
const AGENTS = [
  agent("claude", "Claude Code", "claudecode-color", true, "Claude Opus 5.5"),
  agent("codex", "Codex", "codex-color", true, "gpt-6-sol"),
  agent("pi", "Pi", "pi", true, "deepseek-v4.1-flash-1017"),
  agent("gemini", "Gemini CLI", "geminicli-color", false, ""),
  agent("opencode", "OpenCode", "opencode", false, ""),
];
const PROFILES = [{ name: "home", summary: "", agents: [{ id: "codex", name: "Codex", fields: [{ key: "model", label: "model", value: "gpt-6-sol" }] }] }];
const inHours = (h) => new Date(Date.now() + h * 3600e3).toISOString();
const QUOTAS = [
  { provider: "claude-max", name: "Claude", icon: "claude-color", plan: "Max 20x", windows: [{ name: "5 hours", used: 42, resetsAt: inHours(2.3) }, { name: "7 days", used: 68, resetsAt: inHours(70) }] },
  { provider: "codex-pro", name: "Codex", icon: "codex-color", plan: "Pro", windows: [{ name: "5 hours", used: 15, resetsAt: inHours(4.1) }, { name: "7 days", used: 31, resetsAt: inHours(120) }] },
];
const MIDNIGHT = new Date();
MIDNIGHT.setHours(0, 0, 0, 0);
const CALLS = [0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 6, 4, 2, 1, 0, 0, 0, 0, 0];
const SERIES = CALLS.map((n, i) => ({
  label: String(i), time: new Date(MIDNIGHT.getTime() + i * 3600e3).toISOString(),
  calls: n, errors: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, cost: 0,
}));
// the same answer the panel's Usage detail draws: its totals, its hours and
// each agent's share (by id), so the overview and the detail can't part
const TODAY = {
  period: "today", rows: [], offset: 0, total: 41, calls: 41, errors: 3, retried: 9,
  input: 1e6, output: 4e5, cache_read: 11.8e6, cache_write: 5e5, cost: 13.7, unpriced: 0, reasoning: 0,
  bucket: "hour", series: SERIES,
  by: {
    agent: [
      { id: "claude", name: "Claude Code", calls: 20, cost: 11.7 },
      { id: "codex", name: "Codex", calls: 15, cost: 2 },
      { id: "pi", name: "Pi", calls: 6, cost: 0 },
    ],
    provider: [], model: [],
  },
  agents: [], providers: [], purposes: [],
};

function server(lang, { theme = "light", left = false, quotas = QUOTAS, featuresOff = [], asked = [] } = {}) {
  const settings = { lang, theme, tray: "panel", quotaLeft: left, currency: "usd", guideHidden: true };
  return async (route) => {
    const url = new URL(route.request().url()), p = url.pathname;
    const json = (d) => route.fulfill({ json: d });
    if (p === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:false,featuresOff:${JSON.stringify(featuresOff)}};` });
    if (p === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (p === "/api/state") return json({ agents: AGENTS, profiles: PROFILES, settings, served: 42 });
    if (p === "/api/settings") return json(settings);
    if (p === "/api/settings/quota-left") return json({ ...settings, quotaLeft: true });
    if (p === "/api/usage/quotas") return json(quotas);
    if (p === "/api/usage/requests") { asked.push(url.searchParams); return json(TODAY); }
    if (p === "/api/usage") return json({ models: [], accounts: [] });
    if (p === "/api/groups") return json({ groups: [], models: [] });
    if (p === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (p === "/api/gateway/trace") return json({ mine: true, now: new Date().toISOString(), seq: 0, totals: { requests: 412, rerouted: 9, errors: 3 }, routes: [] });
    if (p === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (p === "/api/plugins") return json({ plugins: [] });
    if (p.startsWith("/api/")) return json({});
    const file = path.join(assets, p === "/" ? "index.html" : p);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function open(t, engine, lang, options = {}) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width: 500, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(8000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, options));
  await page.goto("http://owl.test/?mode=panel");
  await page.locator("#pHome .ph-mod").first().waitFor();
  await page.waitForFunction(() => /13\.70/.test(document.querySelector("#pHome .ph-big")?.textContent || ""));
  return { page, errors };
}

const home = async (page) => {
  if (await page.locator("#pHome").isVisible()) return;
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => document.body.dataset.ptab === "home");
};
const detail = async (page, tab, link = tab) => {
  await page.locator(`#pHome [data-open="${link}"]`).first().click();
  await page.waitForFunction((t) => document.body.dataset.ptab === t, tab);
};
const noSideways = (page) => page.evaluate(() => {
  const bad = [];
  for (const e of document.querySelectorAll("#pHome .ph-head, #pHome .ph-today, #pHome .ph-q, #pHome .ph-agent, #pHome .ph-rest, #pHome .ph-route")) {
    if (e.scrollWidth > e.clientWidth + 1) bad.push((e.className || e.tagName) + ": " + e.scrollWidth + " > " + e.clientWidth);
  }
  return bad;
});
// one figure on the overview, and one in the Usage detail's totals
const fact = (page, label) => page.evaluate((label) => {
  const f = [...document.querySelectorAll("#pHome .ph-fact")].find((x) => x.querySelector("span")?.textContent.trim() === t(label));
  return f?.querySelector("b")?.textContent.trim() || "";
}, label);
const block = (page, label) => page.evaluate((label) => {
  const b = [...document.querySelectorAll("#panelUsage .pu-tot .blk")].find((x) => x.querySelector(".k")?.textContent.trim() === t(label));
  return b?.querySelector(".v")?.textContent.trim() || "";
}, label);

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: the panel opens on its overview, and every › opens its full page`, async (t) => {
      const asked = [];
      const { page, errors } = await open(t, engine, lang, { asked });

      // no icon rail; the overview is what opens
      assert.equal(await page.locator("#ptabs").count(), 0, "the panel has no icon rail");
      assert.equal(await page.evaluate(() => document.body.dataset.ptab), "home");
      assert.equal(await page.locator("#pHome > .ph-mod").count(), 3, "today, allowances and agents");
      assert(await page.locator(".ph-status .ph-dot").isVisible(), "the gateway's state is in the title bar");

      // today: the spend, its four figures, and the hours
      assert.equal((await page.locator("#pHome .ph-big").textContent()).trim(), "$13.70");
      assert.deepEqual(await page.locator("#pHome .ph-fact b").allTextContents(), ["41", "14M", "89%", "3"]);
      assert.equal(await page.locator("#pHome .ph-spark i").count(), 24, "one bar an hour");
      assert.match((await page.locator("#pHome .ph-route").textContent()).trim(), /9/, "today's reroutes");

      // the agents' today adds up to the day's spend; one with none says —
      const costs = await page.locator("#pHome .ph-agent .ph-a-cost").allTextContents();
      assert.equal(costs.length, 3, "the connected agents only");
      assert.equal(costs[2], "—");
      const sum = costs.reduce((n, s) => n + (s === "—" ? 0 : Number(s.replace(/[^0-9.]/g, ""))), 0);
      assert.equal(Math.round(sum * 100), 1370, "each agent's today adds up to the day's spend");

      // allowances: each subscription, its tightest window, the others quieter
      assert.equal(await page.locator("#pHome .ph-q").count(), 2);
      const first = page.locator("#pHome .ph-q").first();
      assert.match(await first.textContent(), /68%/);
      assert.match(await first.textContent(), /42%/);
      assert.equal(await first.locator(".ph-q-bar i").evaluate((e) => e.style.width), "68%");
      assert.deepEqual(await noSideways(page), [], "nothing runs sideways");

      // what the overview says today was, the Usage detail says the same:
      // both draw the day's own page, so no figure can part
      const seen = {
        cost: (await page.locator("#pHome .ph-big").textContent()).trim(),
        requests: await fact(page, "Requests"), tokens: await fact(page, "Tokens"), hit: await fact(page, "Cache hit rate"),
      };

      // today's › opens the request totals; Escape comes back
      await detail(page, "stats");
      assert(await page.locator("#panelUsage").isVisible());
      await page.locator("#panelUsage .pu-tot .blk").first().waitFor();
      assert.equal((await block(page, "Cost")).replace(/^≈/, ""), seen.cost, "the detail's cost is the overview's");
      assert.equal(await block(page, "Requests"), seen.requests, "the detail's requests are the overview's");
      assert.equal(await block(page, "Tokens"), seen.tokens, "the detail's tokens are the overview's");
      assert.equal(await block(page, "Cache hit rate"), seen.hit, "the detail's hit rate is the overview's");
      assert.equal(await page.locator(".ph-back").getAttribute("aria-label"), await page.evaluate(() => t("Back to overview")));
      await page.keyboard.press("Escape");
      await page.waitForFunction(() => document.body.dataset.ptab === "home");
      assert(await page.locator("#pHome").isVisible());

      // the allowances' › opens their full page
      await detail(page, "usage");
      assert(await page.locator("#panelQuota").isVisible());
      await home(page);
      assert(await page.locator("#pHome").isVisible());

      // an agent's row opens the agents page with that row open (its name
      // is the control; the model beside it opens the picker)
      await page.locator('#pHome .ph-agent[data-key="a:claude"] .ph-agent-go').click();
      await page.waitForFunction(() => document.body.dataset.ptab === "agents");
      const row = page.locator('#agents .row.agent[data-id="claude"]');
      await row.waitFor();
      assert.equal(await row.getAttribute("aria-expanded"), "true", "the agent asked for is opened");
      await home(page);
      await detail(page, "agents");
      assert(await page.locator("#agents").isVisible());
      await home(page);

      // the router's line opens its trace
      await detail(page, "routing");
      assert(await page.locator("#panelRouting").isVisible());
      await home(page);

      // a menu bar cell still lands on its allowance, and a total on its period
      await page.evaluate(() => panelQuotaFocus("claude-max"));
      await page.waitForFunction(() => document.body.dataset.ptab === "usage");
      await page.locator('#panelQuota .pq-card[data-card="claude-max"]').waitFor();
      await home(page);

      // 在定位回调执行之前返回概览，模拟迟到的一帧。
      const after = await page.evaluate(() => {
        const requestFrame = window.requestAnimationFrame;
        let locate;
        window.requestAnimationFrame = (fn) => {
          if (fn !== focusQuotaCard) return requestFrame.call(window, fn);
          locate = fn;
          return 0;
        };
        try {
          panelQuotaFocus("claude-max");
          document.querySelector(".ph-back").click();
          locate();
          return document.body.dataset.ptab;
        } finally {
          window.requestAnimationFrame = requestFrame;
        }
      });
      assert.equal(after, "home", "迟到的额度定位不能重新打开详情");
      await page.evaluate(() => panelUseFocus("7d"));
      await page.waitForFunction(() => document.body.dataset.ptab === "stats");
      await page.waitForFunction(() => [...document.querySelectorAll("#panelUsage .segs .opt")].some((b) => b.classList.contains("on") && /7/.test(b.textContent)));
      assert(asked.some((q) => q.get("period") === "7d"), "the period asked for: " + asked.map((q) => q.toString()).join(" "));
      await home(page);
      assert.deepEqual(errors, []);
      await page.context().close();
    });
  }

  test(`${engine} zh dark: the overview keeps its numbers and its pages`, async (t) => {
    const { page, errors } = await open(t, engine, "zh", { theme: "dark", left: true });
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "dark");
    assert.equal((await page.locator("#pHome .ph-big").textContent()).trim(), "$13.70");
    // Left: the same windows, counted from what remains
    const first = page.locator("#pHome .ph-q").first();
    assert.match(await first.textContent(), /32%/);
    assert.match(await first.textContent(), /58%/);
    assert.equal(await first.locator(".ph-q-bar i").evaluate((e) => e.style.width), "32%");
    await detail(page, "usage");
    assert(await page.locator("#panelQuota").isVisible());
    assert.deepEqual(errors, []);
    await page.context().close();
  });

  test(`${engine} zh: no allowances has no card, and routing off has no router line`, async (t) => {
    const { page, errors } = await open(t, engine, "zh", { quotas: [], featuresOff: ["routing"] });
    assert.equal(await page.locator("#pHome > .ph-mod").count(), 2, "today and the agents only");
    assert.equal(await page.locator('#pHome [data-open="usage"]').count(), 0);
    assert.equal(await page.locator('#pHome [data-open="routing"]').count(), 0, "routing is off");
    await detail(page, "stats");
    assert(await page.locator("#panelUsage").isVisible());
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => document.body.dataset.ptab === "home");
    assert.deepEqual(errors, []);
    await page.context().close();
  });

  test(`${engine} en: Profiles opens from the overview and the Agents detail`, async (t) => {
    const { page, errors } = await open(t, engine, "en");
    const btn = page.locator("#profBtn"), list = page.locator(".profiles.open");
    assert(await btn.isVisible(), "the overview keeps Profiles");
    await btn.click();
    await list.locator("#profiles .chip").first().waitFor();
    assert.equal(await btn.getAttribute("aria-expanded"), "true");
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.querySelector(".profiles.open"));
    assert(await page.locator("#pHome").isVisible(), "Escape closes the list, not the panel");

    await page.locator('#pHome [data-open="agents"]').click();
    await page.waitForFunction(() => document.body.dataset.ptab === "agents");
    assert(await btn.isVisible(), "the Agents detail keeps Profiles");
    await btn.click();
    await list.locator("#profiles .chip").first().waitFor();
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => !document.querySelector(".profiles.open"));

    // a detail with nothing of the reader's to pick from leaves it out, as before
    await page.locator(".ph-back").click();
    await page.waitForFunction(() => document.body.dataset.ptab === "home");
    await detail(page, "usage");
    assert(!(await btn.isVisible()), "Allowances leaves Profiles out");
    assert.deepEqual(errors, []);
    await page.context().close();
  });
}