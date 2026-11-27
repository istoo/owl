// Run with Node's test runner and Playwright on the module path; see README.md.
// The "In your agents" rows (an MCP server or a skill the agents have of
// their own, not in the library) showed each agent as a full-size logo in
// the row's own text colour, one after another: nine of them read as the
// row's content, louder than its name, and which agent was which was left
// to its silhouette. They are drawn as the hand of chips a library row's
// agents get (client.js haveAgents): the same marks, read only, on the
// row's one line, named in the hand's tooltip. English and Chinese, wide
// and narrow, in Chromium and WebKit. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { navTo } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/me";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const AGENTS = [
  agent("claude", "Claude Code", "claudecode-color"),
  agent("codex", "Codex", "codex-color"),
  agent("opencode", "OpenCode", "opencode"),
  agent("cursor", "Cursor", "cursor"),
  agent("copilot", "GitHub Copilot", "copilot-color"),
  agent("qoder", "Qoder", "qoder"),
  agent("grok", "Grok Build", "grok"),
  agent("pi", "Pi", "pi"),
  agent("gemini", "Gemini CLI", "geminicli-color"),
];
const ALL = AGENTS.map((a) => a.id);
const srv = (name, ids, extra = {}) => ({ name, transport: "stdio", command: "npx -y " + name, agents: ids, ...extra });
const found = [
  // every agent, as a server an app installs for itself
  { server: srv("many", ALL) },
  // two of them, with a difference said beside it — as many agents as a real
  // server is set up in, so the pill is longer than a narrow row can hold
  { server: srv("two", ["claude", "cursor"], { command: "", transport: "http", url: "https://api.githubcopilot.com/mcp" }), others: ["opencode", "qoder", "grok", "pi", "gemini"] },
];
const lib = () => ({
  dir: `${HOME}/.config/owl/library`, backups: `${HOME}/.config/owl/backups`, home: HOME,
  agents: structuredClone(AGENTS), instructions: { agents: [], sets: [] }, servers: [], skills: [],
  foundServers: structuredClone(found), projects: [], foundSkills: [], skillGroups: [], problems: [],
});

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(lib());
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], gateway: { running: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// where each chip is drawn: its size, and where on the row it sits
const chips = (row) => [...row.querySelectorAll(":scope > .lib-agents.lib-have > .lib-ag")].map((c) => {
  const r = c.getBoundingClientRect(), i = c.querySelector(".ic").getBoundingClientRect();
  return { x: Math.round(r.left), top: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height), iw: Math.round(i.width), title: c.title };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the agents that already have it are drawn as a hand of chips", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    // 734 is the owner's window, where a pill listing eight agents ran past
    // the row's right edge and took Bring in and Remove with it
    for (const lang of ["en", "zh"]) for (const width of [900, 734, 440]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 900 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("owl.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        try {
          await page.goto("http://owl.test/");
          await navTo(page, "library");
          // the rows of the servers the agents have of their own, by name
          const rowOf = (name) => page.locator(`#view-library .lib-row:has(> .who > .name.mono:text-is("${name}"))`);
          await rowOf("many").waitFor();
          assert.equal(await rowOf("many").count(), 1, "the server every agent has is listed");
          assert.equal(await rowOf("two").count(), 1, "and the one two have");

          // every agent is a chip of the same hand a library row's agents get
          for (const [name, count] of [["many", ALL.length], ["two", 2]]) {
            const row = rowOf(name);
            assert.ok(await row.evaluate((r) => r.classList.contains("with-agents")), `${name}: the row keeps room for its name`);
            const cs = await row.evaluate(chips);
            assert.equal(cs.length, count, `${name}: one chip an agent, ${JSON.stringify(cs)}`);
            for (const c of cs) {
              // a chip, not a logo the row's own size: the row's mark is 26px
              assert.ok(c.w <= 26 && c.h <= 26, `${name}: a chip of ${c.w}×${c.h}`);
              assert.ok(c.iw <= 16, `${name}: its mark of ${c.iw}px`);
            }
            // on one line, fanned in the row's direction, none drawn over the next
            assert.equal(new Set(cs.map((c) => c.top)).size, 1, `${name}: one line, ${JSON.stringify(cs)}`);
            for (let i = 1; i < cs.length; i++) assert.ok(cs[i].x > cs[i - 1].x, `${name}: in order, ${JSON.stringify(cs)}`);
            // and every chip says which agent it is
            assert.deepEqual(cs.map((c) => c.title), AGENTS.filter((a) => (name === "many" ? ALL : ["claude", "cursor"]).includes(a.id)).map((a) => a.name));
            // the hand is named as a whole too, for a reader that can't hover
            assert.equal(await row.locator(":scope > .lib-have").getAttribute("aria-label"), cs.map((c) => c.title).join(", "));
          }

          // the hand doesn't run into the button beside it, and nothing
          // pushes the page sideways
          const overlap = await rowOf("many").evaluate((row) => {
            const hand = row.querySelector(":scope > .lib-have").getBoundingClientRect();
            const b = row.querySelector(":scope > button.action").getBoundingClientRect();
            return Math.round(hand.right - b.left);
          });
          assert.ok(overlap <= 0, `the hand ends ${overlap}px into Bring in`);
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, `no sideways scroll at ${width}px`);

          // the pill naming the agents that have another server by this name
          // gives way instead of pushing the row's buttons out of the list:
          // a row cut at its right edge hid Bring in and lost the remove
          // button altogether (no room left for them)
          for (const name of ["many", "two"]) {
            const inside = await rowOf(name).evaluate((row) => {
              const r = row.getBoundingClientRect();
              const cs = getComputedStyle(row);
              const right = r.right - parseFloat(cs.paddingRight);
              const out = [];
              for (const c of row.children) {
                const b = c.getBoundingClientRect();
                if (c.matches(".lib-agents")) {
                  // the hand is laid out fanned, past its own box on purpose
                  if (Math.round(b.right) > Math.round(right) + 1) out.push(`${c.className}: hand ends ${Math.round(b.right - right)}px out`);
                  continue;
                }
                if (Math.round(b.right) > Math.round(right) + 1) out.push(`${c.className || c.tagName}: ${Math.round(b.right - right)}px out`);
              }
              const btn = row.querySelector(":scope > button.action");
              const rm = row.querySelector(":scope > .lib-icon.danger");
              return {
                out,
                scroll: row.scrollWidth - row.clientWidth,
                bring: btn ? Math.round(btn.getBoundingClientRect().width) : 0,
                trash: rm ? Math.round(rm.getBoundingClientRect().width) : 0,
                bringIn: btn ? Math.round(btn.getBoundingClientRect().right) <= Math.round(right) : false,
                trashIn: rm ? Math.round(rm.getBoundingClientRect().right) <= Math.round(right) : false,
              };
            });
            assert.deepEqual(inside.out, [], `${name}: nothing sticks out of the row`);
            assert.ok(inside.scroll <= 0, `${name}: the row hides nothing (${inside.scroll}px)`);
            assert.ok(inside.bring > 0 && inside.bringIn, `${name}: Bring in is whole and inside the row`);
            assert.ok(inside.trash > 0 && inside.trashIn, `${name}: Remove is whole and inside the row`);
          }

          // the long pill is what gives way, and it says so with an ellipsis
          // (its tooltip holds the full sentence) rather than being cut off
          const pill = await rowOf("two").evaluate((row) => {
            const tag = row.querySelector(":scope > .lib-tag.warn");
            const t = tag.querySelector(":scope > .t");
            const cs = getComputedStyle(t);
            const btn = row.querySelector(":scope > button.action").getBoundingClientRect();
            return { text: t.textContent, cut: t.scrollWidth > t.clientWidth + 1, ellipsis: cs.textOverflow, title: tag.title, height: tag.getBoundingClientRect().height, under: Math.round(tag.getBoundingClientRect().top - btn.top) };
          });
          assert.ok(pill.ellipsis === "ellipsis", "the pill ends in an ellipsis");
          assert.ok(pill.title && pill.title.length > pill.text.length, "its tooltip still holds the whole sentence");
          if (width === 900) assert.equal(pill.cut, false, "wide, the pill says it all");
          else if (width === 734) assert.equal(pill.cut, true, `at ${width}px the pill is cut, not the row: ${pill.text}`);
          // a phone-width row puts the pill under the name, the hand and the
          // buttons, where it has the row's whole width for the names
          if (width === 440) assert.ok(pill.under > 20, `the pill sits under the row's first line (${pill.under}px)`);
          else assert.ok(Math.abs(pill.under) < 20, `the pill sits beside Bring in (${pill.under}px)`);
          assert.deepEqual(errors, []);
        } finally {
          await ctx.close();
        }
      });
    }
  });
}