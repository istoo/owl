// The Agents page's Install for an agent that isn't on this computer: the
// command it would run and where it comes from, asked about first; then the
// install's progress with the end of its output, and how it came out. The
// request names the agent and the command's number (POST /api/agents/install),
// never the command, so what runs is only what GET /api/agents/install
// lists. app.js imports this when Install is pressed, so it weighs nothing
// on the core.

// installFrom is where a command's install comes from: the script's own
// address, or the package manager's name
function installFrom(c) {
  const url = /https:\/\/[^\s"'|;]+/.exec(c.command);
  return c.via === "script" || c.via === "powershell" ? (url ? url[0] : c.command) : t(VIA[c.via] || c.via);
}

// installLine is a command as the install row shows it: how it runs, the
// command, and a button that copies it
function installLine(x, c) {
  const line = el("div", "ag-install-cmd");
  line.append(el("span", "ag-install-via", c.node ? t("Node.js + npm") : t(VIA[c.via] || c.via)), el("code", "", c.command), copyBtn(c.command, t("{agent}'s install command", { agent: x.name })));
  return line;
}

// agentInstall asks before running command i of x (an item of GET
// /api/agents/install), or shows the install already running
window.agentInstall = async function (x, i) {
  const ed = el("div", "editor install-ask");
  const head = el("div", "ehead");
  ed.append(head);
  const body = el("div", "ebody");
  const bar = el("div", "bar");
  ed.append(body, bar);
  confirmAsk = ed;
  openModal(ed);
  $("#modal").classList.add("lib");
  const close = el("button", "text", t("Close"));
  close.type = "button";
  close.onclick = cancelAsk;
  let now = null;
  try { now = await api("agents/install/run"); } catch { /* asked again on Install */ }
  if (now?.state === "running") return progress(now);
  // the commands owl runs itself: an npm one that wants an administrator is for a terminal
  const ok = x.commands.map((c, n) => [n, c]).filter(([, c]) => !c.admin);
  let pick = i;
  const ask = () => {
    const c = x.commands[pick];
    head.replaceChildren(el("b", "", t("Install {agent}", { agent: x.name })));
    const parts = [el("p", "lib-confirm", t("owl will run this command on this computer, as you:"))];
    if (ok.length > 1) parts.push(segs(ok.map(([n, c]) => [String(n), c.node ? t("Node.js + npm") : t(VIA[c.via] || c.via)]), String(pick), (n) => { pick = +n; ask(); }));
    parts.push(installLine(x, c), el("p", "ag-note", t("From {source}", { source: installFrom(c) })));
    if (c.via === "script" || c.via === "powershell") parts.push(el("p", "ag-note", t("The script is downloaded to a file and checked before it runs; nothing is piped into a shell.")));
    body.replaceChildren(...parts);
    const go = el("button", "text primary", t("Install"));
    go.type = "button";
    go.onclick = async (e) => {
      e.stopPropagation();
      go.disabled = true;
      try { progress(await api("agents/install", { id: x.id, index: pick })); } catch (err) { go.disabled = false; statusTErr(err); }
    };
    const cancel = el("button", "text", t("Cancel"));
    cancel.type = "button";
    cancel.onclick = cancelAsk;
    bar.replaceChildren(el("span", "grow"), cancel, go);
    go.focus(NO_SCROLL);
  };
  ask();

  // progress shows the install as it stands until it ends, on a second's poll
  async function progress(run) {
    const out = el("pre");
    const box = el("div", "call-body");
    box.hidden = true;
    box.append(out);
    const say = el("p", "lib-confirm");
    const toggle = el("button", "text", t("Show output"));
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", "false");
    toggle.onclick = () => {
      box.hidden = !box.hidden;
      toggle.textContent = box.hidden ? t("Show output") : t("Hide output");
      toggle.setAttribute("aria-expanded", String(!box.hidden));
    };
    const stop = el("button", "text", t("Cancel install"));
    stop.type = "button";
    stop.onclick = () => { stop.disabled = true; api("agents/install/cancel", {}).catch(statusTErr); };
    const run1 = x.commands[run.index] || x.commands[pick];
    let shown = false;
    const draw = (r) => {
      if (!ed.isConnected) return;
      const name = r.name || x.name;
      head.replaceChildren(el("b", "", r.state === "running" ? t("Installing {agent}…", { agent: name }) : t("Install {agent}", { agent: name })));
      out.textContent = r.tail || "";
      out.scrollTop = out.scrollHeight;
      say.classList.toggle("err", r.state === "failed");
      say.textContent = r.state === "running" ? t("Installing {agent}…", { agent: name })
        : r.state === "done" ? t("{agent} is installed", { agent: name })
        : r.state === "canceled" ? t("Installing {agent} was canceled", { agent: name })
        : t("Installing {agent} failed: {why}", { agent: name, why: t(r.error || "") });
      const parts = [say, toggle, box];
      if (r.state === "failed" || r.state === "canceled") {
        // the command to run in a terminal, as the list shows it
        parts.push(el("p", "ag-note", t("Run it yourself in a terminal:")), installLine(x, run1 || { via: r.via, command: r.command }));
        if (r.state === "failed" && !shown) { shown = true; box.hidden = false; toggle.textContent = t("Hide output"); toggle.setAttribute("aria-expanded", "true"); }
      }
      body.replaceChildren(...parts);
      bar.replaceChildren(el("span", "grow"), r.state === "running" ? stop : close);
      if (r.state !== "running") close.focus(NO_SCROLL);
    };
    draw(run);
    while (run.state === "running") {
      await new Promise((r) => setTimeout(r, 1000));
      try { run = await api("agents/install/run"); } catch { continue; }
      draw(run);
    }
    // it has ended: what is on this computer is looked for again, so the agent is listed with the others
    if (run.state === "done") {
      try { await refreshPage("agents"); } catch (err) { statusTErr(err); }
    }
  }
};
