// Settings' Advanced fold: each part shows the rows most readers touch, and
// the rest sit in a closed fold at its foot (the owner: 产品要简单好用).
// Nothing is removed and no default changes; a row only moves.
//
// The fold is the provider editor's (details.more, its summary and its
// click, app.js's foldMoves), and it follows the editor's rule: it is open
// while any row in it is set to something other than its default, so a
// setting the reader changed is never hidden; shut or opened by hand it
// stays so until the page is loaded again. Settings' search opens it while
// a row in it matches (findSettings), and a link to a row in it opens it
// too (openSettingsAt, through shown()).
//
// Rows are drawn by many functions that empty their list and draw it
// again, so the rows are found where they are drawn and moved after each
// draw, before the page is painted; a row drawn again replaces the one it
// moved the last time (data-adv-from). A row that is drawn once in
// index.html, or a whole list, is moved as it is.
const FOLDS = {
  general: {
    set: (s) => s.noBird || fontChoices(s).uiFont || fontChoices(s).codeFont || s.gatewayMode || s.plainNames || s.plainOwnNames, // a browser tab keeps its fonts itself
    put: [{ row: "#birdRow" }, { row: "#uiFontRow" }, { row: "#codeFontRow" }, { row: "#gatewayModeRow" }, { row: "#plainNamesSegs" }],
  },
  usage: {
    set: (s) => s.quotaLeft || s.quotaReads || s.resetReminder || s.codexWarmup || s.claudeWarmup || s.codexWarmAts?.length || s.claudeWarmAts?.length
      || s.workbuddyCheckin || s.traeCheckin || s.minimaxCheckin || s.qoderCheckin,
    put: [{ row: "#quotaLeftSegs" }, { row: "#quotaReadsRow" }, { row: "#resetReminderRow" }, { head: "#setPage-usage .warm-head", list: "#setPage-usage .warm-panes" }],
  },
  network: {
    set: (s) => s.bareModelIDs || s.busyLoad || s.githubTokenFrom === "settings" || s.corsOrigins?.length,
    put: [{ row: "#bareIdsRow" }, { row: "#busyLoadRow" }, { whole: "#githubList" }, { whole: "#corsList" }],
  },
  models: {
    set: (s) => s.imageTimeout > 0 || s.searchAll || s.replyModel || s.noLoopGuard || (s.refusalRetries != null && s.refusalRetries >= 0)
      || s.classifyTimeout > 0 || s.claudeLeanPrompt || s.claudePrintMode || s.disabledModels?.length,
    put: [{ list: "#imagesList", ids: ["imageTimeoutRow", "visionMcpRow", "imageMcpRow"] }, { list: "#searchList", ids: ["searchAllRow", "searchMcpRow"] },
      { whole: "#replyList" }, { whole: "#disabledModelsList" }],
  },
  privacy: {
    set: (s) => s.redactWords?.length || s.redactWordsAnyCase || s.redactWordsWhole || s.redactRules?.length || Object.keys(s.redactActions || {}).length
      || s.redactAllow?.length || s.signInLinkOnly || sessionsMixed(),
    put: [{ list: "#findList", ids: ["maskRulesRow"] }, { list: "#redactList", from: "Masked words" }, { whole: "#maskActList" },
      { whole: "#signInOpenList" }, { whole: "#sessionReadsList" }],
  },
  about: {
    set: (s) => (s.updateEvery && s.updateEvery !== 360) || s.updateChannel === "stable" || s.noAutoDownload || s.autoRestart || s.updateMirror,
    put: [{ list: "#about", names: ["Check every", "Update channel", "Download updates automatically", "Restart automatically", "Download source", "Status line on another computer"] }],
  },
};

// the agents' session-history rows (Privacy) set apart from one another:
// some read, some not, which no default gives
const sessionsMixed = () => {
  const on = [...document.querySelectorAll("#sessionReadsList .row[data-agent] .opt.on")].map((b) => b.textContent === t("On"));
  return on.includes(true) && on.includes(false);
};

