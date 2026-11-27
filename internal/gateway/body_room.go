package gateway

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/istoo/owl/internal/proc"
)

// bodyRoom bounds the decoded request bytes the gateway serves at once.
// One request's limit (defaultBodyLimit) bounds no sum: each body is held
// while its request is served, with what is made from it, the model
// rewritten, a translation to Chat, a plugin's base64 line, several times
// its size. On a 2 GiB server Codex turns of ~45 MiB, one after another
// and side by side, held the Go heap at ~500 MiB and the plugin host at
// ~500 MiB, into swap (jihuayu, #4004).
//
// A body of bodyRoomFree or more takes room for its size until its
// request ends. With too little left it waits its turn, first come first
// served, for requests to end, bodyRoomWait at most, and is answered 503
// with the provider not asked. A body bigger than the whole room runs
// alone. Small bodies, an agent's ordinary turns, pass without counting.
type bodyRoomT struct {
	mu sync.Mutex
	// room is the most held at once; 0 no bound. sized says room was set.
	room  int64
	why   string
	sized bool
	held  int64
	// by is what each request being served holds, by its body-room key,
	// so a body read twice in one request takes its room once. The key is
	// the request's mark (withBodyRoom), which a context made from it
	// keeps: a middleware's WithContext (withCaller, keySlots, withSpent)
	// would otherwise count the same body again.
	by    map[any]int64
	queue []*bodyWaiter
}

type bodyWaiter struct {
	ctx context.Context
	n   int64
	in  chan struct{} // closed when let in
}

var bodyRoom = &bodyRoomT{}

// bodyRoomFree is the size a body takes room from; smaller ones pass.
const bodyRoomFree = 1 << 20

// bodyRoomWait is how long a body waits for room before its request is
// answered 503: within unroutedMost, which the wait counts toward
// (noteWait), so the answer says why.
var bodyRoomWait = 100 * time.Second

// bodyRoomKey marks a request for bodyRoom. Its value is one per
// request, so the contexts it is made from (a middleware's WithContext)
// count a body read twice as one request's. withCaller sets it.
type bodyRoomKey struct{}

// bodyRoomMark is one request's. It carries an id so two of them are
// never the same key: pointers to zero-size values may share an address,
// and every request of every caller would then count as one.
type bodyRoomMark struct{ id uint64 }

var bodyRoomMarkID atomic.Uint64

// withBodyRoom is ctx marked as a request's, once at its start.
func withBodyRoom(ctx context.Context) context.Context {
	return context.WithValue(ctx, bodyRoomKey{}, &bodyRoomMark{id: bodyRoomMarkID.Add(1)})
}

// roomKey is the mark ctx carries, or ctx itself when it carries none
// (a handler called directly, a test's own context): either way, a body
// read twice through the same context is counted once.
func roomKey(ctx context.Context) any {
	if m, ok := ctx.Value(bodyRoomKey{}).(*bodyRoomMark); ok {
		return m
	}
	return ctx
}

// bodyRoomFloor is the least room, however small the machine.
const bodyRoomFloor = 64 << 20

// bodyRoomSize is a thirty-second of what owl's processes may use
// (memLimit on Linux, the computer's memory elsewhere), bodyRoomFloor at
// least: a 44 MiB Responses turn sent on as Chat peaked at ~700 MiB of Go
// heap on its way, garbage not yet collected included, and a plugin's host
// holds more. Where neither is known there is no bound: a limit not known
// is not a small one.
func bodyRoomSize() (int64, string) {
	limit, why := memLimit(memGOOS, memRoot)
	if limit <= 0 && memOwn() {
		limit, why = proc.MemTotal(), "the computer's memory"
	}
	if limit <= 0 {
		return 0, ""
	}
	return max(limit/32, bodyRoomFloor), fmt.Sprintf("a thirty-second of %d MiB (%s)", limit>>20, why)
}

// bodyFull is the error of a body that found no room in bodyRoomWait.
type bodyFull struct {
	n, held, room int64
	why           string
	waited        time.Duration
	// grew says a body already let in grew past the room it was let in
	// for, rather than one never let in: no wait was spent on it.
	grew bool
}

