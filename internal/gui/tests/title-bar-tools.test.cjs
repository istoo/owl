// Run with Node's test runner and Playwright on the module path; see README.md.
// #2625 (linusxiong): beside the sidebar, a page whose head is only its
// tools (Library, Plugins, Settings) had them in the title bar, after where
// the reader is, and in a narrow window they ran over the title. Pinned
// there they then wrapped: the tabs took two lines and pushed into the
// title bar (owl's owner, 11-14: Settings', Plugins' and the Library's
// heads "都有bug", "窗口的宽度变窄的时候会出现错位"). Now the head is a row
// of its own under the title bar: the tabs on one line, the tools beside
// them on the same line, none over a tab or another tool or past the
// page's edge, and the title whole in the bar. A tool shown as its icon
// keeps its name for the reader and a tooltip. Settings from 900px wide
// lays its tabs down the page's left, and says so to a screen reader.
// Every Library tab, Plugins' and Settings', the window from its least
// width (560px) up, the sidebar picked, in all five languages, Chromium
// and WebKit. No backend: the API is faked.
//
// 11-16 (screenshot): beside the rail in a 700px window, Chinese Settings'
// last part read "关:" — the search box shared the tabs' line and took its
// room. The search has a line of its own over the parts now, the head's
// width, and every part is whole: one that the strip can't fit scrolls
// under a fade on the side it hides, and the part reached (a click, an
// arrow key) is brought whole into view. The 700px window is its own test,
// and a 560px German strip, which must scroll, checks the fade and the
// part brought into view.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.OWL_GUI_ASSETS || path.join(__dirname, "../assets"));
const HOME = "/Users/tester";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });

function serve(lang) {
  const view = {
    dir: `${HOME}/.owl/library`, backups: `${HOME}/.owl/backups`, home: HOME,
    agents: [agent("claude", "Claude Code", "claudecode-color")],
    instructions: { agents: [], sets: [] },
    servers: [{ name: "filesystem", transport: "stdio", command: "npx", args: ["-y", "fs"], agents: ["claude"] }],
    foundServers: [], projects: [], foundSkills: [], problems: [], hooks: [], subagents: [],
    skills: [{ name: "pdf", kind: "folder", description: "PDFs", agents: ["claude"], source: `${HOME}/skills/pdf` }],
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [], mirror: false });
    if (url.pathname === "/api/library") return json(view);
    if (url.pathname === "/api/library/market/servers") return json({ items: [{ id: "fetch", name: "fetch", title: "Fetch", description: "Read the web.", transport: "stdio", runs: "uvx", featured: true }] });
    if (url.pathname === "/api/library/market/skills") return json({ items: [] });
    if (url.pathname === "/api/library/rtk") return json({ path: `${HOME}/.local/bin/rtk`, version: "0.51.0", url: "https://www.rtk-ai.app", agents: [{ id: "claude", name: "Claude Code", icon: "", on: true }] });
    if (url.pathname === "/api/plugins/builtin") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, url: "http://127.0.0.1:3425" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

