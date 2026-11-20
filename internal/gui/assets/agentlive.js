// The line under a connected agent's name saying which provider, model and
// account its last request through owl went to, what it has spent today,
// and — when it was passed over from another one — why. The state's
// agents[].live (agentlive.go) says where it went; today's spend needs the
// usage summary, which /api/state never reads, so it comes from the page or
// panel that already reads it (window.agentToday) and, where no page reads
// it (a narrow window, or the Usage page hidden), from one light read of
// the requests page per day, at the first connected row.
// A page that draws the rows calls mark (window.agentLive), and the tray
// panel's overview builds a line of its own. Loaded only when the page
// draws agents, so the core carries nothing of it (coreScriptBudget).

// today's spend per agent from the light read, the day its data is of,
// when it was last asked, and the ask in flight. The day is the one the
// data is of — the requests page's own window start — not the day the
// answer landed, so a read sent at 23:59 and answered at 00:00 is not
// drawn as the new day's.
let spent = null, spentDay = "", spentAt = 0, spentAsk = null;
const day = () => {
  const d = new Date();
  return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
};
// the data's day from the requests page: its first bucket's time, in the
// zone it was read in; "" when the answer has no chart
const dataDay = (p) => (p?.series?.[0]?.time || "").slice(0, 10);

// today: what the agent spent today, as the page that already read it
// says (pagehead.js's /api/usage or panelhome.js's requests page), else
// the light read's answer for this day; nil when neither has one
function spendOf(a) {
  const ext = window.agentToday?.(a);
  if (ext) return ext;
  if (!spent || spentDay !== day()) return null;
  return spent[a.id] || (a.name && spent[a.name]) || null;
}

// readSpend: only where no page reads today itself — a narrow window, or
// the Usage page hidden — the row's spend comes from one light read of the
// requests page (the latest row per agent, not the summary). It is asked
// when the day turns, and again when the reader asks for it (a return to
// the window, an existing refresh), at most once a minute: no timer, and
// never beside a page's own read.
function readSpend(force) {
  if (spentAsk || mode !== "window") return;
  // the rows of a view not on screen are drawn too: their spend is not
  // asked for until the Agents page is
  if (typeof view === "string" && view !== "agents") return;
  if (window.agentTodayWillRead?.() !== false) return; // that page's read is this line's
  const now = Date.now();
  if (spentAt && now - spentAt < 60e3) return; // a return or a refresh, at most a minute apart
  if (!force && spentDay === day()) return;
  const sentDay = day(); // the request's day, not the answer's
  spentAt = now;
  spentAsk = api("usage/requests?period=today&limit=1").then((p) => {
    const m = {};
    for (const g of p?.by?.agent || []) {
      if (g.id) m[g.id] = g;
      if (g.name) m[g.name] = g;
    }
    spent = m;
    spentDay = dataDay(p) || sentDay;
    paint(); // the day guard in spendOf draws it only when it is today's
  }, () => {} /* a failed read is unknown: keep the last, ask again after a minute */)
    .finally(() => { spentAsk = null; });
}

function parts(a, withCost) {
  const l = a.live, out = [];
  const place = [l.provider, l.model].filter(Boolean).join("/");
  if (place) out.push(t("Via {model}", { model: place }));
  if (l.account) out.push(l.account);
  if (withCost !== false) {
    const u = spendOf(a);
    if (u) {
      const cost = fmtCost({ cost: u.cost, unpriced: u.unpriced > 0 });
      if (cost) out.push(t("{cost} today", { cost }));
    }
  }
  if (l.rerouted) {
    const why = l.rerouted.why;
    out.push(why
      ? t("Rerouted from {from}: {why}", { from: l.rerouted.from, why })
      : t("Rerouted from {from}", { from: l.rerouted.from }));
  }
  return out;
}

// say: the line's words, the time it was made left to its title so the
// place and the account fit the row
export function sayOf(a, withCost) {
  if (!a?.live) return t("No requests through owl today");
  return parts(a, withCost).join(" · ");
}

// the whole of it, for the row's title: when, then the same words
function full(a) {
  if (!a?.live) return sayOf(a);
  return [t("Last request {when}", { when: ago(a.live.at) }), ...parts(a, true)].join(" · ");
}

// line: the row's line, its title the whole of it even where the row cuts it
export function line(a, withCost) {
  const s = el("div", "sub ag-live", sayOf(a, withCost));
  s.title = full(a);
  return s;
}

// mark: the line in an agent's row, on a line of its own across the row,
// under the name, the model and the switch (app.css places it in the window,
// the panel and the window's cards alike). A card that already says today's
// spend (the window's own line above 1200px, pagehead.js) keeps that figure
// only, so the line does not say it twice.
export function mark(row, a) {
  if (!a?.wired) return;
  row.append(line(a, !row.querySelector(":scope > .ag-today")));
  row.classList.add("with-live");
  // the first connected row is the moment the fallback read is made, where
  // no page reads today itself; no connected agent, no read
  readSpend();
}

// paint: the lines drawn before today's spend was read say it again, in
// place, so what a read of usage changes does not redraw the row under a
// click. The window's cards drop the figure when pagehead's own line has it.
export function paint() {
  for (const r of document.querySelectorAll(".row.agent[data-id]")) {
    const a = state?.agents?.find((x) => x.id === r.dataset.id);
    const e = r.querySelector(":scope > .ag-live");
    if (a && e) {
      e.textContent = sayOf(a, !r.querySelector(":scope > .ag-today"));
      e.title = full(a);
    }
  }
  for (const r of document.querySelectorAll(".ph-agent[data-key]")) {
    const a = state?.agents?.find((x) => "a:" + x.id === r.dataset.key);
    const e = r.querySelector(".ph-agent-live");
    if (a && e) {
      e.textContent = sayOf(a, false);
      e.title = full(a);
    }
  }
}

export function start() {
  window.agentLive = { mark, line, say: sayOf, full, paint, readSpend };
  // the rows may have been drawn before this module landed
  if (state?.agents?.length) {
    renderAgents();
    window.panelHomeRefresh?.();
  }
  // coming back is the reader asking for it: a fresh answer, at most once
  // a minute (readSpend), never on a timer
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") readSpend(true);
  });
}