// Run with Node's test runner and Playwright on the module path; see README.md.
// After Use Claude Owl (xMopf7RF on X, 0.1.4111: "点击启用 Claude Owl
// 还是会先把 Claude Desktop 接入，反而留下一个 Claude Owl 显示未接入"):
// - the row the switch wires is Claude Owl's, named so by the state
//   (api.go names claude-desktop "Claude Owl" while it is on), so no
//   row named Claude Desktop turns on;
// - "Claude Code · Claude Owl", the folder Claude Owl's Code tab
//   runs on, no longer reads "not connected, on its own settings": the
//   tab is on owl through Claude Owl (twinCode), and its line says
//   so, lit. Before the click it still reads not connected.
// Every language, wide and narrow, both engines. No backend: faked here,
// as api.go answers.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang) {
  const settings = { lang, theme: "light" };
  const iso = { on: false, claude: false, claudeDir: "/test/.claude-owl-desktop" };
  let wired = false;
  const state = () => ({
    agents: [{
      id: "claude-desktop", name: iso.on ? "Claude Owl" : "Claude Desktop", path: "/test/claude_desktop_config.json", icon: "claude-color", wired,
      fields: [{ key: "provider", label: "provider", value: wired ? "owl" : "", options: [{ value: "owl", label: "owl", icon: "owl" }] }],
      models: { shown: 2, listed: 2, by: [{ name: "OpenCode Go", icon: "opencode", n: 2 }], names: ["DeepSeek V4 Pro", "Exo Free"] },
      isolated: { ...iso },
    }, {
      id: "claude#owl-desktop", name: "Claude Code · Claude Owl", path: "/test/.claude-owl-desktop/settings.json", icon: "claudecode-color", wired: false,
      fields: [{ key: "provider", label: "provider", value: "", options: [{ value: "owl", label: "owl", icon: "owl" }] }],
      ...(wired && iso.on && iso.claude ? { twinCode: true } : {}),
    }],
    profiles: [], settings,
  });
  async function serve(route) {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/settings/desktop-isolated") {
      iso.on = req.postDataJSON().on;
      iso.claude = iso.on;
      iso.launcher = "/test/Applications/Claude Owl.app";
      return json(state());
    }
    if (url.pathname === "/api/agents/connect/claude-desktop") {
      wired = true;
      return json(state());
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve };
}

const W = {
  en: { before: "Not connected · Claude Code · Claude Owl uses its own settings", after: "On owl through Claude Owl" },
  zh: { before: "未接入 · 用 Claude Code · Claude Owl 自己的设置", after: "经 Claude Owl 接入 owl" },
  "zh-TW": { after: "經 Claude Owl 接入 owl" },
  ja: { after: "Claude Owl 経由で owl に接続" },
  de: { after: "Über Claude Owl auf owl" },
};
const cd = '.row.agent[data-id="claude-desktop"]';
const code = '.row.agent[data-id="claude#owl-desktop"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const width of [1100, 640]) {
      test(`${engine} ${lang} ${width}px: after Use Claude Owl the lit row is Claude Owl, and its Code tab's row says it is on owl`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 800 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang).serve);
        await page.goto("http://owl.test/");
        await page.locator(cd).waitFor({ state: "attached" });
        if (await page.locator(`.agent-fold ${cd}`).count()) {
          await page.locator(".agent-more").click();
          await page.waitForFunction(() => !document.querySelector(".agent-fold-inner")?.inert);
        }
        assert.equal(await page.locator(`${cd} .name`).textContent(), "Claude Desktop");
        const line = page.locator(`${code} .ag-st`);
        if (w.before) assert.equal((await line.locator(".ag-st-t").textContent()).trim(), w.before);
        assert.equal(await line.evaluate((n) => n.classList.contains("on")), false, "lit before Claude Owl is on owl");

        const y = await page.evaluate(() => scrollY);
        await page.locator(`${cd} .ag-twin`).click();
        await page.locator(`${cd} .ag-conn.on`).waitFor();
        // the lit row is Claude Owl's; none named Claude Desktop is on
        assert.equal(await page.locator(`${cd} .name`).textContent(), "Claude Owl");
        assert.equal(await page.evaluate(() => scrollY), y, "a click scrolled the page");
        const lit = await page.locator(".row.agent").evaluateAll((rs) => rs.filter((r) => r.querySelector(".ag-conn.on")).map((r) => r.querySelector(".name").textContent));
        assert.ok(!lit.includes("Claude Desktop"), `on: ${lit}`);
        // the Code tab's row: on owl through Claude Owl, not "not connected"
        await page.locator(code).scrollIntoViewIfNeeded();
        assert.equal((await line.locator(".ag-st-t").textContent()).trim(), w.after);
        assert.equal(await line.evaluate((n) => n.classList.contains("on")), true, "its line isn't lit");
        const box = await line.boundingBox(), row = await page.locator(code).boundingBox();
        assert.ok(box.x + box.width <= row.x + row.width + 1 && box.x + box.width <= width, `past the row: ${JSON.stringify([box, row])}`);
        assert.equal(await page.locator("select").count(), 0, "a native select");
        assert.deepEqual(errors, []);
      });
    }
  }
}
