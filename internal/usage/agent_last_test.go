package usage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// An agent's group says when its newest call in the period was made
// (Group.Last), for the Usage page to fold the clients idle for days
// (WJaunita45604 on X), and — with LastCall — where its newest request
// owl carried to a provider went, for the Agents page's row saying which
// provider is in use. The other lists say nothing of either.
func TestSummaryAgentsSayTheirLastCall(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	early, late := now.AddDate(0, 0, -5), now.Add(-time.Hour)
	recs := []Record{
		{Time: late, Agent: "bun/1.3", Provider: "relay", Model: "m", ProviderAccount: "late@example.com", RouteID: 42, Input: 10, Status: 200},
		{Time: early, Agent: "bun/1.3", Provider: "other", Model: "first", ProviderAccount: "early@example.com", RouteID: 7, Input: 10, Status: 200},
		{Time: early, Agent: "1340C", Provider: "relay", Model: "m", Input: 10, Status: 200},
	}
	last := func(s Summary) map[string]time.Time {
		out := map[string]time.Time{}
		for _, g := range s.Agents {
			out[g.ID] = g.Last
		}
		return out
	}
	s := summarize(Month, now, recs)
	got := last(s)
	if !got["bun"].Equal(late) || !got["1340C"].Equal(early) {
		t.Fatalf("last calls: %v", got)
	}
	// and where that newest call went, for the agent's row
	var bun Group
	for _, g := range s.Agents {
		if g.ID == "bun" {
			bun = g
		}
	}
	if !bun.LastCall.Equal(late) || bun.LastProvider != "relay" || bun.LastModel != "m" || bun.LastAccount != "late@example.com" || bun.LastRoute != 42 {
		t.Fatalf("the newest call's place: %+v", bun)
	}
	b, _ := json.Marshal(s.Models)
	if strings.Contains(string(b), `"last"`) || strings.Contains(string(b), `"lastCall"`) {
		t.Fatalf("a model says a last call: %s", b)
	}

	// the session files' calls are ones no gateway saw: they set Last for
	// the Usage page, and nothing of a request owl carried
	rows := []Row{}
	for _, r := range recs {
		rows = append(rows, Row{Record: r, Source: "log"})
	}
	d := direct(Month, now, rows)
	got = last(d)
	if !got["bun"].Equal(late) || !got["1340C"].Equal(early) {
		t.Fatalf("session files' last calls: %v", got)
	}
	for _, g := range d.Agents {
		if !g.LastCall.IsZero() || g.LastProvider != "" || g.LastRoute != 0 {
			t.Fatalf("a session file's call says owl carried it: %+v", g)
		}
	}
}

// The newest request owl carried is the newest one it carried: one the
// agent's app reported it made itself, a search owl ran with its own key
// and owl's own call (a title, under the "owl" provider) never move it,
// though they are the agent's newest call in the log.
func TestAgentLastCallSkipsWhatOwlDidNotCarry(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	recs := []Record{
		{Time: now.Add(-4 * time.Hour), Agent: "codex", Provider: "relay", Model: "m", RouteID: 1, Input: 10, Status: 200},
		{Time: now.Add(-3 * time.Hour), Agent: "codex", Provider: "searchvendor", Model: "deep", Source: SourceSearch, RouteID: 2, Input: 10, Status: 200},
		{Time: now.Add(-2 * time.Hour), Agent: "codex", Provider: "reported", Model: "m", Source: SourceReported, RouteID: 3, Input: 10, Status: 200},
		{Time: now.Add(-time.Hour), Agent: "codex", Provider: "owl", Model: "gpt-title", RouteID: 4, Input: 10, Status: 200},
		// a try handed on to the provider that answered is no place of its own
		{Time: now.Add(-30 * time.Minute), Agent: "codex", Provider: "bad", Model: "m", RouteID: 5, Input: 10, Status: 429, HandedOn: true},
	}
	s := summarize(Today, now, recs)
	for _, g := range s.Agents {
		if g.ID != "codex" {
			continue
		}
		if !g.Last.Equal(now.Add(-30 * time.Minute)) {
			t.Fatalf("the Usage page's last call is any call: %v", g.Last)
		}
		if !g.LastCall.Equal(now.Add(-4*time.Hour)) || g.LastProvider != "relay" || g.LastRoute != 1 {
			t.Fatalf("the last request owl carried: %+v", g)
		}
		return
	}
	t.Fatal("no codex group")
}

func TestLoggedSummaryAgentsLastCallMovesOn(t *testing.T) {
	pageHome(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	holdClock(t, now)
	Append(Record{Time: now.Add(-2 * time.Hour), Agent: "1340C", Input: 3, Status: 200})
	if g := Summarize(Today).Agents; len(g) != 1 || !g[0].Last.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("first: %+v", g)
	}
	Append(Record{Time: now.Add(-time.Minute), Agent: "1340C", Input: 3, Status: 200})
	if g := Summarize(Today).Agents; len(g) != 1 || !g[0].Last.Equal(now.Add(-time.Minute)) || g[0].Calls != 2 {
		t.Fatalf("after another call: %+v", g)
	}
}

// VisitLastCalls is what the newest summary of today says of each agent's
// last request through owl, for a view that must not read the whole
// summary: published by a summary of today only, for the log it read.
func TestLastCallsFromTodaysSummary(t *testing.T) {
	pageHome(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	holdClock(t, now)
	Append(Record{Time: now.Add(-time.Hour), Agent: "codex", Provider: "relay", Model: "m", ProviderAccount: "me@example.com", RouteID: 9, Input: 3, Status: 200})
	Append(Record{Time: now.Add(-30 * time.Minute), Agent: "codex", Provider: "owl", Model: "title", RouteID: 10, Input: 3, Status: 200})
	SummarizeTop(Week, 0) // a week's summary says nothing of today
	visit := func() map[string]LastCall {
		got := map[string]LastCall{}
		VisitLastCalls(func(id string, c LastCall) { got[id] = c })
		return got
	}
	if got := visit(); len(got) != 0 {
		t.Fatalf("a week's summary published today's calls: %+v", got)
	}
	SummarizeTop(Today, 0)
	got := visit()
	c, ok := got["codex"]
	if !ok || !c.At.Equal(now.Add(-time.Hour)) || c.Provider != "relay" || c.Model != "m" || c.Account != "me@example.com" || c.Route != 9 {
		t.Fatalf("today's last call: %+v", got)
	}
}

func TestSummaryReadsCountsEverySummary(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	before := SummaryReads()
	SummarizeTop(Today, 0)
	SummarizeTop(Today, 0)
	if got := SummaryReads() - before; got != 2 {
		t.Fatalf("two summaries read counted %d", got)
	}
}
