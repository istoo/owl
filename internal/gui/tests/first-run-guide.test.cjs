// Run with Node's test runner and Playwright on the module path; see README.md.
// A first launch (no saved language, no model source): the Get started card
// is the one place the first action belongs. The agent rows say their state
// and carry no Add a provider of their own while the card is up; put the
// card away and their way in comes back, so nobody is left without one.
// The first step also offers the configurations this computer's own agents
// already use, found by the server, and opens the import dialog with them
// for a preview and a confirm. The page follows the system's language when
// none was ever saved. Both engines, a wide and a narrow window, every
// language; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LOCALES = { en: "en-US", zh: "zh-CN", "zh-TW": "zh-TW", ja: "ja-JP", de: "de-DE" };
const TAGS = { en: "en", zh: "zh-CN", "zh-TW": "zh-TW", ja: "ja-JP", de: "de-DE" };

function world() {
  return { hidden: false, guides: 0 };
}

// no model source: owl has none for the agents to list
function agents() {
  const field = [{ key: "model", label: "model", value: "", options: [] }];
  return [
    { id: "codex", name: "Codex", icon: "codex-color", path: "/fixture/codex", fields: field },
    { id: "claude", name: "Claude Code", icon: "claudecode-color", path: "/fixture/claude", fields: field },
    { id: "opencode", name: "OpenCode", icon: "opencode", path: "/fixture/opencode", fields: field },
    { id: "pi", name: "Pi", icon: "pi", path: "/fixture/pi", fields: field },
    { id: "cursor", name: "Cursor CLI", icon: "cursor", path: "/fixture/cursor", fields: [] },
  ];
}

const DISCOVERY = [{ fingerprint: "f1", source: "Claude Code" }];
const SOURCES = [{
  id: "claude-code", name: "Claude Code", found: true, path: "/home/coder/.claude/settings.json",
  items: [{ ref: "relay", fingerprint: "f1", provider: { name: "Relay", chat: "https://relay.example/v1", key: "sk-…abcd", models: ["m1"] } }],
}];

function server(lang, w, posts) {
  const settings = () => ({ lang: "system", theme: "light", guideHidden: w.hidden || undefined });
  const state = () => ({ agents: agents(), profiles: [], served: 0, settings: settings() });
  return async (route) => {
    const req = route.request(), url = new URL(req.url()), p = url.pathname;
    const json = (d) => route.fulfill({ json: d });
    if (p === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"system",theme:"light",web:false};` });
    if (p === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (p.startsWith("/api/")) posts.push(req.method() + " " + p);
    if (p === "/api/state") return json(state());
    if (p === "/api/settings") return json(settings());
    if (p === "/api/settings/guide") {
      w.hidden = !!req.postDataJSON().hidden;
      w.guides++;
      return json(settings());
    }
    if (p === "/api/importapps/discovery") return json(DISCOVERY);
    if (p === "/api/importapps") return json(SOURCES);
    if (p === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (p === "/api/groups") return json({ groups: [], models: [], pools: [], deciders: [], templates: [] });
    if (p === "/api/usage/quotas") return json([]);
    if (p === "/api/plugins") return json({ plugins: [] });
    if (p === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (p.startsWith("/api/")) return json({});
    const file = path.join(assets, p === "/" ? "index.html" : p);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function open(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const w = world(), posts = [];
  const context = await browser.newContext({ viewport: { width, height: 820 }, locale: LOCALES[lang], reducedMotion: "reduce" });
  const page = await context.newPage();
  page.setDefaultTimeout(8000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, w, posts));
  await page.goto("http://owl.test/?view=agents");
  await page.locator("#agentsLead .guide").waitFor();
  return { page, errors, w, posts, context };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: a first launch follows the system, one action in the guide, rows only say their state`, async (t) => {
      const { page, errors, w, posts, context } = await open(t, engine, lang, 1280);
      const tr = (s, v) => page.evaluate(([s, v]) => t(s, v), [s, v]);

      // no language was ever saved: the page reads in the system's own
      assert.equal(await page.evaluate(() => document.documentElement.lang), TAGS[lang]);
      assert.equal(await page.locator(".guide-h").textContent(), await tr("Get started"));
      if (lang !== "en") assert.notEqual(await page.locator(".guide-h").textContent(), "Get started");

      // the first action is in the card, once: the rows have none
      assert.equal(await page.locator(".ag-add").count(), 0, "no Add a provider on the agent rows");
      assert.equal(await page.locator('.guide-step[data-step="source"] .guide-go').count(), 1, "the card's own, once");
      for (const id of ["codex", "claude", "opencode", "pi"]) {
        const said = await page.locator(`.row.agent[data-id="${id}"] .ag-st-t`).textContent();
        assert.ok(said.trim(), `the ${id} row still says why`);
      }

      // this computer's own agents already use a relay: offered here, in the card
      const local = page.locator(".guide-local");
      await local.waitFor();
      assert.equal(await local.textContent(), await tr("Import local configurations ({n})…", { n: 1 }));
      assert.match(await local.getAttribute("title"), /Claude Code/);
      await local.click();

      // the window's own import dialog, as a preview before anything is saved
      await page.locator("#view-providers").waitFor({ state: "visible" });
      const dialog = page.locator(".editor.importapps");
      await dialog.waitFor({ state: "visible" });
      // the dialog draws its sources once they are in
      await page.waitForFunction(() => document.querySelector(".editor.importapps")?.textContent.includes("relay.example"));
      assert.match(await dialog.textContent(), /Relay/);
      assert.match(await dialog.textContent(), /relay\.example/);
      assert.match(await dialog.textContent(), /sk-…abcd/, "the key is shown as the server masked it");
      assert.ok(posts.includes("GET /api/importapps"), "the preview asks for the sources: " + posts.join(" "));
      assert.ok(!posts.includes("POST /api/importapps"), "nothing is saved before the confirm");
      assert.deepEqual(errors, []);

      // put the card away: every row gets its way in back, so none is a dead end
      await page.goto("http://owl.test/?view=agents");
      await page.locator("#agentsLead .guide").waitFor();
      await page.locator(".guide-x").click();
      await page.locator(".guide").waitFor({ state: "detached" });
      assert.equal(w.hidden, true);
      assert.ok(w.guides >= 1);
      assert.equal(await page.locator(".ag-add").count(), 4, "the rows' own Add a provider comes back");
      assert.equal(await page.locator(".guide-go").count(), 0);
      assert.deepEqual(errors, []);
      await context.close();
    });

    test(`${engine} ${lang}: the first screen at 420px keeps the one action and the import entry`, async (t) => {
      const { page, errors, context } = await open(t, engine, lang, 420);
      assert.equal(await page.locator(".ag-add").count(), 0);
      assert.equal(await page.locator('.guide-step[data-step="source"] .guide-go').count(), 1);
      const local = page.locator(".guide-local");
      await local.waitFor();
      assert.equal(await local.isVisible(), true);
      const bad = await page.evaluate(() => {
        const v = document.querySelector("#view-agents");
        return v.scrollWidth > v.clientWidth + 1;
      });
      assert.equal(bad, false, "nothing runs sideways");
      assert.deepEqual(errors, []);
      await context.close();
    });
  }
}