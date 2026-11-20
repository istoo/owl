package gui

import (
	"cmp"
	"strings"
	"time"

	"github.com/istoo/owl/internal/gateway"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/usage"
)

// agentLiveJSON is what an agent's row says of its own requests: the last
// one owl carried to a provider and where it went. The row's line is drawn
// by agentlive.js, which adds today's spend from the page or panel that
// already reads it: the whole usage summary is never part of /api/state.
type agentLiveJSON struct {
	// At is when the request was made, and Route its id (Record.RouteID):
	// the identity a view matches it by, rather than the time alone.
	At       time.Time `json:"at,omitzero"`
	Route    int64     `json:"route,omitempty"`
	Provider string    `json:"provider,omitempty"` // the provider's name
	Model    string    `json:"model,omitempty"`    // the model it served
	Account  string    `json:"account,omitempty"`  // the account that answered
	// Rerouted: the last request was answered by another provider or
	// account than the one it went to first (the gateway's fallbacks)
	Rerouted *agentRerouteJSON `json:"rerouted,omitempty"`
}

type agentRerouteJSON struct {
	From string `json:"from"`          // the provider and account asked first
	Why  string `json:"why,omitempty"` // why it was passed over
}

// agentLives is each agent's live line by id: the newest request owl
// carried to a provider for it. The gateway's recent calls ring is the
// first source, since it also holds the fallbacks that explain a reroute;
// behind it is what the newest summary of today said (VisitLastCalls),
// which survives a restart and costs no work here. The ring answers for a
// call the summary also names, since only it carries the fallbacks. A request owl refused before
// asking any provider, one the agent's app reported it made itself, a
// search owl ran with its own key and owl's own calls (a title, a vision
// description) are not requests owl carried for the agent and say nothing
// of the provider in use. Empty for an agent with no such request today.
func agentLives() map[string]*agentLiveJSON {
	calls := recentCalls()
	names := providerDisplayNames()
	start := usage.Today.Since(usage.Clock())
	out := map[string]*agentLiveJSON{}
	usage.VisitLastCalls(func(id string, c usage.LastCall) {
		if c.At.Before(start) {
			return // the summary predates midnight
		}
		if l := liveOf(c.Route, c.At, c.Provider, c.Model, c.Account, names); l != nil {
			out[id] = l
		}
	})
	for _, c := range calls { // newest first
		if c.Time.Before(start) {
			continue // a call from before midnight is not today's
		}
		if c.Agent == "" || c.Kind == "web_search" || c.Kind == "vision" || c.Kind == "routing" {
			continue
		}
		if old := out[c.Agent]; old != nil {
			// The ring's call is the one that knows the fallbacks: for the
			// same request (its route) it answers even when the summary
			// names it at the same instant, as a summary read after the
			// call does. An older ring call never takes a newer one's place.
			same := c.Route != 0 && c.Route == old.Route
			if !same && !c.Time.After(old.At) {
				continue
			}
		}
		l := liveOf(c.Route, c.Time, c.Provider, cmp.Or(c.ServedModel, c.Model), c.Account, names)
		if l == nil {
			continue // refused before it reached a provider
		}
		l.Rerouted = rerouteOf(c.Fallback, names)
		out[c.Agent] = l
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// recentCalls is the gateway's ring, newest first, without bodies.
func recentCalls() []gateway.Call {
	if gw := served.Load(); gw != nil {
		return gw.RecentMeta()
	}
	return nil
}

// liveOf is an agent's live line for a request that went to providerID;
// nil when it names no provider, or names owl's own ("owl", which a
// title's call does), which is no provider the agent's requests reached.
func liveOf(route int64, at time.Time, providerID, model, account string, names map[string]string) *agentLiveJSON {
	if providerID == "" || providerID == "owl" {
		return nil
	}
	l := &agentLiveJSON{At: at, Route: route, Provider: shownProvider(names, providerID), Model: model}
	if account != "" {
		l.Account = provider.AccountShownOf(providerID, account)
	}
	return l
}

// providerDisplayNames is each provider's name by id, where it has one:
// the name the Providers page shows it by.
func providerDisplayNames() map[string]string {
	out := map[string]string{}
	for _, p := range provider.All() {
		if p.Name != "" {
			out[p.ID] = p.Name
		}
	}
	return out
}

// shownProvider is a provider id as the page names it: the provider set up
// under it, else the name owl knows it by (one removed since), else its id.
func shownProvider(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	if n, _ := provider.KnownFace(id); n != "" {
		return n
	}
	return id
}

// rerouteOf is the first provider a request was passed over from and why,
// as Call.Fallback tells it: "<id or id (account)>: <why>; …". nil when
// nothing was passed over.
func rerouteOf(fallback string, names map[string]string) *agentRerouteJSON {
	first, _, _ := strings.Cut(fallback, "; ")
	label, why, ok := strings.Cut(first, ": ")
	if !ok || strings.TrimSpace(label) == "" {
		return nil
	}
	from := label
	// "claude (me@example.com)": the provider's name as the page shows it,
	// and the account as the user named it
	if i := strings.Index(label, " ("); i > 0 && strings.HasSuffix(label, ")") {
		id, user := label[:i], label[i+2:len(label)-1]
		from = shownProvider(names, id) + " (" + provider.AccountShownOf(id, user) + ")"
	} else {
		from = shownProvider(names, label)
	}
	why = strings.TrimSpace(why)
	if i := strings.Index(why, ". "); i > 0 {
		why = why[:i] // the first sentence, which says what failed
	}
	if r := []rune(why); len(r) > 140 {
		why = strings.TrimSpace(string(r[:140])) + "…"
	}
	return &agentRerouteJSON{From: from, Why: why}
}
