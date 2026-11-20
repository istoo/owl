// The tray panel's overview: a header saying whether the gateway runs, and
// three cards — today's spend with its hours, each subscription's tightest
// allowance, and the connected agents with their model and today's cost.
// A card's › opens the full page the panel had as its own tab; the router's
// line under today's hours does the same. app.js imports this only in the
// panel (panelhome.js), so the window's core carries none of it.
const TITLES = { usage: "Allowances", stats: "Usage", routing: "Routing", agents: "Agents" };
let home = null, status = null, statusDot = null, statusText = null, back = null, title = null;
let today = null;
let gateway = window.bootPrefs?.sideGateway || null, gatewayKnown = !!gateway;
let drawn = "";

const chev = () => {
  const s = el("span", "ph-chev");
  s.innerHTML = '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>';
  return s;
};

// the title bar: the owl and the gateway's state on the overview, a back
// and the detail's name on a page
function chrome() {
  const brand = $(".top .brand");
  if (brand && !status) {
    status = el("span", "ph-status");
    statusDot = el("i", "ph-dot");
    statusText = el("span", "ph-status-t");
    status.append(statusDot, statusText);
    status.hidden = true;
    brand.append(status);
  }
  if (!back) {
    back = el("button", "ph-back");
    back.type = "button";
    back.append(svg("M10 3.5 5.5 8 10 12.5", 14, 1.6), el("span", "", t("Overview")));
    back.setAttribute("aria-label", t("Back to overview"));
    back.onclick = () => setPanelTab("home");
    title = el("span", "ph-title");
    $(".top").prepend(back, title);
  }
  const tab = document.body.dataset.ptab;
  title.textContent = t(TITLES[tab] || "Overview");
}

function drawStatus() {
  if (!status) return;
  status.hidden = !gatewayKnown;
  if (!gatewayKnown) return;
  const on = !!gateway?.running;
  statusDot.classList.toggle("off", !on);
  statusText.textContent = t(on ? "Gateway running" : "Gateway stopped");
}

function readGateway() {
  if (window.bootPrefs?.sideGateway) { gateway = window.bootPrefs.sideGateway; gatewayKnown = true; drawStatus(); return; }
  if (typeof providers !== "undefined" && providers?.gateway) { gateway = providers.gateway; gatewayKnown = true; drawStatus(); return; }
  // the list kept from a last read, never a fresh one: the panel asks for
  // that only when its list is opened
  api("providers?last=1").then((r) => {
    if (!r?.gateway) return;
    gateway = r.gateway;
    gatewayKnown = true;
    drawStatus();
    draw();
  }, () => {});
}

// the panel's own Usage detail reads today's page (app.js's panelTodayPage):
// the overview shows that very answer, so the two never part
function readToday(force) {
  const p = window.panelTodayPage?.(force);
  if (!p) return;
  p.then((u) => { if (!u) return; today = u; draw(); }, () => {} /* a failed read is unknown: keep the last */);
}

// the overview as it stands, redrawn only when what it shows changed
function signature() {
  return JSON.stringify([state.agents.map((a) => [a.id, a.name, a.icon, !!a.wired, a.fields?.find((f) => f.key === "model")?.value]),
    quotas, quotaLeft, today, [...featuresOff], gatewayKnown && !!gateway?.running]);
}

function head(titleText, count, linkLabel, tab) {
  const h = el("div", "ph-head");
  const name = el("h2");
  name.append(t(titleText));
  if (count) name.append(el("em", "", count));
  h.append(name);
  if (linkLabel) h.append(openLink(linkLabel, tab, "head:" + tab));
  return h;
}

function openLink(label, tab, key) {
  const b = el("button", "ph-more");
  b.type = "button";
  b.dataset.open = tab;
  b.dataset.key = key;
  b.append(el("span", "", t(label)), chev());
  b.onclick = () => openTab(tab);
  return b;
}

function openTab(tab, agent) {
  // a card's › is the reader's own way in: the page it leaves gives way
  // without the scroll guard putting the clicked card back in sight
  readerScrolls(600);
  if (agent) panelOpenAgent = agent;
  // the card that was clicked is about to be out of sight: let it go first,
  // so the scroll guard has no hidden focus to pull back into view
  if (home.contains(document.activeElement)) document.activeElement.blur();
  setPanelTab(tab);
  // and the keyboard goes to the back button, the one control in sight
  back.focus({ preventScroll: true });
  requestAnimationFrame(() => back.focus({ preventScroll: true }));
  if (tab !== "agents") return;
  renderAgents();
  if (!agent) return;
  requestAnimationFrame(() => {
    const row = document.querySelector(`#agents .row.agent[data-id="${CSS.escape(agent)}"]`);
    if (!row) return;
    readerScrolls(600);
    row.scrollIntoView({ block: "nearest", behavior: "instant" });
  });
}

