package gui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/sessions"
	"github.com/istoo/owl/internal/tune"
)

// The kept calls are a compact form of tune.Call: a stream and subagent flag
// kept once per file, a model index, and 32-bit numbers, with anything that
// doesn't fit kept whole. What a refresh asks over must be the calls
// FromCalls made of the file, field for field, boundary values included.
func TestTuneKeptCallsReadBackWhole(t *testing.T) {
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	plus8 := time.FixedZone("plus8", 8*3600)
	call := func(mod func(*sessions.Call)) sessions.Call {
		c := sessions.Call{
			Agent: "claude", Session: "s", File: "/p/s.jsonl", Time: t0,
			Model: "claude-opus-5", Msg: "m",
			Tokens: sessions.Tokens{Input: 10, CacheRead: 20000, CacheWrite: 1000, Output: 200, CacheWrite1h: 100},
		}
		if mod != nil {
			mod(&c)
		}
		return c
	}
	cs := []sessions.Call{
		call(nil),
		call(func(c *sessions.Call) { c.Requested = "Claude-Opus-5[1m]"; c.Time = t0.Add(time.Minute) }),
		call(func(c *sessions.Call) { c.Model = "claude-sonnet-4-5"; c.Time = t0.Add(2 * time.Minute) }),
		// failed and promptless calls: hashed, not kept as calls
		call(func(c *sessions.Call) { c.Error = "rate_limit"; c.Msg = "failed" }),
		call(func(c *sessions.Call) { c.Input, c.CacheRead, c.CacheWrite = 0, 0, 0; c.Msg = "empty" }),
		call(func(c *sessions.Call) { c.Time = time.Time{} }),
		call(func(c *sessions.Call) { c.Time = t0.In(plus8) }),
		// the last prompt that fits int32, and one past it
		call(func(c *sessions.Call) {
			c.Input, c.CacheRead, c.CacheWrite = math.MaxInt32-21000, 20000, 1000
			c.Output, c.CacheWrite1h = math.MaxInt32, math.MaxInt32
		}),
		call(func(c *sessions.Call) { c.Input, c.CacheRead = math.MaxInt32, 1 }),
		// a negative compaction, and one past int32 milliseconds
		call(func(c *sessions.Call) { c.Compact = -5 }),
		call(func(c *sessions.Call) { c.Compact = 1 << 33 }),
		// a subagent's stream, back in the main one, and no File at all
		call(func(c *sessions.Call) { c.File = "/p/subagents/a.jsonl" }),
		call(func(c *sessions.Call) { c.File = "/p/s.jsonl" }),
		call(func(c *sessions.Call) { c.File = ""; c.Session = "t" }),
	}
	f := tuneFileOf("claude", cs)
	want := tune.FromCalls(cs)
	var src []int // the source line of each kept call
	for i, c := range cs {
		if _, ok := tune.FromCall(c); ok {
			src = append(src, i)
		}
	}
	if len(f.calls) != len(want) || len(src) != len(want) {
		t.Fatalf("%d kept, want %d", len(f.calls), len(want))
	}
	// the calls that fit are compact, the rest are kept whole
	if len(f.wide) != 4 {
		t.Fatalf("%d calls kept whole, want 4", len(f.wide))
	}
	for i := range want {
		got := f.call(i)
		if !got.Time.Equal(want[i].Time) {
			t.Fatalf("call %d's time %v, want %v", i, got.Time, want[i].Time)
		}
		g, w := got, want[i]
		// a time from another zone keeps its instant, in UTC; every reader
		// compares instants
		g.Time, w.Time = time.Time{}, time.Time{}
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("call %d:\n%+v\n%+v", i, got, want[i])
		}
		if m := f.msgs[f.calls[i].msg]; m != msgHash(cs[src[i]].Msg) {
			t.Fatalf("call %d's message hash %x, want %x", i, m, msgHash(cs[src[i]].Msg))
		}
	}
	// the failed and promptless calls are hashed too, so a message a resumed
	// file copied is still counted once
	if f.msgs[3] == 0 || f.msgs[4] == 0 {
		t.Fatalf("a call kept out as a message hash: %x %x", f.msgs[3], f.msgs[4])
	}
}

