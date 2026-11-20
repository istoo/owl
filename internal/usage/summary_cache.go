package usage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

const summaryCacheSessions = 1024

type summaryEntry struct {
	version uint64
	meta    string
	// value keeps the top summaryCacheSessions sessions only, of its
	// SessionCount
	value Summary
}

// summaryKey is a period as read in a zone: each reader's zone (Zone) has
// days of its own, and readers in two zones don't put each other's out.
type summaryKey struct {
	p    Period
	zone *time.Location
}

var summaries struct {
	sync.Mutex
	entries map[summaryKey]summaryEntry
	worked  int // how many summaries were worked out, for the tests
}

// LastCall is where an agent's newest call of today went, as the newest
// summary of today names it.
type LastCall struct {
	At       time.Time
	Route    int64 // the request's id (Record.RouteID)
	Provider string
	Model    string
	Account  string
}

type lastCallsEntry struct {
	path  string // the log it was read from
	calls map[string]LastCall
}

// lastCalls is what the newest summary of today said of each agent's last
// call. A view that must not read the whole summary (the Agents page's
// state, every refresh) reads this instead: the summary is already worked
// out at start (ReadAhead) and by the Usage page, so most of the time it
// costs nothing. It is keyed by the log's path, so a test's or a changed
// home's log never answers for another.
var lastCalls atomic.Pointer[lastCallsEntry]

// SummaryReads counts the summaries read since the process started, for
// the tests that check a view leaves the whole summary alone.
var summaryReads atomic.Int64

// SummaryReads is how many summaries have been read: a view not reading
// the whole usage summary leaves it unchanged.
func SummaryReads() int64 { return summaryReads.Load() }

// VisitLastCalls calls fn for each agent's newest call of today, id to
// call, as the log at Path was last summed, and not at all when today has
// not been summed for it. The map stays the package's: a view reads what
// it is told, never keeps or changes it.
func VisitLastCalls(fn func(id string, c LastCall)) {
	e := lastCalls.Load()
	if e == nil || e.path != Path() {
		return
	}
	for id, c := range e.calls {
		fn(id, c)
	}
}

// noteLastCalls keeps what a summary of today says of each agent's last
// call for VisitLastCalls. A summary of any other period says nothing of
// today.
func noteLastCalls(p Period, snapshot *logSnapshot, s Summary) Summary {
	if p != Today {
		return s
	}
	m := make(map[string]LastCall, len(s.Agents))
	for _, g := range s.Agents {
		if g.LastCall.IsZero() {
			continue
		}
		m[g.ID] = LastCall{At: g.LastCall, Route: g.LastRoute, Provider: g.LastProvider, Model: g.LastModel, Account: g.LastAccount}
	}
	lastCalls.Store(&lastCallsEntry{path: snapshot.path, calls: m})
	return s
}

func indexedSummary(p Period, now time.Time) Summary { return topSummary(p, now, -1) }

// ReadAhead parses the log into its index and works out today's summary in
// this computer's zone, for owl to run as it starts: the first reader,
// the tray panel's line of today's calls, finds both done. Parsing the log
// cold took 0.33 s at 2 MB and 0.77 s at 40 MB on one core, and the
// panel's numbers waited for it on the first open after a start
// (CyberCaius on Discord). A reader that comes while it runs waits for the
// same parse, never a second.
func ReadAhead() { SummarizeTopIn(Today, summaryCacheSessions, nil) }

// SummarizeTop is Summarize with only the top n sessions by tokens (all of
// them for n < 0); SessionCount says how many the period has. It is what a page that
// shows a few of them or none asks for: a summary of more than
// summaryCacheSessions sessions is kept with those only, and Summarize, which
// gives every one, works it out again each time. Over a long history
// (Vantiboo1ean, ~750k requests in ~84k sessions) that was every refresh of
// the Usage page and the TUI, and the page was sent all 84k.
func SummarizeTop(p Period, n int) Summary { return topSummary(p, localNow(), n) }

// SummarizeTopIn is SummarizeTop with the period's days and the chart's
// hours read in zone (Zone gives it), the reader's: the Usage page's.
func SummarizeTopIn(p Period, n int, zone *time.Location) Summary {
	if zone == nil {
		zone = Zone("")
	}
	return topSummary(p, Clock().In(zone), n)
}

