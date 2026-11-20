// Providers' Check all: one model-list request each to the vendors that
// are on, four at a time, with the provider's own key, headers and proxy.
// The result is drawn on its row until the page is reloaded: it is a
// look, not a setting, and never changes a route, an account's rest or
// providers.json (provider.CheckHealth). app.js keeps only the click that
// loads this file and the call that puts the results back after a redraw.
const results = new Map(); // id -> {kind, ms, status, error}, this session's
let running = null; // { stop, requests: Map(run -> AbortController) }

const button = () => document.getElementById("pvHealth");
const onProviders = () => (providers?.providers || []).filter((p) => !p.off);
const hostOf = (p) => {
  try { return new URL(p.chat || p.responses || p.anthropic || "").host; } catch { return p.host || ""; }
};

// the button's words follow its state, in the page's language, and a
// language change re-reads them from data-en (translateStatic)
function says(en) {
  const s = button()?.firstElementChild;
  if (s) { s.dataset.en = en; s.textContent = t(en); }
}
function saysTip(en) {
  const b = button();
  if (b) { b.dataset.tt = en; delete b.dataset.enTitle; b.title = t(en); }
}
function sync() {
  const b = button();
  if (!b) return;
  b.disabled = !running && !onProviders().length;
  b.setAttribute("aria-busy", running ? "true" : "false");
  says(running ? "Stop checking" : "Check all");
  saysTip(running ? "Click to stop the checks under way"
    : "Check every provider that is on: one small model-list request each, with its saved key and proxy");
}

function short(r) {
  switch (r.kind) {
    case "ok": return t("{ms} ms", { ms: r.ms });
    case "auth": return t("Key refused");
    case "network": return t("Unreachable");
    case "unsupported": return t("Can't check");
    case "wait": return t("Checking…");
    default: return t("Error {status}", { status: r.status || "?" });
  }
}

// detail is what the row says on hover: the next thing to look at, which
// differs by kind. The vendor's own words follow it, untouched.
function detail(p, r) {
  let said;
  switch (r.kind) {
    case "ok": said = t("Answered in {ms} ms; the key was accepted", { ms: r.ms }); break;
    case "auth": said = t("The provider answered {status}: it refused the key. Paste a current key in this provider's settings", { status: r.status }); break;
    case "network": said = t("Couldn't reach {host}. Check the provider's address and proxy", { host: hostOf(p) }); break;
    case "unsupported": return t("This sign-in has no model list owl can ask on its own: open the provider's editor and use Test");
    case "wait": return t("Asking this provider's model list with its saved key and proxy");
    default: said = t("The provider answered {status}. Its API may be down: try again later, or check the address in this provider's settings", { status: r.status }); break;
  }
  return r.error ? said + " — " + r.error : said;
}

// drawRow puts p's result before its key pill, if the row is drawn. A row
// redrawn by the page gets it back from rows().
function drawRow(p) {
  const row = document.querySelector(`#providers > .row.provider[data-id="${CSS.escape(p.id)}"], #offProviders > .row.provider[data-id="${CSS.escape(p.id)}"]`);
  if (!row) return;
  let pill = row.querySelector(".pv-health");
  const r = results.get(p.id);
  if (!r) { pill?.remove(); return; }
  if (!pill) {
    pill = el("span", "pv-health");
    const key = row.querySelector(".key");
    if (key) row.insertBefore(pill, key); else row.append(pill);
  }
  pill.className = "pv-health " + r.kind;
  pill.textContent = short(r);
  pill.title = detail(p, r);
}

// rows is called by app.js after it draws the list again.
export function rows() {
  for (const p of onProviders()) drawRow(p);
  sync();
}
// app.js calls it after each redraw of the list, so a result outlives one
window.pvHealthRows = rows;

async function one(p) {
  const run = Math.random().toString(36).slice(2), ac = new AbortController();
  running.requests.set(run, ac);
  let r = null;
  try {
    const a = await api("provider/test", { id: p.id, health: true, run }, null, ac.signal);
    r = { kind: a?.kind || "server", ms: a?.ms || 0, status: a?.status || 0, error: a?.error || "" };
  } catch (e) {
    // a stop took it, or the ask itself failed before a kind came back
    if (!ac.signal.aborted) r = { kind: "network", ms: 0, status: 0, error: e?.message || "" };
  }
  running?.requests.delete(run);
  if (!running) return; // stopped: its row has gone back to nothing
  if (r) { results.set(p.id, r); drawRow(p); }
  sync();
}

export async function toggle() {
  if (running) return stop();
  const queue = onProviders(), total = queue.length;
  if (!total) return;
  results.clear();
  running = { stop: false, requests: new Map() };
  for (const p of queue) { results.set(p.id, { kind: "wait", ms: 0, status: 0, error: "" }); drawRow(p); }
  sync();
  status(t("Checking {n} providers…", { n: total }));
  const again = running;
  const worker = async () => {
    while (!again.stop) {
      const p = queue.shift();
      if (!p) return;
      await one(p);
    }
  };
  await Promise.all(Array.from({ length: Math.min(4, total) }, worker));
  if (running !== again) return; // a stop has cleared it
  running = null;
  const bad = [...results.values()].filter((r) => r.kind === "auth" || r.kind === "network" || r.kind === "server").length;
  const skipped = [...results.values()].filter((r) => r.kind === "unsupported").length;
  sync();
  if (skipped) status(t("{n} can't be checked here: open each one's editor and use Test", { n: skipped }));
  else if (bad) status(t("Checked {n}: look at {bad}", { n: total, bad }), "err");
  else status(t("Checked {n}: every one answered", { n: total }), "ok");
}

function stop() {
  if (!running) return;
  const was = running;
  was.stop = true;
  for (const [run, ac] of was.requests) {
    ac.abort();
    api("provider/teststop", { run }).catch(() => {});
  }
  was.requests.clear();
  if (running === was) running = null;
  for (const p of onProviders()) { results.delete(p.id); drawRow(p); }
  sync();
  status(t("Check stopped"));
}