// The cache's cost is what the Context tab's refresh pays in residency.
// tune.Call is 112 bytes and the old file also kept a source index per call
// and append's spare room: 131 bytes a call measured (some 190-225 in the
// 0.2.20 heap). tuneCall is 32 and its message hash 8; 50 leaves room over
// 40 and is well under the old form.
const tuneBytesPerCall = 50

// TestTuneKeptCallCostsUnder50Bytes measures the kept calls of 60,000
// synthetic Claude calls across three files in a process running only this
// test, as LESSONS asks of a heap bound: another test's leftovers must not
// count. The old representation measured 131 bytes a call here.
func TestTuneKeptCallCostsUnder50Bytes(t *testing.T) {
	const child = "OWL_TEST_TUNE_CACHE_MEM"
	if os.Getenv(child) == "1" {
		tuneCacheChild(t)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := proc.CommandContext(ctx, os.Args[0], "-test.run=^TestTuneKeptCallCostsUnder50Bytes$", "-test.timeout=9m")
	cmd.Env = append(os.Environ(), child+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a cached call costs over %d bytes: %v\n%s", tuneBytesPerCall, err, out)
	}
}

func tuneCacheChild(t *testing.T) {
	cc := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cc)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	tuneKept.Lock()
	tuneKept.files = map[string]tuneFile{}
	tuneKept.Unlock()
	tuneForget()
	t.Cleanup(tuneForget)

	proj := filepath.Join(cc, "projects", "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const perFile = 20000
	now := time.Now()
	for k := range 3 {
		sess := fmt.Sprintf("%08d-0000-0000-0000-000000000000", k)
		var b strings.Builder
		b.Grow(perFile * 280)
		for i := range perFile {
			id := k*perFile + i
			ts := now.Add(-time.Duration(id) * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
			fmt.Fprintf(&b, `{"type":"assistant","uuid":"u-%d","timestamp":"%s","message":{"id":"msg_%d","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":5,"cache_creation_input_tokens":1000,"cache_read_input_tokens":%d,"output_tokens":200}},"requestId":"req_%d","sessionId":"%s","cwd":"/work/app","entrypoint":"cli"}`+"\n",
				id, ts, id, 20000+i, id, sess)
		}
		if err := os.WriteFile(filepath.Join(proj, sess+".jsonl"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// read once first: the parsers' one-time setup and the line readers
	// they keep on their shelves (~1 MiB here) are not the cache's cost, so
	// the baseline holds them already. Reset lets the parse itself go and
	// keeps the shelves.
	for _, s := range sessions.CallSources() {
		if s.Agent == "claude" || s.Agent == "codex" {
			sessions.ReadCallSource(s)
		}
	}
	sessions.Reset()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	since := now.AddDate(0, 0, -7)
	if n := keptTuneCalls(since); n != 3*perFile {
		t.Fatalf("%d calls kept, want %d", n, 3*perFile)
	}
	// what sessions keeps of a read (its revision window, ~2 MB here) is
	// not the cache being measured; Reset leaves tuneKept as it is
	sessions.Reset()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)

	kept := 0
	tuneKept.Lock()
	for _, f := range tuneKept.files {
		kept += len(f.calls)
	}
	tuneKept.Unlock()
	if kept != 3*perFile {
		t.Fatalf("%d calls in the cache, want %d", kept, 3*perFile)
	}
	per := float64(after.HeapAlloc-before.HeapAlloc) / float64(kept)
	t.Logf("kept heap %d bytes over %d calls: %.1f bytes a call (limit %d)",
		after.HeapAlloc-before.HeapAlloc, kept, per, tuneBytesPerCall)
	if per > tuneBytesPerCall {
		t.Fatalf("%.1f bytes a cached call, want at most %d", per, tuneBytesPerCall)
	}
}

// keptTuneCalls builds the cache and returns the calls it gives back; in a
// function of its own so the answer's own slice is gone before measuring.
func keptTuneCalls(since time.Time) int {
	by := tuneCallsSince(since)
	return len(by["claude"])
}
