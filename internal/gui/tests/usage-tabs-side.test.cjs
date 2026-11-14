// Run with Node's test runner and Playwright on the module path; see README.md.
// Beside the rail a page's tabs are one thing everywhere (the owner, 11-14:
// "让你重构界面，不只是重构侧边栏，里面的细节也要重构"): the Library's,
// Plugins' and Settings' are words with a line under the one picked, on a
// hairline across the head. Usage's four tabs (Overview, Requests, Sessions,
// Context) were pills beside its periods, a second kind of tab on the same
// rail. They are the same underlined words now, and the period strips
// (Today, 7 days, 30 days, All, Custom; and Sessions' and Context's own)
// keep their pills.
//
// The head wraps the sum under the tabs rather than hiding a tab behind a
// sideways scroll (app.js's tight), so every tab is whole at every width.
// Read at 1100, 900, 760, 650 and 560: the tabs on one row, the picked one
// underlined, the hairline under the head, nothing over a filter or the
// tools, nothing outside the head or the window, every tab whole, and the
// filter's options still pills. Chromium and WebKit, every language, light
// and dark. OWL_GUI_ASSETS points at older assets: the underlined tab
// assertions fail there (the tabs were pills).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.OWL_GUI_ASSETS || path.join(__dirname, "../assets"));
const iso = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

// a page with rows, a day's points and a year's heatmap, as the Requests
// tab reads; the head itself is what this file measures
const now = Date.now();
const R = (i, model, out) => ({ route_id: 900 + i, t: new Date(now - (i + 1) * 60e3).toISOString(), agent: "codex", agentName: "Codex", icon: "codex-color",
  provider: "openai", providerName: "openai", host: "chatgpt.com", ep: "/v1/responses", req: model, model, served: model, effort: "medium",
  in: 4000, out, cache_read: 110000, cache_write: 0, ms: 11000, ttft_ms: 6400, status: 200, rid: "r" + i, cost: 0.00175, priced: true });
const ROWS = [R(0, "gpt-6.1-sol", 240), R(1, "gpt-6.1-sol", 145), R(2, "codex-auto-review", 134), R(3, "gpt-6.1-sol", 93)];
const M = (id, calls, out) => ({ id, calls, input: 1, output: out, cache_read: 0, cache_write: 0, cost: 0.003, timed: calls, ttft_ms: 9000, decode_ms: 6000, decode_out: out });
const part = (x) => ({ calls: x.calls, tokens: x.output, cost: x.cost, timed: x.timed, ttft_ms: x.ttft_ms, decode_ms: x.decode_ms, decode_out: x.decode_out });
function HOURS() {
  const m = new Date(); m.setHours(0, 0, 0, 0);
  const upto = new Date().getHours();
  return Array.from({ length: 24 }, (_, h) => {
    const on = h <= upto && h >= 6, k = on ? (h % 5) + 1 : 0;
    return { label: String(h).padStart(2, "0"), time: new Date(m.getTime() + h * 3600e3).toISOString(), calls: k * 3, input: k * 100, output: k * 500, cache_read: k * 90000, cache_write: 0, cost: k * 0.01,
      timed: k, ttft_ms: k * 900, decode_ms: k * 4000, decode_out: k * 400,
      by: { model: on ? { "gpt-6.1-sol": part(M("gpt-6.1-sol", k * 2, k * 300)), "codex-auto-review": part(M("codex-auto-review", k, k * 200)) } : {} } };
  });
}
const LEDGER = (period) => ({
  period, rows: ROWS, offset: 0, total: ROWS.length, calls: ROWS.length, errors: 0, input: 2, output: 612, cache_read: 0, cache_write: 0,
  cost: 0.007, unpriced: 0, timed: 4, ttft_ms: 18000, decode_ms: 12000, decode_out: 612,
  by: { model: [M("gpt-6.1-sol", 3, 478), M("codex-auto-review", 1, 134)] }, bucket: "hour", series: HOURS(),
  agents: [{ id: "codex", name: "Codex", icon: "codex-color" }],
});
function HEAT() {
  const to = new Date(), from = new Date(); from.setDate(from.getDate() - 364);
  const days = [];
  for (let i = 0; i < 40; i++) { const d = new Date(); d.setDate(d.getDate() - i); days.push({ date: iso(d), calls: 3 + i, tokens: 1000 * (i + 1), cost: 0.01 * i }); }
  return { from: iso(from), to: iso(to), days };
}