func topSummary(p Period, now time.Time, n int) Summary {
	summaryReads.Add(1)
	p = p.shown()
	snapshot := readLogSnapshot()
	work := func() Summary {
		summaries.Lock()
		summaries.worked++
		summaries.Unlock()
		return summarizeFrom(p, now, snapshot.first, snapshot.keyProviders, func(fn func(Record)) { snapshot.visit(p.Since(now), fn) })
	}
	if p.IsRange() || p.Last() > 0 {
		// a picked range is read afresh each time, so ranges never pile up
		// in the cache (#1492), and so is a rolling window, whose start
		// moves with the clock though the log hasn't changed (#2075)
		return topOf(noteLastCalls(p, snapshot, work()), n)
	}
	meta := usagePriceCacheKey(now)
	summaries.Lock()
	k := summaryKey{p, now.Location()}
	old, ok := summaries.entries[k]
	summaries.Unlock()
	if ok && old.version == snapshot.version && old.meta == meta && (n >= 0 && n <= len(old.value.Sessions) || old.value.SessionCount == len(old.value.Sessions)) {
		return topOf(noteLastCalls(p, snapshot, cloneSummary(old.value)), n)
	}
	summaries.Lock()
	summaries.worked++
	summaries.Unlock()
	value := foldSummary(k, p, now, snapshot, meta)
	// the cache keeps a copy with the top sessions only; the answer has as
	// many as asked
	kept := value
	kept.Sessions = kept.Sessions[:min(len(kept.Sessions), summaryCacheSessions)]
	kept = cloneSummary(kept)
	summaries.Lock()
	if summaries.entries == nil {
		summaries.entries = map[summaryKey]summaryEntry{}
	}
	if previous := summaries.entries[k]; previous.version <= snapshot.version {
		summaries.entries[k] = summaryEntry{snapshot.version, meta, kept}
	}
	summaries.Unlock()
	return topOf(noteLastCalls(p, snapshot, value), n)
}

// topOf is s with its top n sessions (all for n < 0).
func topOf(s Summary, n int) Summary {
	if n >= 0 && len(s.Sessions) > n {
		s.Sessions = s.Sessions[:n:n]
	}
	return s
}

// The exported result remains owned by its caller.
func cloneSummary(s Summary) Summary {
	s.Series = slices.Clone(s.Series)
	s.Agents = slices.Clone(s.Agents)
	s.Models = slices.Clone(s.Models)
	s.ProviderKeys = slices.Clone(s.ProviderKeys)
	s.Accounts = slices.Clone(s.Accounts)
	s.CallerKeys = slices.Clone(s.CallerKeys)
	s.Sessions = slices.Clone(s.Sessions)
	return s
}

// usagePriceCacheKey invalidates priced views when local prices or provider feeds change.
func usagePriceCacheKey(now time.Time) string {
	// one catalog for the providers and the group names read below (#5978)
	defer provider.Hold()()
	_, offset := now.Zone()
	meta := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d", statKey(settings.Path()), statKey(provider.Path()), catalog.CacheKey(), liveListKey("antigravity"), now.Format("2006-01-02"), now.Location(), offset)
	// Sign-ins and plugin lists can change independently of providers.json.
	// Their identity and price feeds affect both grouping and historical costs.
	h := sha256.New()
	st := settings.Load()
	prices, _ := json.Marshal(st.ModelPrices)
	aliases := provider.UsageRenames().Key()
	// and the names models' usage is counted under, which the groups key on
	fmt.Fprint(h, string(prices), string(aliases), provider.UsageNamesKey())
	for _, current := range provider.All() {
		fmt.Fprintf(h, "%q:%q:%v:%t:%t:%t;", current.ID, current.Where(), current.Catalogs(), current.IsPlugin(), current.Account == nil, current.Key != "")
		if current.Account != nil {
			// a plan prices calls too: Codex's auto-review is free on a
			// paid ChatGPT plan only (provider.ChatGPTReviewFree)
			fmt.Fprintf(h, "%q;", current.Account.Plan)
		}
		fmt.Fprint(h, liveListKey(current.ID))
	}
	meta += fmt.Sprintf("|%x", h.Sum(nil))
	// and the clients the user counts as agents (#1958)
	return meta + "|" + clientAgentsKey()
}
