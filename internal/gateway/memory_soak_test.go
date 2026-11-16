package gateway

// The memory soak: one ruler for every later memory change. It runs a
// gateway and a fake upstream in a child process of its own (so nothing
// another test left running is measured), sends Claude Code-sized
// requests through it for a while, and samples the runtime's metrics and
// the OS's footprint once a second. It prints the peak, the steady state,
// the allocation rate and the GC's share of CPU for a redaction-off and a
// redaction-on half, and, when asked, what FreeOSMemory gives back after
// the load.
//
// The child is this test binary, not the app: no tray, window or Dock
// icon is started. It runs under the package's sandbox HOME and binds
// loopback ports of its own, and the outer test kills it by its PID when
// the run outlives its context.
//
// It is skipped unless OWL_MEMORY_SOAK is set, and it is heavy: run one at
// a time, under nice -n 10, only on a machine whose load is quiet.
//
//	OWL_MEMORY_SOAK=2m \
//	OWL_MEMORY_SOAK_OUT=/tmp/owl-soak.jsonl \
//	OWL_MEMORY_SOAK_RELEASE=1 \
//	  nice -n 10 go test -v ./internal/gateway -run '^TestMemorySoak$' -timeout 45m
//
// The value of OWL_MEMORY_SOAK is the whole run; each half (redaction off,
// redaction on) gets half of it. OWL_MEMORY_SOAK_CONCURRENCY sets the
// requests in flight (6 by default). OWL_MEMORY_SOAK_OUT writes one JSON
// object per line, so two configurations can be compared afterwards.
// GOGC and GOMEMLIMIT are read by the runtime in the child as the user set
// them, which is how the runtime policy's choices are compared.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

const (
	soakChildEnv   = "OWL_MEMORY_SOAK_CHILD"
	soakMinutesEnv = "OWL_MEMORY_SOAK"
	soakOutEnv     = "OWL_MEMORY_SOAK_OUT"
	soakReleaseEnv = "OWL_MEMORY_SOAK_RELEASE"
	soakWorkersEnv = "OWL_MEMORY_SOAK_CONCURRENCY"
)

// soakDefault is the run OWL_MEMORY_SOAK=1 asks for: long enough to reach
// a steady heap and see several collections, short enough to repeat.
const soakDefault = 2 * time.Minute

func TestMemorySoak(t *testing.T) {
	d, ok := soakDuration(os.Getenv(soakMinutesEnv))
	if !ok {
		t.Skip("set OWL_MEMORY_SOAK=2m to run the memory soak (see the file's top comment)")
	}
	if os.Getenv(soakChildEnv) != "" {
		soakChild(t, d)
		return
	}
	// the child is the whole run plus room for the build and teardown; it
	// is killed by its own PID through this context if it overruns
	limit := d + 5*time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := proc.CommandContext(ctx, os.Args[0], "-test.run=^TestMemorySoak$", "-test.v",
		"-test.timeout="+limit.String())
	cmd.Env = append(os.Environ(), soakChildEnv+"=1")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "SOAK ") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if err != nil {
		t.Fatalf("the soak child: %v\n%s", err, out.String())
	}
}

