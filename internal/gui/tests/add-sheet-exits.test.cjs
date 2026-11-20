// Run with Node's test runner and Playwright on the module path; see README.md.
// The add sheet's two ways out and its added rows. With a search typed, the
// catalog ends in the two ways out (Add it as a custom provider, look for a
// plugin) whatever it found: none, some or all, the plugin link carrying the
// words typed. Without one the foot keeps its Custom provider line. A plain
// preset already added is left out of the default catalog (its row in the
// list opens it) and found by a search with an Added label; a subscription,
// a region pair with one end not added, and a heading with nothing left under
// it are kept or dropped as they should. The two search boxes say what each
// searches. Every language, Chromium and WebKit; no backend, the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

const preset = (id, name, kind, added, extra = {}) => ({ id, name, icon: "openai", kind, chat: `https://api.${id}.example.com/v1`, added, ...extra });
const presets = [
  preset("anthropic", "Anthropic", "vendor", true),
  preset("openai", "OpenAI", "vendor", false),
  preset("moonshot", "Kimi", "vendor", false),
  preset("moonshot-cn", "Kimi (China)", "vendor", true, { chat: "https://api.moonshot.cn/v1" }),
  preset("zhipu", "Zhipu", "vendor", true),
  preset("zhipu-cn", "Zhipu (China)", "vendor", true, { chat: "https://open.zhipu.example.cn/v1" }),
  preset("openrouter", "OpenRouter", "relay", true),
  preset("siliconflow", "SiliconFlow", "relay", false),
  preset("ollama", "Ollama", "local", true, { noKey: true }),
];
const prov = (id, name, extra = {}) => ({ id, name, icon: "openai", preset: id, models: [], agents: [], key: { set: true, masked: "sk-…ab12" }, ...extra });
const providers = [
  prov("anthropic", "Anthropic"), prov("openrouter", "OpenRouter"), prov("ollama", "Ollama"), prov("zhipu", "Zhipu"), prov("zhipu-cn", "Zhipu (China)"),
  prov("claude", "Claude", { preset: "", account: { agent: "claude", logins: [{ user: "a" }] } }),
];

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers, presets, excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the add sheet keeps its ways out and leaves added rows to the list", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of LANGS) {
      await t.test(lang, async () => {
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://owl.test/?view=providers");
        const tr = (s) => page.evaluate((s) => t(s), s);
        await page.locator("#pvQ").waitFor();
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator(".tile").first().waitFor();
        const find = sheet.locator(".find");
        const names = () => sheet.locator(".tile .n").allTextContents();
        const kinds = () => sheet.locator(".kind b").allTextContents();

        // each box says what it searches, in the language shown
        const own = await tr("Find a preset to add…"), listed = await tr("Find a configured provider");
        assert.equal(await find.getAttribute("placeholder"), own);
        assert.equal(await page.locator("#pvQ").getAttribute("placeholder"), listed);
        if (lang !== "en") for (const w of [own, listed]) assert.notEqual(w, w === own ? "Find a preset to add…" : "Find a configured provider", "no translation");

        // no search: the plain presets already added are not drawn; a pair with one
        // end left is, and a subscription; a heading with nothing under it is not
        let ns = await names();
        for (const gone of ["Anthropic", "OpenRouter", "Ollama", "Zhipu"]) assert(!ns.includes(gone), gone + " is added: " + ns);
        for (const kept of ["OpenAI", "Kimi", "SiliconFlow", "Claude"]) assert(ns.includes(kept), kept + " kept: " + ns);
        const ks = await kinds();
        assert(!ks.includes(await tr("On this machine")), "a heading with nothing under it: " + ks);
        assert.equal(await sheet.locator(".custom-foot .custom").count(), 1, "the foot's Custom provider line");
        assert.equal(await sheet.locator(".none").count(), 0);

        const exits = async (q, said) => {
          await find.fill(q);
          const none = sheet.locator(".none");
          assert.equal(await none.count(), 1, q + ": one line of ways out");
          assert.equal(await sheet.locator(".custom-foot").count(), 0);
          assert.deepEqual(await none.locator("button.link").allTextContents(), [await tr("Add it as a custom provider"), await tr("look for a plugin")], q);
          const said0 = await page.evaluate((q) => t("Nothing called “{q}”. ", { q }), q);
          assert.equal((await none.textContent()).includes(said0.trim()), said, q + ": the nothing-found words");
        };
        await exits("zzzqq", true);
        assert.deepEqual(await names(), []);
        await exits("openai", false);
        assert.deepEqual(await names(), ["OpenAI"]);
        await exits("kimi", false);

        // an added one is found, says so, and opens what it opened
        await exits("anthropic", false);
        assert.deepEqual(await names(), ["Anthropic"]);
        const row = sheet.locator(".tile", { has: page.locator(".n", { hasText: /^Anthropic$/ }) });
        assert.equal(await row.locator(".have").count(), 1);
        assert.equal(await row.locator(".badge").textContent(), await tr("Added"));
        // a pair with both ends added says so too; one with an end left does not
        await exits("zhipu", false);
        assert.equal(await sheet.locator(".tile .badge").count(), 1);
        await exits("kimi", false);
        assert.equal(await sheet.locator(".tile .badge").count(), 0);
        // a subscription is there whatever its state
        await exits("claude", false);
        assert(await sheet.locator(".tile .n", { hasText: /^Claude$/ }).count() === 1);

        // the plugin link takes the words typed
        await find.fill("my words");
        await page.evaluate(() => { window.asked = null; window.openPlugins = (q) => { window.asked = q; }; });
        await sheet.locator(".none button.link").nth(1).click();
        assert.equal(await page.evaluate(() => window.asked), "my words");
        await sheet.locator(".none button.link").nth(0).click();
        await page.locator(".editor.new, #modal .editor").first().waitFor();

        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}
