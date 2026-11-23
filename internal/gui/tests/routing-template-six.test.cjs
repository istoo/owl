// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page offers six templates (provider.TemplateKinds) and the
// page named three: after the backend sync (11-20) Plan first, Fastest first
// and Spread evenly came out in English in every language, with no line of
// what they do. Each of the six shows its name and its line in the reader's
// language, and "plan first" with no plan-billed provider says what it
// needs, not "Add a provider first". Chromium and WebKit, five languages,
// 1280px and 420px; the API is faked. OWL_GUI_ASSETS points at an older
// asset folder to see it fail there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.OWL_GUI_ASSETS ? path.resolve(process.env.OWL_GUI_ASSETS) : path.resolve(__dirname, "../assets");
const models = [
  { id: "a/big", name: "Big", providerName: "A", icon: "generic" },
  { id: "b/big", name: "Big", providerName: "B", icon: "generic" },
];
const info = (ids) => ids.map((id) => ({ id, ready: true }));
const grp = (id, name, members) => ({ id, name, members, routing: "order" });
const templates = [
  { kind: "smart", group: grp("smart-split", "Hard to strong, easy to cheap", ["a/big", "b/big"]) },
  { kind: "thrifty", group: grp("cheap-first", "Cheap first", ["a/big", "b/big"]) },
  { kind: "steady", group: grp("never-stuck", "Never stuck", ["a/big", "b/big"]) },
  { kind: "plan", group: grp("plan-first", "Plan first", []), why: "no provider of yours is billed by a plan: add a subscription or token plan", need: "plan" },
  { kind: "fast", group: grp("fastest-first", "Fastest first", ["a/big", "b/big"]) },
  { kind: "even", group: grp("spread-evenly", "Spread evenly", ["a/big", "b/big"]) },
];

const names = {
  en: ["Hard to strong, easy to cheap", "Cheap first", "Never stuck", "Plan first", "Fastest first", "Spread evenly"],
  zh: ["难题给强模型，简单的给便宜模型", "便宜优先", "永不卡住", "套餐优先", "最快优先", "均匀分摊"],
  "zh-TW": ["難題給強模型，簡單的給便宜模型", "便宜優先", "永不卡住", "套餐優先", "最快優先", "平均分攤"],
  ja: ["難しい作業は強いモデル、簡単な作業は安いモデルへ", "安いモデル優先", "止まらない", "プラン優先", "最速優先", "均等に分散"],
  de: ["Schweres zum starken, Leichtes zum günstigen Modell", "Günstig zuerst", "Nie blockiert", "Tarif zuerst", "Schnellster zuerst", "Gleichmäßig verteilen"],
};
// the line under the three the backend sync added, and what "plan first" needs
const lines = {
  en: ["The strongest model of each provider, those billed by a plan first: the allowance you already paid for is spent before a key",
    "The strongest model of each provider, the one that has answered fastest lately first",
    "The strongest model alone, from every provider that serves it, taken in turn over all their keys and accounts",
    "Needs a provider billed by a plan: add a subscription or token plan"],
  zh: ["每家供应商最强的模型，按套餐计费的排前面：先用掉已付费的额度，最后才用密钥", "每家供应商最强的模型，最近回答最快的排最前",
    "只用最强的那一个模型，来自所有提供它的供应商，在它们的全部密钥和账号间轮流", "需要按套餐计费的供应商：请添加订阅或 Token 套餐"],
  "zh-TW": ["每家供應商最強的模型，按套餐計費的排前面：先用掉已付費的額度，最後才用金鑰", "每家供應商最強的模型，最近回答最快的排最前",
    "只用最強的那一個模型，來自所有提供它的供應商，在它們的全部金鑰和帳號間輪流", "需要按套餐計費的供應商：請新增訂閱或 Token 套餐"],
  ja: ["各プロバイダーの最強モデル。プラン課金のものが先で、支払い済みの枠を使い切ってからキーを使います", "各プロバイダーの最強モデル。最近いちばん速く答えたものが先頭です",
    "最強モデルだけを、それを提供するすべてのプロバイダーのキーとアカウントで順番に使います", "プラン課金のプロバイダーが必要です：サブスクリプションかトークンプランを追加してください"],
  de: ["Das stärkste Modell jedes Anbieters, per Tarif abgerechnete zuerst: das bereits bezahlte Kontingent wird vor einem Key verbraucht",
    "Das stärkste Modell jedes Anbieters, der zuletzt am schnellsten antwortende zuerst",
    "Nur das stärkste Modell, von jedem Anbieter, der es anbietet, reihum über alle Keys und Konten",
    "Braucht einen per Tarif abgerechneten Anbieter: füge ein Abo oder einen Token-Tarif hinzu"],
};
function serve(lang, view) {
  const state = {
    agents: [1, 2, 3, 4].map((i) => ({ id: "ag" + i, name: "Agent " + i, icon: "generic", path: "/fixture/ag" + i, fields: [{ key: "model", label: "model", value: "", options: [] }] })),
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models, pools: [], deciders: [], found: true, groups: [], templates });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(engine, lang, width, view, t) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 1000 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
  await page.route("**/*", serve(lang));
  await page.goto("http://owl.test/?view=" + view);
  return { page, errors };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(names)) {
    for (const width of [1280, 420]) {
      test(`${engine} ${lang} ${width}px: all six routing templates say their name and what they do`, async (t) => {
        const { page, errors } = await open(engine, lang, width, "routing", t);
        const cards = page.locator(".rt-gtpl-card");
        await cards.nth(5).waitFor();
        await page.waitForTimeout(300);
        const got = await cards.evaluateAll((cs) => cs.map((c) => ({ kind: c.dataset.kind, name: c.querySelector("b").textContent.trim(), desc: c.querySelector(".desc")?.textContent.trim() || "", why: c.querySelector(".why")?.textContent.trim() || "" })));
        assert.deepEqual(got.map((g) => g.kind), ["smart", "thrifty", "steady", "plan", "fast", "even"]);
        assert.deepEqual(got.map((g) => g.name), names[lang]);
        for (const g of got) assert(g.desc.length > 0, `${g.kind} has no line saying what it does`);
        assert.deepEqual([got[3].desc, got[4].desc, got[5].desc], lines[lang].slice(0, 3));
        // a template that needs a plan says so, not "add a provider first"
        assert.equal(got[3].why, lines[lang][3]);
        if (lang !== "en") for (const g of got) assert(!/^[\x20-\x7e]+$/.test(g.name) || lang === "de", `${g.kind}'s name is still English: ${g.name}`);
        assert.deepEqual(errors, []);
      });
    }

  }
}