// a row's name in English, whichever language is shown
let enFor = "", enMap = null;
const enOf = (text) => {
  const words = I18N[locale];
  if (!words) return text;
  if (enFor !== locale) { enFor = locale; enMap = new Map(Object.entries(words).map(([k, v]) => [v, k])); }
  return enMap.get(text) || text;
};
const nameOf = (row) => {
  const n = row.querySelector(":scope > .who > .name");
  return n ? n.dataset.en || enOf(n.textContent.trim()) : "";
};

const manual = {}; // tab → what the reader did to its fold: open or shut
const emptied = new Set(); // the lists a row was moved out of
const folds = {};

function foldOf(tab) {
  if (folds[tab]) return folds[tab];
  const d = el("details", "more set-adv");
  d.dataset.tab = tab;
  // the rows that came out of a list of their own sit together in one list
  const inner = el("div", "inner"), loose = el("div", "list prefs");
  inner.append(loose);
  d.append(el("summary", "", t("Advanced")), inner);
  d.onfold = (open) => { manual[tab] = open; };
  $("#setPage-" + tab).append(d);
  return (folds[tab] = d);
}

const shownIn = (row, top) => {
  for (let n = row; n && n !== top; n = n.parentElement) if (n.hidden) return false;
  return !featureHides(row);
};

function settle() {
  const s = prefs || {};
  for (const tab in FOLDS) {
    const page = $("#setPage-" + tab);
    if (!page) continue;
    const d = foldOf(tab), inner = d.querySelector(":scope > .inner"), loose = inner.firstElementChild;
    const rows = [], others = [];
    const take = (n) => {
      if (!n || rows.includes(n) || others.includes(n)) return;
      if (n.parentElement !== loose && n.parentElement?.matches(".list")) emptied.add(n.parentElement);
      (n.matches(".row") ? rows : others).push(n);
    };
    for (const u of FOLDS[tab].put) {
      if (u.row) take($(u.row)?.closest(".row"));
      else if (u.whole || u.head) {
        const list = $(u.whole || u.list);
        const head = u.head ? $(u.head) : list?.previousElementSibling;
        if (head?.matches(".row-head")) take(head);
        take(list);
      } else {
        const list = $(u.list);
        if (!list) continue;
        const kids = [...list.children].filter((r) => r.matches(".row"));
        const at = u.from ? kids.findIndex((r) => nameOf(r) === u.from) : -1;
        const fresh = u.from ? (at < 0 ? [] : kids.slice(at)) : kids.filter((r) => (u.ids ? u.ids.includes(r.id) : u.names.includes(nameOf(r))));
        // drawn again: what was moved last time is the old drawing
        const old = [...loose.children].filter((r) => r.dataset.advFrom === u.list);
        for (const r of fresh) r.dataset.advFrom = u.list;
        for (const r of fresh.length ? fresh : old) take(r);
      }
    }
    const put = (box, want) => {
      if (want.length === box.children.length && want.every((n, i) => box.children[i] === n)) return;
      // a field being typed in keeps its focus through the move
      const on = document.activeElement;
      box.replaceChildren(...want);
      if (on && on !== document.activeElement && box.contains(on)) on.focus({ preventScroll: true });
    };
    put(loose, rows);
    put(inner, [loose, ...others]);
    loose.toggleAttribute("data-adv-empty", !rows.length);
    for (const l of emptied) l.toggleAttribute("data-adv-empty", !l.children.length);
    d.querySelector(":scope > summary").textContent = t("Advanced");
    d.hidden = ![...inner.querySelectorAll(".row")].some((r) => shownIn(r, inner));
    // opened by the setting that is set, unless the reader has shut it
    if (!d.hidden && manual[tab] === undefined && !d.open && FOLDS[tab].set(s)) d.open = true;
  }
}

const view = $("#view-settings");
const watch = new MutationObserver(() => again());
// settled with nothing left to look at: the moves it makes are its own
function again() {
  watch.disconnect();
  settle();
  watch.takeRecords();
  watch.observe(view, { childList: true, subtree: true });
}
// a draw of Settings is settled as it ends, so no frame, no microtask and no
// reader of the page sees a row both where it was drawn and where it was
// moved; what else draws a row (an answer coming in) is caught by the observer
{
  const draw = renderSettings;
  renderSettings = function (...a) {
    const r = draw.apply(this, a);
    again();
    return r;
  };
}
again();