// soakDuration says whether the soak was asked for and for how long.
func soakDuration(v string) (time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return 0, false
	case "1", "on", "true", "yes":
		return soakDefault, true
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// soakSample is one second of the child's life.
type soakSample struct {
	At         time.Duration `json:"at"`
	Footprint  uint64        `json:"footprint"`
	Live       uint64        `json:"live"`
	Goal       uint64        `json:"goal"`
	Objects    uint64        `json:"objects"`
	Unused     uint64        `json:"unused"`
	Free       uint64        `json:"free"`
	Released   uint64        `json:"released"`
	Stacks     uint64        `json:"stacks"`
	Other      uint64        `json:"other"`
	TotalAll   uint64        `json:"totalAlloc"`
	NumGC      uint32        `json:"numGC"`
	GCSeconds  float64       `json:"gcSeconds"`
	CPUSeconds float64       `json:"cpuSeconds"`
}

// soakMetrics is the per-second reading, in the order sample() uses it.
var soakMetrics = []metrics.Sample{
	{Name: "/gc/heap/live:bytes"},
	{Name: "/gc/heap/goal:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/heap/unused:bytes"},
	{Name: "/memory/classes/heap/free:bytes"},
	{Name: "/memory/classes/heap/released:bytes"},
	{Name: "/memory/classes/heap/stacks:bytes"},
	{Name: "/memory/classes/other:bytes"},
	{Name: "/cpu/classes/gc/total:cpu-seconds"},
	{Name: "/cpu/classes/total:cpu-seconds"},
}

func soakSampleNow(at time.Duration) soakSample {
	s := append([]metrics.Sample(nil), soakMetrics...)
	metrics.Read(s)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fp, _ := proc.CurrentFootprint()
	return soakSample{
		At:         at,
		Footprint:  fp.Bytes,
		Live:       metricU64(s[0].Value),
		Goal:       metricU64(s[1].Value),
		Objects:    metricU64(s[2].Value),
		Unused:     metricU64(s[3].Value),
		Free:       metricU64(s[4].Value),
		Released:   metricU64(s[5].Value),
		Stacks:     metricU64(s[6].Value),
		Other:      metricU64(s[7].Value),
		TotalAll:   m.TotalAlloc,
		NumGC:      m.NumGC,
		GCSeconds:  metricFloat(s[8].Value),
		CPUSeconds: metricFloat(s[9].Value),
	}
}

func metricFloat(v metrics.Value) float64 {
	if v.Kind() == metrics.KindFloat64 {
		return v.Float64()
	}
	return 0
}

// soakResult is one half's numbers, printed as one JSON line and kept by
// a comparison.
type soakResult struct {
	Phase           string  `json:"phase"`
	Seconds         float64 `json:"seconds"`
	Requests        int64   `json:"requests"`
	Errors          int64   `json:"errors"`
	Bytes           int64   `json:"bytes"`
	PeakFootprint   uint64  `json:"peakFootprint"`
	SteadyFootprint uint64  `json:"steadyFootprint"`
	PeakLive        uint64  `json:"peakLive"`
	SteadyLive      uint64  `json:"steadyLive"`
	AllocPerSecond  float64 `json:"allocBytesPerSecond"`
	GCPercent       float64 `json:"gcCpuPercent"`           // over this half
	GCCumulative    float64 `json:"gcCpuCumulativePercent"` // since process start
	NumGC           uint32  `json:"numGC"`
}

// soakChild runs the gateway, the fake upstream and the load in this
// process, which the outer test started for it alone.
func soakChild(t *testing.T, total time.Duration) {
	t.Helper()
	workers := 6
	if v := strings.TrimSpace(os.Getenv(soakWorkersEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 64 {
			workers = n
		}
	}
	// the fake upstream reads and drops what it is sent, so the upstream's
	// own memory doesn't grow with the test; it answers a tool-call turn,
	// small enough not to be what the numbers are about. While a phase's
	// probe runs it keeps that body instead, to say whether the phase
	// reached the vendor masked.
	var record atomic.Bool
	var probeMu sync.Mutex
	var probeBody []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if record.Load() {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			probeMu.Lock()
			probeBody = b
			probeMu.Unlock()
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, soakReply)
	}))
	defer up.Close()
	noRests()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "soak", Name: "Soak", Key: "k", Models: []string{"m1"}, Anthropic: up.URL}); err != nil {
		t.Fatal(err)
	}
	s := New()
	gw := httptest.NewServer(s.Handler())
	defer gw.Close()

	bodies := soakBodies()
	transport := &http.Transport{
		MaxIdleConns:        workers,
		MaxIdleConnsPerHost: workers,
		MaxConnsPerHost:     workers,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Minute, Transport: transport}

	out, closeOut := openSoakOut(t)
	defer closeOut()

	half := total / 2
	for _, phase := range []struct {
		name   string
		redact bool
	}{
		{"redact-off", false},
		{"redact-on", true},
	} {
		saved := settings.Settings{Redact: phase.redact, RedactModelOff: true}
		if phase.redact {
			// a word the rules mask, so the phase makes the placeholders a
			// body goes out with; the local model stays off, so no test
			// reaches for its download
			saved.RedactWords = []string{"百度"}
		}
		if err := settings.Save(saved); err != nil {
			t.Fatal(err)
		}
		// settings.Shared keeps what it read for about a second
		time.Sleep(1500 * time.Millisecond)
		// one request alone, before the load: what reached the vendor has
		// the word owl masks only in the masked half, so the two halves
		// really differ
		record.Store(true)
		if code, err := soakProbe(client, gw.URL+"/v1/messages", bodies[0]); err != nil || code != http.StatusOK {
			record.Store(false)
			t.Fatalf("%s probe: %d %v", phase.name, code, err)
		}
		record.Store(false)
		probeMu.Lock()
		masked := !bytes.Contains(probeBody, []byte("百度"))
		probeMu.Unlock()
		if masked != phase.redact {
			t.Fatalf("%s: the vendor got the word masked=%v, want %v", phase.name, masked, phase.redact)
		}
		r := soakHalf(t, gw.URL+"/v1/messages", client, bodies, workers, half, phase.name)
		if r.Requests == 0 || r.Errors > 0 {
			t.Fatalf("%s: %d answered, %d failed: the numbers describe a broken run", phase.name, r.Requests, r.Errors)
		}
		line, _ := json.Marshal(r)
		fmt.Printf("SOAK %s\n", line)
		if out != nil {
			_, _ = out.Write(append(line, '\n'))
		}
	}

	// what giving the pages back at once buys, after the load has stopped:
	// the evidence for a policy that frees after a peak
	if os.Getenv(soakReleaseEnv) != "" {
		before, _ := proc.CurrentFootprint()
		debug.FreeOSMemory()
		time.Sleep(2 * time.Second)
		after, _ := proc.CurrentFootprint()
		line, _ := json.Marshal(map[string]any{
			"phase": "free-os-memory", "before": before.Bytes, "after": after.Bytes,
			"returned": int64(before.Bytes) - int64(after.Bytes),
		})
		fmt.Printf("SOAK %s\n", line)
		if out != nil {
			_, _ = out.Write(append(line, '\n'))
		}
	}
}