// what the title bar and the page's head show, once they have settled: the
// title's parts as far as they show, the head's tabs and tools, and the
// window's own buttons
function bar(head) {
  const box = (r) => ({ left: Math.round(r.left), right: Math.round(r.right), top: Math.round(r.top), bottom: Math.round(r.bottom) });
  const crumb = document.querySelector("#crumb"), c = crumb.getBoundingClientRect();
  const parts = [...crumb.children].map((e) => {
    const r = e.getBoundingClientRect();
    return { text: e.textContent, shown: r.width > 0, right: Math.min(r.right, c.right), cut: r.right > c.right + 0.5 || e.scrollWidth > e.clientWidth + 0.5 };
  });
  const h = document.querySelector(head);
  const shown = (e) => e.getClientRects().length && getComputedStyle(e).visibility !== "hidden" && !e.closest("[hidden]");
  const strip = h.querySelector(".lib-tabs, .set-tabs");
  const tabs = strip ? [...strip.querySelectorAll(":scope > button")].filter(shown).map((e) => box(e.getBoundingClientRect())) : [];
  // the strip's and its parts' exact edges, for what must be whole in it
  const tabEdges = strip ? [...strip.querySelectorAll(":scope > button")].filter(shown).map((e) => {
    const r = e.getBoundingClientRect();
    return { text: e.textContent.trim(), left: r.left, right: r.right, top: r.top, bottom: r.bottom };
  }) : [];
  const sr = strip ? strip.getBoundingClientRect() : null;
  const hr = h.getBoundingClientRect();
  const q = h.querySelector(".set-q"), qr = q ? q.getBoundingClientRect() : null;
  const sc = strip ? getComputedStyle(strip) : null;
  const mask = !!(sc && ((sc.maskImage && sc.maskImage !== "none") || (sc.webkitMaskImage && sc.webkitMaskImage !== "none")));
  const tools = [...h.querySelectorAll("button, label, input")].filter((e) => shown(e) && !e.closest(".lib-tabs, .set-tabs"))
    .filter((e) => !e.parentElement.closest("button, label"))
    .map((e) => {
      const label = [...e.querySelectorAll("span")].find((s) => s.textContent.trim() && !s.closest(".lib-mini"));
      return {
        ...box(e.getBoundingClientRect()), aria: e.getAttribute("aria-label"), title: e.title,
        label: label ? label.getClientRects().length > 0 : null, clipped: e.scrollWidth > e.clientWidth + 1,
      };
    });
  const acts = document.querySelector(".top .actions").getBoundingClientRect();
  return {
    parts, crumb: box(c), tools, tabs, strip: strip ? box(sr) : null, orient: strip?.getAttribute("aria-orientation") || null,
    tabEdges, stripEdge: sr ? { left: sr.left, right: sr.right, top: sr.top, bottom: sr.bottom, width: sr.width } : null,
    q: qr ? { left: qr.left, right: qr.right, top: qr.top, bottom: qr.bottom, height: qr.height } : null,
    headEdge: { left: hr.left, right: hr.right, top: hr.top, bottom: hr.bottom }, mask,
    scrollable: !!strip && strip.scrollWidth > strip.clientWidth + 1,
    head: box(hr), top: Math.round(document.querySelector(".top").getBoundingClientRect().bottom),
    acts: box(acts), side: document.querySelector("#side").getBoundingClientRect().right, view: innerWidth,
  };
}