func (e *bodyFull) Error() string {
	if e.grew {
		return fmt.Sprintf("owl is serving %d MiB of request bodies at once, as much as it holds (%d MiB, %s), "+
			"and this body has grown past the room it was let in with; the provider was not asked",
			e.held>>20, e.room>>20, e.why)
	}
	return fmt.Sprintf("owl is serving %d MiB of request bodies at once, as much as it holds (%d MiB, %s), "+
		"and none of them ended within %s to make room for this %d MiB one; the provider was not asked",
		e.held>>20, e.room>>20, e.why, e.waited, e.n>>20)
}

// take holds n bytes of room for the request ctx belongs to until ctx is
// done, waiting its turn for it. A request takes it on its Content-Length
// before its body is read, so the ones waiting don't hold theirs, and
// again on the body read, which only adds what that is over the first. A
// ctx that is never done (a caller's own) takes nothing, since nothing
// would give it back.
func (b *bodyRoomT) take(ctx context.Context, n int64) error {
	if n < bodyRoomFree || ctx.Done() == nil {
		return nil
	}
	b.mu.Lock()
	if !b.sized {
		b.sized = true
		b.room, b.why = bodyRoomSize()
	}
	key := roomKey(ctx)
	h, holds := b.by[key]
	if holds {
		if n <= h || b.room <= 0 {
			b.mu.Unlock()
			return nil
		}
		// the body read is bigger than its request said, a compressed
		// one: it grows only into the room the others leave. Waiting
		// here would leave every holder holding part of the room while
		// none can finish, so it is refused now. A body alone may grow
		// past the room, as an oversized one runs alone.
		if b.held > h && b.held+(n-h) > b.room {
			full := &bodyFull{n: n, held: b.held, room: b.room, why: b.why, grew: true}
			b.mu.Unlock()
			return full
		}
		b.held += n - h
		b.by[key] = n
		b.mu.Unlock()
		return nil
	}
	if b.room <= 0 {
		b.mu.Unlock()
		return nil
	}
	w := &bodyWaiter{ctx: ctx, n: n, in: make(chan struct{})}
	b.queue = append(b.queue, w)
	b.letIn()
	held, room, why := b.held, b.room, b.why
	b.mu.Unlock()
	select {
	case <-w.in:
		return nil
	default:
	}
	start := time.Now()
	log.Printf("gateway: a %d MiB request body waits for room: %d MiB of %d MiB held (%s)", n>>20, held>>20, room>>20, why)
	defer noteWait(ctx, "body room")()
	timer := time.NewTimer(bodyRoomWait)
	defer timer.Stop()
	var err error
	select {
	case <-w.in:
		return nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-w.in:
		return nil // let in as it gave up: it holds the room, and ctx gives it back
	default:
	}
	b.queue = slices.DeleteFunc(b.queue, func(q *bodyWaiter) bool { return q == w })
	b.letIn() // one ahead gone may let the next in
	if err != nil {
		return err
	}
	return &bodyFull{n: n, held: b.held, room: b.room, why: b.why, waited: time.Since(start).Round(time.Second)}
}

// letIn lets in, in order, each waiter there is room for: one bigger than
// the room when nothing is held. b.mu is held.
func (b *bodyRoomT) letIn() {
	for len(b.queue) > 0 {
		w := b.queue[0]
		if b.held > 0 && b.held+w.n > b.room {
			return
		}
		b.queue = b.queue[1:]
		if b.by == nil {
			b.by = map[any]int64{}
		}
		b.held += w.n
		b.by[roomKey(w.ctx)] = w.n
		close(w.in)
		ctx := w.ctx
		context.AfterFunc(ctx, func() { b.give(ctx) })
	}
}

// give returns what ctx's request held.
func (b *bodyRoomT) give(ctx context.Context) {
	k := roomKey(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.held -= b.by[k]
	delete(b.by, k)
	b.letIn()
}