function serve(lang, theme) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme } });
    if (url.pathname === "/api/usage/requests") return json(LEDGER(url.searchParams.get("period")));
    if (url.pathname === "/api/usage/heatmap") return json(HEAT());
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage") return json({ calls: 0, errors: 0, input: 0, output: 0, reasoning: 0, unpriced: 0, cost: 0, bucket: "day", series: [], agents: [], models: [], path: "" });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], models: [], gateway: { running: false } });
    if (url.pathname === "/api/sessions") return json({ sessions: [], dirs: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// What the head shows now: the tabs and the picked one's ink, the head's
// hairline, every visible piece's box, and whether anything scrolls.
const HEAD = () => {
  const r = (e) => { if (!e || !e.offsetWidth) return null; const b = e.getBoundingClientRect(); return { top: b.top, bottom: b.bottom, left: b.left, right: b.right, w: b.width, h: b.height }; };
  const head = document.querySelector(".usage-head"), tabs = document.querySelector("#usageTab");
  const opts = [...tabs.querySelectorAll(".opt")];
  const on = tabs.querySelector(".opt.on");
  const pieces = [...head.children].filter((e) => e.offsetWidth && !e.classList.contains("grow") && e.id !== "usageTab");
  const filter = ["#period", "#sessRange", "#ctxRange"].map((q) => document.querySelector(q)).find((e) => e && e.offsetWidth);
  const filterOpt = filter && filter.querySelector(".opt");
  const hs = getComputedStyle(head), ts = getComputedStyle(tabs), os = on && getComputedStyle(on), fs = filterOpt && getComputedStyle(filterOpt);
  return {
    side: document.body.classList.contains("side-nav"),
    head: r(head), tabs: r(tabs), tabScrolls: tabs.scrollWidth > tabs.clientWidth + 1,
    headShadow: hs.boxShadow, headHeight: head.getBoundingClientRect().height, tabsHeight: tabs.getBoundingClientRect().height,
    opts: opts.map((o) => { const cs = getComputedStyle(o); return { ...r(o), text: o.textContent.trim(), radius: cs.borderRadius, shadow: cs.boxShadow, bg: cs.backgroundColor, on: o.classList.contains("on") }; }),
    onShadow: os && os.boxShadow, onRadius: os && os.borderRadius, onBg: os && os.backgroundColor,
    filter: r(filter), filterOpt: r(filterOpt), filterRadius: fs && fs.borderRadius, filterBg: fs && fs.backgroundColor,
    pieces: pieces.map((e) => ({ id: e.id || e.className, ...r(e) })),
    refresh: r(document.querySelector("#usageRefresh")), cost: r(document.querySelector("#usageCost")),
    // The page, its body, the view and the head. The ledger table's own
    // pinned scrollbar (#ledHScroll, led-hscroll) is left out on purpose:
    // its 86px of sideways reach at 760px is the Requests table's designed
    // scroll, and it is there at the same place on b401678b3 and bab282e02
    // (the same 736/650 at y 858), not a page that runs sideways.
    wide: [document.documentElement, document.body, document.querySelector("#view-usage"), head].filter((e) => e && e.scrollWidth > e.clientWidth + 1).map((e) => `${e.id || e.className} ${e.scrollWidth}/${e.clientWidth}`),
  };
};

// one tab's head is the page's underlined strip, whole on one row, over the
// hairline, with nothing over a filter or a tool and nothing outside it
function check(h, at) {
  assert.equal(h.side, true, `${at}: the rail is shown`);
  assert.equal(h.opts.length, 4, `${at}: four tabs`);
  // the picked one is a word with a line under it, not a pill: no fill, no
  // corner, the ink line at the strip's foot (the pills had a 999px corner
  // and a fill, and no line)
  assert.equal(h.onRadius, "0px", `${at}: a tab has no corner: ${h.onRadius}`);
  assert.equal(h.onBg, "rgba(0, 0, 0, 0)", `${at}: a tab has no fill: ${h.onBg}`);
  assert.match(h.onShadow, /inset/, `${at}: the picked tab has an inset line: ${h.onShadow}`);
  assert.match(h.onShadow, /-2px/, `${at}: the line is under it: ${h.onShadow}`);
  assert.doesNotMatch(h.onShadow, /rgba\(0, 0, 0, 0\)/, `${at}: the picked tab's line is its ink: ${h.onShadow}`);
  for (const o of h.opts.filter((x) => !x.on)) assert.ok(o.shadow === "none" || /rgba\(0, 0, 0, 0\)/.test(o.shadow), `${at}: only the picked tab is underlined: ${o.text}`);
  // the head keeps the hairline across its foot, as the other pages' do
  assert.match(h.headShadow, /inset/, `${at}: the head has a foot line: ${h.headShadow}`);
  assert.ok(h.headHeight >= 33, `${at}: the head is a page head's height: ${h.headHeight}`);
  assert.ok(h.tabsHeight >= 33, `${at}: the tab strip is drawn a page tab's height: ${h.tabsHeight}`);
  // one row: every tab's middle on the strip's middle, no wrap
  for (const o of h.opts) assert.ok(Math.abs((o.top + o.bottom) / 2 - (h.tabs.top + h.tabs.bottom) / 2) <= 1, `${at}: a tab is off the strip's row: ${JSON.stringify(o)}`);
  // the strip never hides a tab behind its own scroll at these widths
  assert.equal(h.tabScrolls, false, `${at}: every tab is whole`);
  // the period strip is still pills
  assert.ok(h.filter, `${at}: the tab's filter shows`);
  assert.ok(h.filterRadius === "999px", `${at}: the filter is pills: ${h.filterRadius}`);
  assert.notEqual(h.filterBg, "rgba(0, 0, 0, 0)", `${at}: a pill has its fill`);
  // nothing over anything else, nothing outside the head, nothing wider
  // than the window: the tabs never press a filter or the tools
  for (let i = 0; i < h.pieces.length; i++) for (let j = i + 1; j < h.pieces.length; j++) {
    const a = h.pieces[i], b = h.pieces[j];
    const over = Math.min(a.right, b.right) - Math.max(a.left, b.left);
    assert.ok(over <= 0.5 || Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) <= 0.5, `${at}: ${a.id} lies over ${b.id}: ${JSON.stringify([a, b])}`);
  }
  for (const p of h.pieces) {
    assert.ok(p.left >= h.head.left - 0.5 && p.right <= h.head.right + 0.5, `${at}: ${p.id} runs past the head: ${JSON.stringify([p, h.head])}`);
    assert.ok(p.top >= h.head.top - 0.5 && p.bottom <= h.head.bottom + 0.5, `${at}: ${p.id} runs under the head: ${JSON.stringify([p, h.head])}`);
  }
  assert.ok(h.tabs.left >= h.head.left - 0.5 && h.tabs.right <= h.head.right + 0.5, `${at}: the tabs run past the head`);
  assert.deepEqual(h.wide, [], `${at}: something scrolls sideways`);
}

const WIDTHS = [1100, 900, 760, 650, 560];
const TABS = ["usage", "requests", "sessions", "context"]; // the strip's order
const WAIT = {
  requests: () => !document.querySelector("#period").hidden && !document.querySelector("#ledgerPane").hidden,
  sessions: () => !document.querySelector("#sessRange").hidden && !document.querySelector("#sessionsPane").hidden,
  context: () => !document.querySelector("#ctxRange").hidden && !document.querySelector("#contextPane").hidden,
  usage: () => !document.querySelector("#period").hidden && !document.querySelector("#usagePane").hidden,
};

async function head(page, at) {
  const h = await page.evaluate(HEAD);
  check(h, at);
}

// the head's fit settles, rather than the tight class flipping between
// frames (the tab strip and the sum trade room): one shape over a dozen
// frames
async function steady(page, at) {
  const seen = await page.evaluate(() => new Promise((res) => {
    const shape = () => {
      const h = document.querySelector(".usage-head").getBoundingClientRect();
      const t = document.querySelector("#usageTab").getBoundingClientRect();
      return JSON.stringify([Math.round(h.top), Math.round(h.height), Math.round(t.height),
        [...document.querySelectorAll("#usageTab .opt")].map((o) => Math.round(o.getBoundingClientRect().top))]);
    };
    const out = new Set();
    let n = 0;
    const tick = () => { out.add(shape()); if (++n < 12) requestAnimationFrame(tick); else res([...out]); };
    requestAnimationFrame(tick);
  }));
  assert.equal(seen.length, 1, `${at}: the head shakes over its fit: ${seen.join(" | ")}`);
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: Usage's tabs are the page's underlined words beside the rail`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of WIDTHS) {
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.addInitScript(() => { localStorage.setItem("owl.navLayout", "side"); localStorage.setItem("owl.usageTab", "requests"); });
        await page.route("**/*", serve(lang, "light"));
        await page.goto("http://owl.test/?view=usage");
        await page.waitForFunction(() => document.querySelectorAll("#usageTab .opt").length === 4 && document.querySelector(".usage-head").offsetWidth);
        const at = `${width}px`;
        await steady(page, at);
        await head(page, at);
        // every tab's own filter, the same head and the same rule
        for (const tab of TABS.slice(1)) {
          await page.locator("#usageTab .opt").nth(TABS.indexOf(tab)).click();
          await page.waitForFunction(WAIT[tab]);
          await page.waitForTimeout(80);
          await head(page, `${at} ${tab}`);
        }
        assert.deepEqual(errors, [], `${at}: page errors`);
        await context.close();
      }
    });
  }

  test(`${engine}: Usage's underlined head holds in the dark too`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) for (const width of [1100, 760, 560]) {
      const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce", colorScheme: "dark" });
      const page = await context.newPage();
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.addInitScript(() => { localStorage.setItem("owl.navLayout", "side"); localStorage.setItem("owl.usageTab", "requests"); });
      await page.route("**/*", serve(lang, "dark"));
      await page.goto("http://owl.test/?view=usage");
      await page.waitForFunction(() => document.querySelectorAll("#usageTab .opt").length === 4 && document.querySelector(".usage-head").offsetWidth);
      await head(page, `${width}px dark ${lang}`);
      assert.deepEqual(errors, [], `${width}px dark ${lang}: page errors`);
      await context.close();
    }
  });
}