const pages = [
  ...["instructions", "mcp", "skills", "hooks", "subagents", "rtk"].map((tab) => ({ name: "library " + tab, url: "?view=library", head: "#view-library .lib-head", init: ["owl.libTab", tab] })),
  ...["builtin", "discover", "installed"].map((tab) => ({ name: "plugins " + tab, url: "?view=plugins", head: "#view-plugins .pm-head", init: ["owl.pluginTab", tab] })),
  { name: "settings", url: "?view=settings", head: "#view-settings .set-head" },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["zh", "zh-TW", "ja", "de", "en"]) {
    test(`${engine} ${lang}: a page's head is a row of its own, its tabs on one line`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [560, 650, 760, 900, 1024, 1280]) {
        const context = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        let on = "";
        page.on("pageerror", (e) => {
          // WebKit reports a read its navigation cancelled as a page error:
          // "Fetch API cannot load … due to access control checks" (#1350,
          // #1307), not this test's. Its message starts partway through the
          // sentence ("/owl.test/api/agents/cli due to access control
          // checks."), so the filter reads the stack, which has it whole.
          if (engine === "webkit" && /Fetch API cannot load .* due to access control checks/.test(e.stack)) return;
          errors.push(`${width}px ${on}: ${e.stack}`);
        });
        await page.route("**/*", serve(lang));
        await page.addInitScript(() => { localStorage.setItem("owl.navLayout", "side"); });
        for (const p of pages) {
          on = p.name;
          if (p.init) await page.evaluate(([k, v]) => localStorage.setItem(k, v), p.init).catch(() => {});
          await page.goto("http://owl.test/" + p.url);
          if (p.init) { await page.evaluate(([k, v]) => localStorage.setItem(k, v), p.init); await page.reload(); }
          await page.locator(p.head).waitFor();
          await page.locator("#crumb b").waitFor();
          // the Library's buttons have their tooltips once it has loaded
          if (p.name.startsWith("library")) await page.locator("#view-library .lib-more:not([disabled])").waitFor();
          if (p.name === "library mcp") await page.locator("#view-library .lib-discover").waitFor();
          if (/plugins (discover|installed)/.test(p.name)) await page.locator("#view-plugins .pm-mirror").waitFor();
          // settled: two frames with the same bar
          let b = null;
          for (let i = 0; i < 40; i++) {
            await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
            const now = await page.evaluate(bar, p.head);
            if (b && JSON.stringify(now) === JSON.stringify(b)) break;
            b = now;
          }
          const at = `${width}px ${p.name}: ${JSON.stringify(b)}`;
          // the head is the page's, under the title bar, and keeps inside it
          assert(b.head.top >= b.top, `the page's head lies in the title bar at ${at}`);
          assert(b.head.left >= b.side && b.head.right <= b.view, `the page's head runs out of the page at ${at}`);
          // Plugins' Built-in has no search and no mirrors to pick
          assert(b.tools.length > 0 || p.name === "plugins builtin", `no tools in the head at ${at}`);
          const down = p.name === "settings" && width >= 900;
          if (b.tabs.length) {
            // one line of tabs, or (Settings, wide) one column of them
            if (down) assert(b.tabs.every((x) => x.left === b.tabs[0].left), `Settings' tabs are not one column at ${at}`);
            else assert(b.tabs.every((x) => x.top === b.tabs[0].top), `the tabs wrap at ${at}`);
          }
          if (p.name === "settings") assert.equal(b.orient, down ? "vertical" : "horizontal", `Settings' tabs say the wrong way at ${at}`);
          if (p.name === "settings" && !down) {
            // 11-16: beside the rail a search on the tabs' line took the last
            // part's room ("关于" read "关:"). It is a line of its own now,
            // over the parts and the head's whole width
            assert(b.q, `no search box in the head at ${at}`);
            assert(b.q.bottom <= b.stripEdge.top + 0.5, `the search sits on the tabs' line at ${at}`);
            assert(b.q.left <= b.headEdge.left + 0.5 && b.q.right >= b.headEdge.right - 0.5, `the search is not the head's width at ${at}`);
            // every part is whole in the strip, or the strip scrolls and
            // fades the side that hides it: never a part cut with no hint
            for (const tab of b.tabEdges) {
              const whole = tab.left >= b.stripEdge.left - 0.5 && tab.right <= b.stripEdge.right + 0.5;
              assert(whole || (b.scrollable && b.mask), `a part is cut off with no hint at ${at}: ${JSON.stringify(tab)}`);
            }
            // Chinese's parts are whole at 650 and 760 without a scroll
            if (/^zh/.test(lang) && (width === 650 || width === 760)) {
              assert(!b.scrollable, `Chinese's parts need a scroll at ${at}`);
              for (const tab of b.tabEdges) {
                assert(tab.left >= b.stripEdge.left - 0.5 && tab.right <= b.stripEdge.right + 0.5, `a Chinese part is cut at ${at}: ${JSON.stringify(tab)}`);
              }
            }
          }
          for (const tool of b.tools) {
            assert(tool.left >= b.head.left - 0.5 && tool.right <= b.head.right + 0.5, `a tool runs out of the head at ${at}`);
            if (!down) {
              assert(tool.top >= b.head.top - 0.5 && tool.bottom <= b.head.bottom + 0.5, `a tool is off the head's line at ${at}`);
              if (b.strip) {
                const across = !(tool.right <= b.stripEdge.left || tool.left >= b.stripEdge.right);
                const over = !(tool.bottom <= b.stripEdge.top + 0.5 || tool.top >= b.stripEdge.bottom - 0.5);
                assert(!(across && over), `a tool lies over the tabs at ${at}`);
              }
            }
            assert(!tool.clipped, `a tool's label is cut at ${at}`);
            if (tool.label === false) assert(tool.aria && tool.title, `a tool shown as its icon has no name or tooltip at ${at}`);
          }
          for (let i = 1; i < b.tools.length; i++) {
            const [x, y] = [b.tools[i - 1], b.tools[i]].sort((m, n) => m.left - n.left || m.top - n.top);
            assert(x.right <= y.left || x.bottom <= y.top, `two tools lie over each other at ${at}`);
          }
          // the title has the bar to itself: whole, and clear of the
          // window's buttons
          assert(b.crumb.right <= b.acts.left, `the title runs under the window's buttons at ${at}`);
          assert(b.parts.every((x) => x.shown && !x.cut), `the title is cut at ${at}`);
        }
        assert.deepEqual(errors, []);
        await context.close();
      }
    });

    // 11-16 (screenshot): a 700px window, the sidebar as its rail, Chinese:
    // the search took the last part's room and "关于" read "关:". Every part
    // is whole now, with no scroll needed
    test(`${engine} zh: Settings beside the rail at 700px keeps every part whole`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 700, height: 640 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => {
        if (engine === "webkit" && /Fetch API cannot load .* due to access control checks/.test(e.stack)) return;
        errors.push(e.stack);
      });
      await page.route("**/*", serve("zh"));
      await page.addInitScript(() => { localStorage.setItem("owl.navLayout", "side"); });
      await page.goto("http://owl.test/?view=settings");
      await page.locator("#view-settings .set-head").waitFor();
      await page.locator("#crumb b").waitFor();
      let b = null;
      for (let i = 0; i < 40; i++) {
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        const now = await page.evaluate(bar, "#view-settings .set-head");
        if (b && JSON.stringify(now) === JSON.stringify(b)) break;
        b = now;
      }
      const at = `700px: ${JSON.stringify(b)}`;
      assert(await page.evaluate(() => document.body.classList.contains("side-nav")), `the rail is not shown at ${at}`);
      const about = b.tabEdges.find((x) => x.text === "关于");
      assert(about, `the last part has no tab at ${at}`);
      assert(!b.scrollable, `the parts need a scroll at ${at}`);
      for (const tab of b.tabEdges) {
        assert(tab.left >= b.stripEdge.left - 0.5 && tab.right <= b.stripEdge.right + 0.5, `a part is cut at ${at}: ${JSON.stringify(tab)}`);
      }
      assert(about.left >= b.stripEdge.left - 0.5 && about.right <= b.stripEdge.right + 0.5, `the last part is cut at ${at}: ${JSON.stringify(about)}`);
      assert(b.q.bottom <= b.stripEdge.top + 0.5, `the search sits on the tabs' line at ${at}`);
      assert(b.q.left <= b.headEdge.left + 0.5 && b.q.right >= b.headEdge.right - 0.5, `the search is not the head's width at ${at}`);
      assert.deepEqual(errors, []);
      await context.close();
    });

    // a strip too narrow even for its own line scrolls; the part reached by
    // the keys is brought whole into view, and the side it hides is faded
    test(`${engine}: a scrolling Settings strip brings the picked part whole into view`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 560, height: 640 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", serve("de"));
      await page.addInitScript(() => { localStorage.setItem("owl.navLayout", "side"); });
      await page.goto("http://owl.test/?view=settings");
      await page.locator("#view-settings .set-head").waitFor();
      await page.locator("#setTab-general").focus();
      await page.keyboard.press("End");
      await page.waitForFunction(() => document.querySelector("#setTab-about").getAttribute("aria-selected") === "true");
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const v = await page.evaluate(() => {
        const strip = document.querySelector("#setTabs"), r = strip.getBoundingClientRect(), t = document.querySelector("#setTab-about").getBoundingClientRect();
        const cs = getComputedStyle(strip);
        return {
          left: t.left, right: t.right, sl: r.left, sr: r.right, scrollLeft: strip.scrollLeft,
          sw: strip.scrollWidth, cw: strip.clientWidth,
          mask: (cs.maskImage && cs.maskImage !== "none") || (cs.webkitMaskImage && cs.webkitMaskImage !== "none"),
        };
      });
      const at = `560px de: ${JSON.stringify(v)}`;
      assert(v.sw > v.cw + 1, `the strip does not scroll at ${at}`);
      assert(v.scrollLeft > 0, `the strip did not move to the part reached at ${at}`);
      assert(v.left >= v.sl - 0.5 && v.right <= v.sr + 0.5, `the part reached is not whole at ${at}`);
      assert(v.mask, `the side the strip hides is not faded at ${at}`);
      await context.close();
    });
  }
}