// soakHalf sends requests for d and samples once a second.
func soakHalf(t *testing.T, url string, client *http.Client, bodies [][]byte, workers int, d time.Duration, phase string) soakResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	start := time.Now()
	first := soakSampleNow(0)
	var requests, errors, sent atomic.Int64
	var workersWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workersWG.Add(1)
		go func(i int) {
			defer workersWG.Done()
			soakLoad(ctx, client, url, bodies, i, &requests, &errors, &sent)
		}(i)
	}

	var samples []soakSample
	tick := time.NewTicker(time.Second)
	for {
		select {
		case <-ctx.Done():
			tick.Stop()
			workersWG.Wait()
			samples = append(samples, soakSampleNow(time.Since(start)))
			return soakSummary(phase, samples, first, time.Since(start), requests.Load(), errors.Load(), sent.Load())
		case <-tick.C:
			samples = append(samples, soakSampleNow(time.Since(start)))
		}
	}
}

// soakProbe sends one request and reads its answer, to see what the vendor
// was sent before the load starts.
func soakProbe(client *http.Client, url string, body []byte) (int, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res.StatusCode, nil
}

// soakLoad sends a request at a time until ctx ends.
func soakLoad(ctx context.Context, client *http.Client, url string, bodies [][]byte, worker int, requests, errors, sent *atomic.Int64) {
	for i := worker; ctx.Err() == nil; i++ {
		body := bodies[i%len(bodies)]
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			errors.Add(1)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "claude-cli/2.0.0 (external, cli)")
		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errors.Add(1)
			continue
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			errors.Add(1)
			continue
		}
		requests.Add(1)
		sent.Add(int64(len(body)))
	}
}

