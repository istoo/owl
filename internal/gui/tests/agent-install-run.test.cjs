// Run with Node's test runner and Playwright on the module path; see README.md.
// The Agents page's Install for an agent that isn't on this computer: the
// button asks first, showing the exact command and where it comes from;
// the request names the agent and the command's number and nothing else;
// the run shows "installing" and the end of its output; a success lists
// the agent with the others, a failure shows the last lines and the command
// to run in a terminal; an npm command that wants an administrator has no
// button. In the window, wide and narrow, in every language. No backend:
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a", "model-b"].map((m) => ({ value: m, label: m }));
const agent = (id, name) => ({ id, name, path: "~/." + id + "/config.yaml", fields: [{ key: "model", label: "model", value: "model-a", options: models }] });
const CRUSH = agent("crush", "Crush");
const OPENCODE = agent("opencode", "OpenCode");
const SCRIPT = "curl -fsSL https://opencode.ai/v2/install | bash";
const NPM = "npm install -g opencode-ai";
const INSTALLS = [
  { id: "opencode", name: "OpenCode", icon: "opencode", commands: [{ via: "script", command: SCRIPT }, { via: "npm", command: NPM }] },
  { id: "pi", name: "Pi", icon: "pi", commands: [{ via: "npm", command: "npm install -g pi-agent", admin: "C:\\Program Files\\nodejs" }] },
];

// how: "done" or "failed", what the run ends in after two polls that say it is running
function server(lang, how, seen) {
  let polls = 0, installed = false;
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const p = url.pathname;
    if (p === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (p === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    const st = () => ({ agents: installed ? [CRUSH, OPENCODE] : [CRUSH], profiles: [], settings: { lang, theme: "light" } });
    if (p === "/api/state") return route.fulfill({ json: st() });
    if (p === "/api/agents/rescan") { seen.rescans++; return route.fulfill({ json: st() }); }
    if (p === "/api/agents/cli" && req.method() === "GET") return route.fulfill({ json: { agents: {}, pending: false } });
    if (p === "/api/agents/install" && req.method() === "POST") {
      seen.posts.push(req.postDataJSON());
      return route.fulfill({ json: { id: "opencode", name: "OpenCode", index: 0, command: SCRIPT, via: "script", state: "running", tail: "downloading " + SCRIPT.split(" ")[2] + "\n" } });
    }
    if (p === "/api/agents/install") return route.fulfill({ json: installed ? [INSTALLS[1]] : INSTALLS });
    if (p === "/api/agents/install/run") {
      polls++;
      const run = { id: "opencode", name: "OpenCode", index: 0, command: SCRIPT, via: "script" };
      if (!seen.posts.length) return route.fulfill({ json: {} });
      if (polls < 3) return route.fulfill({ json: { ...run, state: "running", tail: "downloading\ninstalling opencode " + polls + "\n" } });
      if (how === "done") { installed = true; return route.fulfill({ json: { ...run, state: "done", tail: "installed opencode\n" } }); }
      return route.fulfill({ json: { ...run, state: "failed", error: "exit status 1", tail: "downloading\nnpm ERR! boom at the last line\n" } });
    }
    if (p === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (p === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (p === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (p === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (p.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, p === "/" ? "index.html" : p);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const said = (page, lang, key, vars = {}) => page.evaluate(([lang, key, vars]) => {
  const s = lang === "en" ? key : I18N[lang]?.[key];
  if (!s) return null;
  return s.replace(/\{(\w+)\}/g, (_, k) => vars[k]);
}, [lang, key, vars]);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Install asks first, runs the listed command and shows how it went", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        for (const how of ["done", "failed"]) {
          await t.test(`${lang} ${width} ${how}`, async () => {
            const seen = { posts: [], rescans: 0 };
            const page = await (await browser.newContext({ viewport: { width, height: 760 } })).newPage();
            page.setDefaultTimeout(8000);
            page.setDefaultNavigationTimeout(60000);
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", server(lang, how, seen));
            await page.goto("http://owl.test/");
            const head = page.locator("#agentsInstall .ag-install-head");
            await head.waitFor();
            await head.click();
            const row = (id) => page.locator(`#agentsInstall .ag-install-row[data-id="${id}"]`);
            // an npm command that wants an administrator is for a terminal
            await row("pi").waitFor();
            assert.equal(await row("pi").locator(".ag-install-go").count(), 0, "an administrator's npm has an Install");
            assert.equal(await row("opencode").locator(".ag-install-go").count(), 2);
            const install = await said(page, lang, "Install");
            assert(install, "Install has no " + lang);
            assert.equal(await row("opencode").locator(".ag-install-go").first().textContent(), install);
            // pressing it only asks: the exact command and its source, nothing sent
            await row("opencode").locator(".ag-install-go").first().click();
            const ask = page.locator("#modal .install-ask");
            await ask.waitFor();
            assert.equal(await ask.locator(".ag-install-cmd code").textContent(), SCRIPT);
            assert((await ask.textContent()).includes("https://opencode.ai/v2/install"), "the source isn't shown");
            assert.equal(seen.posts.length, 0, "something was sent before the question was answered");
            // another command can be picked
            assert.equal(await ask.locator(".segs .opt").count(), 2);
            await ask.locator(".segs .opt").nth(1).click();
            assert.equal(await ask.locator(".ag-install-cmd code").textContent(), NPM);
            await ask.locator(".segs .opt").nth(0).click();
            assert.equal(await ask.locator(".ag-install-cmd code").textContent(), SCRIPT);
            assert.equal(seen.posts.length, 0);
            // confirmed: the agent and the number, no command
            await ask.locator(".bar .primary").click();
            const installing = await said(page, lang, "Installing {agent}…", { agent: "OpenCode" });
            assert(installing, "Installing has no " + lang);
            await page.locator(`#modal .install-ask .lib-confirm:has-text("${installing}")`).waitFor();
            assert.deepEqual(seen.posts, [{ id: "opencode", index: 0 }]);
            assert(await page.locator("#modal .install-ask .call-body pre").evaluate((e) => e.textContent.includes("downloading")), "the output isn't kept");
            if (how === "done") {
              const ok = await said(page, lang, "{agent} is installed", { agent: "OpenCode" });
              await page.locator(`#modal .install-ask .lib-confirm:has-text("${ok}")`).waitFor();
              // looked for again: it is listed with the others, and no longer offered
              await page.locator('#agents .row.agent[data-id="opencode"]').waitFor();
              assert(seen.rescans >= 1);
              await page.locator('#agentsInstall .ag-install-row[data-id="opencode"]').waitFor({ state: "detached" });
            } else {
              const why = await said(page, lang, "Installing {agent} failed: {why}", { agent: "OpenCode", why: "exit status 1" });
              await page.locator(`#modal .install-ask .lib-confirm:has-text("${why}")`).waitFor();
              assert(await page.locator("#modal .install-ask .call-body pre").isVisible(), "the output's end isn't shown");
              assert((await page.locator("#modal .install-ask .call-body pre").textContent()).includes("npm ERR! boom at the last line"));
              // the command to run in a terminal, with a copy button
              assert.equal(await page.locator("#modal .install-ask .ag-install-cmd code").textContent(), SCRIPT);
              assert.equal(await page.locator("#modal .install-ask .ag-install-cmd .copy").count(), 1);
              assert.equal(await page.locator('#agents .row.agent[data-id="opencode"]').count(), 0);
            }
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), "the page scrolls sideways");
          });
        }
      }
    }
  });
}
