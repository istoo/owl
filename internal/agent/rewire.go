package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/settings"
)

// The gateway's port is set in Settings (Magic_zero on Discord): every
// agent owl connected has the gateway's URL written into its config, so
// moving the gateway moves them too. Before the port changes, OnGateway
// notes which agents reach the gateway where it is; once it has moved,
// Rewire sets each of them again, which writes the URL it has now.

// OnGateway are the agents connected to owl whose configs reach the
// gateway at its address now: none of their wiring is off.
func OnGateway() []*Agent {
	var out []*Agent
	for _, a := range All() {
		if len(a.Fields) == 0 || !a.Wired() {
			continue
		}
		if d := a.Drift(); d != nil && d.Kind == "unwired" {
			continue // taken off owl before: the user's to set again
		}
		out = append(out, a)
	}
	return out
}

// Rewire puts the gateway's address now into the configs of agents, taken
// by OnGateway before it moved: each one whose wiring is off now is set
// again, an agent joined to owl beside its own models (Codex signed in
// with ChatGPT) joined again. It says the names of those it moved, and
// what one it couldn't move failed with. Cursor Private Inference's
// variables and dsh's routes, which owl keeps on its own, follow too;
// Claude Science's daemon, started for the old address, is drift
// (scienceCheck) and is started again with the rest.
func Rewire(as []*Agent) (moved []string, err error) {
	var off []*Agent
	for _, a := range as {
		if d := a.Drift(); d != nil && d.Kind == "unwired" {
			off = append(off, a)
		}
	}
	moved, errs := setAgain(off)
	KeepCursorLocalEnv(context.Background())
	if trouble := dshWiredOnce(); trouble != "" {
		errs = append(errs, fmt.Errorf("%s", trouble))
	}
	return moved, joinErrs(errs)
}

// Rekey sets again those of as, taken by OnGateway before LAN sharing was
// turned on or off or its key made anew, that reach the gateway from beyond
// loopback (an agent in a WSL distro under NAT): the key they send there is
// the sharing key while sharing is on (keyAt), so it changed with it. One
// connected while nothing was shared kept gateway.Token, which the gateway
// turns away from beyond loopback, after sharing was turned on for it.
func Rekey(as []*Agent) (moved []string, err error) {
	var away []*Agent
	for _, a := range as {
		if a.Gateway != nil && (onAnotherMachine(a.Gateway()) || a.ID == "claude-desktop" && settings.Shared().DesktopGateway != "") {
			away = append(away, a)
		}
	}
	moved, errs := setAgain(away)
	return moved, joinErrs(errs)
}

// setAgain sets each agent's wiring again, keeping what it had before
// owl, and says the names of those it set and what one failed with.
func setAgain(as []*Agent) (moved []string, errs []error) {
	for _, a := range as {
		// set again on the old URL, an agent reads as not routed through
		// owl yet, and its set notes the old URL, owl's token and
		// model as what it had before owl: what it really had is kept,
		// for switching it off to put back
		was := stashLoad()
		e := reset(a)
		keepOriginals(a, was)
		if e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Name, e))
			continue
		}
		if a.Sync != nil {
			_ = a.Sync()
		}
		moved = append(moved, a.Name)
	}
	return moved, errs
}

// keepOriginals puts back the agent's own settings from before owl, which
// setting it again can take for owl's and write over: a value owl had at
// the address it was wired at (its URL, one of its keys, a model of its
// own) is not what the user had. It reads and writes the stash once, under
// stashMu, so a change another agent made meanwhile is kept; the address
// owl wrote (wiredAt, which follows the move), a plugin's record of what it
// wrote (its new key and value) and every other agent's keys stay as the
// set left them.
func keepOriginals(a *Agent, was map[string]string) {
	if len(was) == 0 {
		return
	}
	addresses := make([]string, 0, 1)
	for k, v := range was {
		if strings.HasSuffix(k, wiredAt) && v != "" {
			addresses = append(addresses, v)
		}
	}
	stashMu.Lock()
	defer stashMu.Unlock()
	now := stashLoad()
	changed := false
	for k, old := range was {
		if old == "" || !originKey(a, k) {
			continue
		}
		cur, ok := now[k]
		if !ok || cur == old || !owlWrote(cur, addresses) {
			continue
		}
		now[k] = old
		changed = true
	}
	if changed {
		stashWrite(now)
	}
}

// originKey says whether key holds a value of the agent's own from before
// owl: it is the agent's (its id, its kind, or the id a WSL row is the
// twin of), not another agent's, and not what owl keeps of itself there.
func originKey(a *Agent, key string) bool {
	// a plugin's record holds what owl last wrote (Key, Wrote) beside the
	// user's originals: the set has just renewed it, so it stays
	if strings.HasPrefix(key, "plugin:") {
		return false
	}
	// the address owl wrote follows the move
	if strings.HasSuffix(key, wiredAt) {
		return false
	}
	ids := []string{a.ID}
	if kind, _, ok := strings.Cut(a.ID, "@wsl:"); ok {
		ids = append(ids, kind)
	}
	if kind, _, ok := strings.Cut(a.ID, "#"); ok {
		ids = append(ids, kind)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if key == id || strings.HasPrefix(key, id+".") || strings.HasPrefix(key, id+":") || strings.HasPrefix(key, id+"-") {
			return true
		}
	}
	return false
}

// owlWrote says v is something owl had written into an agent's config
// before it was set again: one of the gateway's keys, a model of owl's, or
// the gateway at an address it wrote (addresses).
func owlWrote(v string, addresses []string) bool {
	if ourKey(v) || gatewayKey(v) || v == owlID || prefixed(v) || isOwl(v) {
		return true
	}
	if !strings.Contains(v, "://") {
		return false
	}
	if sameHost(v, gateway.URL()) {
		return true
	}
	for _, addr := range addresses {
		if sameHost(v, addr) {
			return true
		}
	}
	return false
}

// joinErrs is errs as one error, "; " between them; nil for none.
func joinErrs(errs []error) (err error) {
	if len(errs) > 0 {
		err = errs[0]
		for _, e := range errs[1:] {
			err = fmt.Errorf("%w; %w", err, e)
		}
	}
	return err
}

// reset sets an agent's wiring again: one joined beside its own models
// joined again, any other's fields set again.
func reset(a *Agent) error {
	if a.Native != nil {
		return a.Native.Connect()
	}
	if a.Join != nil && a.Joined != nil && a.Joined() {
		ok, err := a.Join()
		if err != nil || ok {
			return err
		}
	}
	return a.Reapply()
}

// wiredAt ends a stash key holding the gateway address owl last wrote
// into an agent's config, which a move of the gateway updates.
const wiredAt = ".wired_at"