// soakSummary turns the samples into a result: peak, and the steady state
// as the median of the second half, which is past the warm-up.
func soakSummary(phase string, samples []soakSample, first soakSample, elapsed time.Duration, requests, errors, sent int64) soakResult {
	r := soakResult{Phase: phase, Seconds: elapsed.Seconds(), Requests: requests, Errors: errors, Bytes: sent}
	if n := len(samples); n > 0 {
		last := samples[n-1]
		r.NumGC = last.NumGC - first.NumGC
		if secs := elapsed.Seconds(); secs > 0 {
			r.AllocPerSecond = float64(last.TotalAll-first.TotalAll) / secs
		}
		if d := last.CPUSeconds - first.CPUSeconds; d > 0 {
			r.GCPercent = 100 * (last.GCSeconds - first.GCSeconds) / d
		}
		if last.CPUSeconds > 0 {
			r.GCCumulative = 100 * last.GCSeconds / last.CPUSeconds
		}
		var footprints, lives []uint64
		steadyFrom := n / 2
		for i, s := range samples {
			r.PeakFootprint = max(r.PeakFootprint, s.Footprint)
			r.PeakLive = max(r.PeakLive, s.Live)
			if i >= steadyFrom {
				footprints = append(footprints, s.Footprint)
				lives = append(lives, s.Live)
			}
		}
		r.SteadyFootprint = median(footprints)
		r.SteadyLive = median(lives)
	}
	return r
}

func median(v []uint64) uint64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]uint64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// openSoakOut opens OWL_MEMORY_SOAK_OUT for one JSON object a line, if it
// is set.
func openSoakOut(t *testing.T) (io.Writer, func()) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(soakOutEnv))
	if path == "" {
		return nil, func() {}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return f, func() { f.Close() }
}

// soakBodies are the request shapes: Claude Code's, with a long system
// prompt, tools and a tool result, and a user turn of 200 KiB to 2 MiB.
// The same bytes are sent again and again, so what the client holds is a
// few MB that don't grow; the server's copies, masking's placeholders and
// the routes' kept bodies are what the soak measures.
func soakBodies() [][]byte {
	sizes := []int{200 << 10, 512 << 10, 1 << 20, 2 << 20}
	bodies := make([][]byte, len(sizes))
	for i, n := range sizes {
		bodies[i] = soakBody(n)
	}
	return bodies
}

func soakBody(size int) []byte {
	system := "You are Claude Code, Anthropic's official CLI for Claude. " + strings.Repeat(
		"You help with software engineering tasks, reading files, running commands and editing code. ", 160)
	secret := "sk-ant-api03-" + strings.Repeat("A1b2C3d4", 5)
	text := "百度 " + secret + " " + strings.Repeat("The quick brown fox jumps over the lazy dog. ", size/45+1)
	body, err := json.Marshal(map[string]any{
		"model":         "soak/m1",
		"max_tokens":    1024,
		"stream":        true,
		"system":        []map[string]string{{"type": "text", "text": system}},
		"thinking":      map[string]string{"type": "adaptive"},
		"output_config": map[string]string{"effort": "high"},
		"tools": []map[string]any{
			{"name": "Read", "description": "Read a file from the local filesystem.",
				"input_schema": map[string]any{"type": "object", "properties": map[string]any{"file_path": map[string]string{"type": "string"}}, "required": []string{"file_path"}}},
			{"name": "Bash", "description": "Run a shell command.",
				"input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]string{"type": "string"}}, "required": []string{"command"}}},
		},
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{{"type": "text", "text": text}}},
			{"role": "assistant", "content": []map[string]any{
				{"type": "text", "text": "I'll read that file."},
				{"type": "tool_use", "id": "toolu_01", "name": "Read", "input": map[string]string{"file_path": "/tmp/soak/main.go"}},
			}},
			{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "toolu_01", "content": strings.Repeat("package main\n", 512)},
				{"type": "text", "text": "Now summarize it."},
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	return body
}

// soakReply is a tool-call turn as Anthropic streams it.
const soakReply = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_soak","type":"message","role":"assistant","model":"m1","content":[],"stop_reason":null,"usage":{"input_tokens":300,"output_tokens":1}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_soak","name":"Read","input":{}}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/tmp/soak/main.go\"}"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"
