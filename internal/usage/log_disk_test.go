package usage

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/istoo/owl/internal/probelog"
	"github.com/istoo/owl/internal/provider"
)

// logLines appends n calls from the i-th on to the log, as a gateway that
// isn't running reads them at its next start: in two zones, with hosts,
// keys, a try handed on and probes among them.
func logLines(t *testing.T, from, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.FixedZone("", 8*3600))
	for i := from; i < from+n; i++ {
		r := Record{Time: at.Add(time.Duration(i) * time.Minute), Agent: "codex", Provider: fmt.Sprintf("p%d", i%3), Model: "m",
			Host: fmt.Sprintf("h%d.example", i%5), Session: fmt.Sprintf("s%d", i/40), Input: i, Status: 200}
		switch {
		case i%7 == 0:
			r.Time = r.Time.UTC()
		case i%11 == 0:
			r.ProviderKeyID, r.ProviderKeyName = fmt.Sprintf("k%d", i), "Team"
		case i%13 == 0:
			r.Source, r.Kind, r.Endpoint, r.Status, r.Error = SourceProbe, probelog.KindTest, "/v1/chat/completions", 500, "down"
		case i%17 == 0:
			r.RouteID, r.Status, r.Error = int64(i), 502, "bad gateway"
		case i%17 == 1:
			r.RouteID = int64(i - 1)
		}
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// logRead is what a snapshot gives its readers, written out.
func logRead(t *testing.T, s *logSnapshot) map[string]any {
	t.Helper()
	var rows, probes []string
	s.visit(time.Time{}, func(r Record) {
		b, _ := json.Marshal(r)
		rows = append(rows, string(b))
	})
	eachProbe(s.probes, time.Time{}, func(r Record, off int64) {
		b, _ := json.Marshal(r)
		probes = append(probes, fmt.Sprintf("%d %s", off, b))
	})
	first, _ := s.first.MarshalText()
	firstOwn, _ := s.firstOwn.MarshalText()
	return map[string]any{"rows": rows, "probes": probes, "first": string(first), "firstOwn": string(firstOwn), "off": s.off, "hash": s.hash,
		"blocks": len(s.blocks), "keys": slices.Sorted(maps.Keys(s.keyProviders)), "hosts": s.hosts}
}

func restartLog() {
	logIndex.Lock()
	logIndex.snapshot = nil
	logIndex.Unlock()
	logDiskWrote.Lock()
	logDiskWrote.path, logDiskWrote.off, logDiskWrote.at = "", 0, time.Time{}
	logDiskWrote.Unlock()
}

// A start reads the log's full blocks from log-v1.bin and parses only the
// lines after them, into the snapshot a parse of the whole log makes; a
// log whose first lines changed is parsed whole again (#2278).
func TestStartReadsKeptLogBlocks(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	loaded := -1
	logDiskLoaded = func(n int) { loaded = n }
	t.Cleanup(func() { logDiskLoaded = nil })
	restartLog()
	logLines(t, 0, 2*logBlockRows+300)
	first := logRead(t, readLogSnapshot())
	if loaded != -1 {
		t.Fatalf("the first start read %d blocks from a file it hadn't written", loaded)
	}
	if _, err := os.Stat(logDiskPath()); err != nil {
		t.Fatalf("the full blocks weren't kept: %v", err)
	}
	if n := len(first["probes"].([]string)); n < 50 {
		t.Fatalf("%d probes", n)
	}

	// the log grows while owl isn't running
	logLines(t, 2*logBlockRows+300, logBlockRows)
	restartLog()
	kept := logRead(t, readLogSnapshot())
	if loaded != 2 {
		t.Fatalf("started again, %d blocks read from the file, want 2", loaded)
	}
	loaded = -1
	os.Remove(logDiskPath())
	restartLog()
	whole := logRead(t, readLogSnapshot())
	if loaded != -1 {
		t.Fatal("read blocks from a file that was removed")
	}
	if !reflect.DeepEqual(kept, whole) {
		for k := range whole {
			if !reflect.DeepEqual(kept[k], whole[k]) {
				t.Errorf("%s from the kept blocks differs from the log's parse:\n%v\nwant\n%v", k, kept[k], whole[k])
			}
		}
	}

	// the log's first line is written over: its blocks are parsed again
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(b, '1'); i < 0 {
		t.Fatal("no digit")
	} else {
		b[i] = '2'
	}
	if err := os.WriteFile(Path(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	restartLog()
	rewritten := logRead(t, readLogSnapshot())
	if loaded != -1 {
		t.Fatalf("read %d blocks kept of the log before it was written over", loaded)
	}
	os.Remove(logDiskPath())
	restartLog()
	if want := logRead(t, readLogSnapshot()); !reflect.DeepEqual(rewritten, want) {
		t.Fatal("the log written over isn't read as a parse of it reads it")
	}
}

// The full blocks a first parse made are of the file's pages once it is
// kept, not of the heap until the next start: a long history's ~50 MB of
// archives, which every reader decodes a block of on demand anyway.
func TestBuiltLogBlocksAreKeptOnDisk(t *testing.T) {
	if !mapsFiles {
		t.Skip("this system maps no file")
	}
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	restartLog()
	logLines(t, 0, 3*logBlockRows+100)
	s := readLogSnapshot()
	if s.full.n != 3 {
		t.Fatalf("%d full blocks, want 3", s.full.n)
	}
	check := func(kind string, chunks []*rowChunk, n int) {
		t.Helper()
		for i := range n {
			c := chunks[i]
			if c == nil || len(c.Archive) == 0 {
				t.Fatalf("%s block %d: no archive", kind, i)
			}
			if c.mapped == nil {
				t.Fatalf("%s block %d: of the heap after the file was kept", kind, i)
			}
			at, start := uintptr(unsafe.Pointer(unsafe.SliceData(c.Archive))), uintptr(unsafe.Pointer(unsafe.SliceData(c.mapped.data)))
			if at < start || at+uintptr(len(c.Archive)) > start+uintptr(len(c.mapped.data)) {
				t.Fatalf("%s block %d: archive outside the mapping", kind, i)
			}
		}
	}
	check("gateway", s.blocks, s.full.n)
	check("probe", s.probes, s.full.probes)
	before := logRead(t, s)
	// the mapping, not the path, keeps the pages: removing the file it
	// was read from changes nothing these blocks read
	if err := os.Remove(logDiskPath()); err != nil {
		t.Fatal(err)
	}
	if got := logRead(t, s); !reflect.DeepEqual(got, before) {
		t.Fatal("the mapped blocks no longer read as the log's parse")
	}
}

// A continued parse shares the published snapshot's blocks: the write
// that puts them on the file copies them before it gives them its pages,
// so a reader of the snapshot before decodes what it was.
func TestKeptLogBlocksStayAsTheirSnapshotReadThem(t *testing.T) {
	if !mapsFiles {
		t.Skip("this system maps no file")
	}
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	restartLog()
	logLines(t, 0, 2*logBlockRows+100)
	before := readLogSnapshot()
	want := logRead(t, before)
	first := before.blocks[0]
	if first.mapped == nil {
		t.Fatal("the first read's block isn't on the file")
	}
	// the next save isn't held back by the ten minutes between writes
	logDiskWrote.Lock()
	logDiskWrote.at = time.Time{}
	logDiskWrote.Unlock()
	logLines(t, 2*logBlockRows+100, logBlockRows)
	after := readLogSnapshot()
	if after == before || after.full.n <= before.full.n {
		t.Fatalf("not written again: %d blocks before, %d after", before.full.n, after.full.n)
	}
	if after.blocks[0] == first {
		t.Fatal("the write changed a block its snapshot before still reads")
	}
	if got := logRead(t, before); !reflect.DeepEqual(got, want) {
		t.Fatal("the snapshot before the write no longer reads as it did")
	}
}

// A history first parsed in this process keeps no more of its blocks on
// the heap than the file's size: before the write's blocks went to its
// pages, a long history's ~50 MB stayed until the next start. Measured in
// a process of its own, so the package's earlier tests aren't counted
// (tasks/lessons.md, 11-14).
func TestLogBlocksMemory(t *testing.T) {
	if !mapsFiles {
		t.Skip("this system maps no file")
	}
	const child = "OWL_TEST_LOG_BLOCKS_HEAP"
	if os.Getenv(child) != "" {
		pageHome(t)
		settleLogClock(t)
		t.Cleanup(restartLog)
		restartLog()
		logWideLines(t, 60000)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		s := readLogSnapshot()
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(s)
		held, archives := int64(after.HeapAlloc)-int64(before.HeapAlloc), 0
		for _, c := range s.blocks {
			archives += len(c.Archive)
		}
		t.Logf("%d blocks hold %d bytes of archives; the heap grew by %d bytes", len(s.blocks), archives, held)
		if held > int64(archives)/4+(1<<20) {
			t.Fatalf("%d bytes of archives put %d bytes on the heap", archives, held)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLogBlocksMemory$", "-test.v")
	cmd.Env = append(os.Environ(), child+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the measurement failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "the heap grew by") {
		t.Fatalf("the measurement printed nothing: %s", out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}

// logWideLines writes n calls of a long, varied body, as a real history's
// records are.
func logWideLines(t *testing.T, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		r := Record{Time: at.Add(time.Duration(i) * time.Second), Agent: fmt.Sprint("agent-", i%7), Provider: fmt.Sprint("p", i%3),
			Model: fmt.Sprint("model-", i%11), Host: fmt.Sprintf("h%d.example", i%5), Session: fmt.Sprintf("s%d", i/40),
			Input: 1200 + i%97, Output: 300, Status: 200, RequestID: fmt.Sprintf("%032x%s", i*2654435761, strings.Repeat("q", 64)),
			Error: strings.Repeat(fmt.Sprintf("e%d-", i), 30)}
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// probeLines appends n MCP tool calls from the i-th on to the log.
func probeLines(t *testing.T, from, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for i := from; i < from+n; i++ {
		r := Record{Time: at.Add(time.Duration(i) * time.Second), Source: SourceMCP, Agent: "codex", Provider: fmt.Sprintf("p%d", i%3),
			Model: fmt.Sprintf("tool%d", i%9), Millis: int64(i), Status: 200}
		if i%5 == 0 {
			r.Source, r.Kind, r.Host, r.Status, r.Error = SourceProbe, probelog.KindTest, "h.example", 500, "down"
		}
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// The probes are kept in blocks as the gateway rows are: a read continued
// over appended probes reopens the last block as a copy, so the snapshot
// before it reads as it did, and the two read as one parse of the log.
func TestProbeBlocksContinued(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	restartLog()
	probeLines(t, 0, 2*logBlockRows+100)
	before := readLogSnapshot()
	want := logRead(t, before)
	if n := len(want["probes"].([]string)); n != 2*logBlockRows+100 {
		t.Fatalf("%d probes, want %d", n, 2*logBlockRows+100)
	}
	if len(before.probes) != 3 {
		t.Fatalf("%d probe blocks, want 3", len(before.probes))
	}
	probeLines(t, 2*logBlockRows+100, logBlockRows)
	after := readLogSnapshot()
	if after == before || len(after.probes) < 4 {
		t.Fatalf("not read again: %d blocks", len(after.probes))
	}
	if got := logRead(t, before); !reflect.DeepEqual(got["probes"], want["probes"]) {
		t.Fatal("the snapshot before the append changed under its readers")
	}
	grown := logRead(t, after)
	os.Remove(logDiskPath())
	restartLog()
	if whole := logRead(t, readLogSnapshot()); !reflect.DeepEqual(grown["probes"], whole["probes"]) {
		t.Fatalf("the continued read's probes differ from the log's parse: %d vs %d", len(grown["probes"].([]string)), len(whole["probes"].([]string)))
	}
	if c := probeChunk(after.probes, time.Date(2026, 8, 1, 9, 20, 0, 0, time.UTC), provider.Renames{}, nil); c == nil || c.Count != 3*logBlockRows+100-1200 {
		t.Fatalf("probes since: %v", c)
	}
}

// A log of MCP tool calls and probes alone has no gateway block, and was
// never kept: every start parsed all of it again. Its full probe blocks
// are kept, and a start reads them as a parse of the log does (#2278).
func TestStartReadsKeptProbeBlocks(t *testing.T) {
	pageHome(t)
	settleLogClock(t)
	t.Cleanup(restartLog)
	loaded := -1
	logDiskLoaded = func(n int) { loaded = n }
	t.Cleanup(func() { logDiskLoaded = nil })
	restartLog()
	probeLines(t, 0, 3*logBlockRows+100)
	readLogSnapshot()
	if _, err := os.Stat(logDiskPath()); err != nil {
		t.Fatalf("a log of probes wasn't kept: %v", err)
	}
	probeLines(t, 3*logBlockRows+100, logBlockRows/2)
	restartLog()
	kept := logRead(t, readLogSnapshot())
	if loaded != 3 {
		t.Fatalf("started again, %d blocks read from the file, want 3", loaded)
	}
	os.Remove(logDiskPath())
	restartLog()
	loaded = -1
	whole := logRead(t, readLogSnapshot())
	if loaded != -1 {
		t.Fatal("read blocks from a file that was removed")
	}
	if !reflect.DeepEqual(kept, whole) {
		t.Fatalf("the kept probes differ from the log's parse: %d vs %d", len(kept["probes"].([]string)), len(whole["probes"].([]string)))
	}
}