function todayCard() {
  const d = today || {};
  const s = el("section", "ph-mod");
  s.append(head("Today", "", "Usage", "stats"));
  const row = el("div", "ph-today");
  const big = el("div", "ph-big");
  const cost = today && fmtCost(today);
  const m = cost ? /^([^\d]*)(.*)$/.exec(cost) : null;
  if (m && m[1]) big.append(el("small", "", m[1]), m[2]);
  else big.textContent = cost || "—";
  const facts = el("div", "ph-facts");
  const fact = (v, label, bad) => {
    const f = el("div", "ph-fact");
    f.append(el("b", bad ? "bad" : "", v), el("span", "", t(label)));
    return f;
  };
  const hit = cacheHitOf(d);
  facts.append(fact(fmtN(d.calls || 0), "Requests"), fact(fmtN(allTokens({ input: 0, output: 0, cache_read: 0, cache_write: 0, ...d })), "Tokens"), fact(hit === null ? "—" : hitPct(hit), "Cache hit rate"));
  if (d.errors) facts.append(fact(fmtN(d.errors), "Failed", true));
  row.append(big, facts);
  const spark = el("div", "ph-spark");
  spark.setAttribute("aria-hidden", "true");
  const counts = new Array(24).fill(0), hour = new Date().getHours();
  for (const p of d.series || []) {
    const at = new Date(p.time);
    if (!isNaN(at)) counts[at.getHours()] += p.calls || 0;
  }
  const max = Math.max(0, ...counts);
  for (let i = 0; i < 24; i++) {
    const v = i > hour ? 0 : counts[i];
    const bar = el("i", !v ? "zero" : i === hour ? "now" : "");
    bar.style.height = v ? Math.max(8, Math.round((v / max) * 100)) + "%" : "2px";
    spark.append(bar);
  }
  const axis = el("div", "ph-axis");
  axis.setAttribute("aria-hidden", "true");
  for (const n of ["0", "6", "12", "18", "24"]) axis.append(el("span", "", n));
  s.append(row, spark, axis);
  if (!featureHides($("#panelRouting"))) {
    const b = el("button", "ph-route");
    b.type = "button";
    b.dataset.open = "routing";
    b.dataset.key = "route";
    b.append(el("span", "", t("Routing · {n} rerouted today", { n: d.retried || 0 })), chev());
    b.onclick = () => openTab("routing");
    s.append(b);
  }
  return s;
}

function quotaCard() {
  const hidden = new Set(state.settings?.panelUsageHidden || []);
  const subs = byUsageOrder(keyCards(quotas || []).filter((q) => q.balance || q.error || q.windows?.length))
    .filter((q) => !hidden.has(q.provider));
  if (!quotas || !subs.length) return null;
  const s = el("section", "ph-mod");
  s.append(head("Allowances", t("{n} subscriptions", { n: subs.length }), "All", "usage"));
  for (const q of subs) {
    const b = el("button", "ph-q");
    b.type = "button";
    b.dataset.key = "q:" + q.provider;
    b.onclick = () => openTab("usage");
    const name = el("span", "ph-q-name");
    name.append(quotaName(q) || q.name, el("em", "", q.plan || ""));
    const parts = [icon(q.icon || "generic"), name];
    const ws = (q.windows || []).filter((w) => !w.unlimited && !quotaNoShare(w));
    if (ws.length) {
      const tight = ws.reduce((a, w) => ((w.used || 0) > (a.used || 0) ? w : a), ws[0]);
      const pct = Math.round(quotaFill(tight));
      const bar = el("span", "ph-q-bar");
      const fill = el("i");
      fill.style.width = Math.max(0, Math.min(100, pct)) + "%";
      bar.append(fill);
      if (to100(tight.used) >= 90) bar.classList.add("full");
      const others = ws.filter((w) => w !== tight).map((w) => Math.round(quotaFill(w)) + "%");
      const say = [t(tight.name), tight.resetsAt ? resetInText(new Date(tight.resetsAt)) : "", others.join(" · ")].filter(Boolean).join(" · ");
      parts.push(el("span", "ph-q-pct", pct + "%"), bar, el("span", "ph-q-say", say));
    } else {
      parts.push(el("span", "ph-q-pct", q.balance || (q.error ? t("Failed") : "—")));
      if (!q.error) parts.push(el("span", "ph-q-say", t("Balance")));
    }
    b.append(...parts);
    s.append(b);
  }
  return s;
}

