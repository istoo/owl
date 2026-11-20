// The first step of the Get started card, when this computer already has a
// key or a relay another agent uses: the entry that opens the import dialog
// on exactly what discovery found, for a preview and a confirm. app.js
// imports this when the card first draws with no model source, so the
// window's core carries only that one import.
export function localEntry(body, fresh) {
  const imp = el("button", "text guide-local", t("Import local configurations ({n})…", { n: fresh.length }));
  imp.type = "button";
  imp.title = [...new Set(fresh.map((c) => c.source))].join(" · ");
  imp.onclick = (e) => { e.stopPropagation(); importThese(fresh); };
  body.append(imp);
}

// The window shows the dialog on Providers, where it lives; the panel
// can't draw it, so it opens the window there, as its Add a provider does.
export async function importThese(fresh) {
  if (mode === "panel") { api("window/main?view=providers", {}).catch(statusErr); return; }
  if (!await show("providers")) return;
  if (!providers) { try { await loadProviders(); } catch { return; } }
  openImportApps(new Set(fresh.map((c) => c.fingerprint)));
}