function modelField(a) {
  return a.fields?.find((x) => x.key === "model" && x.options?.length) || a.fields?.find((x) => x.key === "model");
}

function modelOf(a) {
  const f = modelField(a);
  if (!f) return "";
  return optionFor(f, f.value)?.label || f.value || "";
}

function agentCard() {
  const wired = arrangeAgents().all.filter((a) => a.wired);
  const rest = (state.agents || []).length - wired.length;
  const s = el("section", "ph-mod");
  s.append(head("Agents", t("{n} connected", { n: wired.length }), "Manage", "agents"));
  for (const a of wired) {
    // the row opens the detail; its model is a picker of its own, the same
    // one the window's row opens, so a model or a routing group is changed
    // here without leaving the panel (the pick writes through /api/set and
    // says how it takes effect, as the window's does)
    const row = el("div", "ph-agent");
    row.dataset.key = "a:" + a.id;
    const name = el("span", "ph-agent-name");
    const go = el("button", "ph-agent-go");
    go.type = "button";
    go.setAttribute("aria-label", t("Open {name}", { name: a.name }));
    go.append(el("b", "", a.name));
    go.onclick = (e) => { e.stopPropagation(); openTab("agents", a.id); };
    const f = modelField(a);
    const pickable = !!f?.options?.length;
    const model = el(pickable ? "button" : "span", "ph-agent-model");
    if (pickable) {
      model.type = "button";
      model.title = t("Pick one of owl's models: {agent} is connected and starts on it", { agent: a.name });
    }
    model.append(el("span", "", modelOf(a)));
    if (pickable) {
      const c = el("span", "chev");
      c.append(svg(CHEV, 10, 1.6));
      model.append(c);
      model.onclick = (ev) => { ev.stopPropagation(); openPicker(a, f, model, ev); };
    }
    name.append(go, model);
    const shares = today?.by?.agent || [];
    const u = shares.find((s) => s.id === a.id) || shares.find((s) => s.id === a.name) || shares.find((s) => s.name === a.name);
    const cost = u && u.cost > 0 ? fmtCost(u) : "";
    row.append(icon(a.icon), name, el("span", "ph-a-cost" + (cost ? "" : " none"), cost || "—"), chev());
    row.onclick = (e) => { if (!e.target.closest("button")) openTab("agents", a.id); };
    s.append(row);
  }
  if (rest > 0) {
    const b = el("button", "ph-rest");
    b.type = "button";
    b.dataset.key = "rest";
    b.append(el("span", "", t("The other {n} not connected", { n: rest })), chev());
    b.onclick = () => openTab("agents");
    s.append(b);
  }
  return s;
}

function draw(force) {
  if (!home) return;
  chrome();
  const onHome = document.body.dataset.ptab === "home";
  // the overview out of sight is out of the keyboard's way, and the focus
  // left on one of its cards doesn't pull the page back when a detail scrolls
  home.inert = !onHome;
  if (!onHome && home.contains(document.activeElement)) {
    document.activeElement.blur();
    back.focus({ preventScroll: true });
  }
  const sig = signature();
  if (!force && sig === drawn) { drawStatus(); return; }
  drawn = sig;
  drawStatus();
  const focused = home.contains(document.activeElement) ? document.activeElement.closest("[data-key]")?.dataset.key : "";
  const cards = [todayCard(), quotaCard(), agentCard()].filter(Boolean);
  home.replaceChildren(...cards);
  if (focused) home.querySelector(`[data-key="${CSS.escape(focused)}"]`)?.focus({ preventScroll: true });
}

function escapeHome() {
  if (document.body.dataset.ptab === "home") return false;
  setPanelTab("home");
  return true;
}

export function start() {
  if (mode !== "panel") return;
  home = el("div");
  home.id = "pHome";
  $("#panelUpdate").after(home); // the update line is the overview's first
  // a card's › leaves for a detail, so the page's scroll guard must not keep
  // the clicked card in sight: the press says so before the click is handled
  // (the guard hears clicks in the capture phase, before the card's own)
  home.addEventListener("pointerdown", () => readerScrolls(1000), true);
  home.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") readerScrolls(1000); }, true);
  window.panelHomeRefresh = () => draw();
  window.panelHomeEscape = escapeHome;
  chrome();
  readGateway();
  readToday(true);
  draw(true);
  fit();
  setInterval(() => { if (!document.hidden && document.body.dataset.ptab === "home") readToday(); }, 15e3);
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) return;
    readGateway();
    if (document.body.dataset.ptab === "home") readToday(true);
  });
  window.addEventListener("focus", () => { if (document.body.dataset.ptab === "home") readToday(); });
}