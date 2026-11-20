package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The Recent-calls ring keeps a call's bodies compressed, not as the
// strings the page asks for only when a row is opened: forty requests of
// 256 KiB were ~14 MB of an idle owl. Every reader still gets the body
// back byte for byte.
func TestRecentCallsKeepBodiesCompressed(t *testing.T) {
	s := &Server{}
	request := `{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("the same question, asked again. ", 8000) + `"}]}`
	response := `{"choices":[{"message":{"content":"` + strings.Repeat("an answer that repeats itself. ", 8000) + `"}}]}`
	s.record(Call{Time: time.Now(), Model: "m", RequestBody: request, ResponseBody: response, RequestTruncated: true})
	s.record(Call{Time: time.Now(), Model: "m", RequestBody: "tiny", ResponseBody: "ok"})

	s.mu.Lock()
	big, small := s.recent[0], s.recent[1]
	s.mu.Unlock()
	if big.RequestBody != "" || big.ResponseBody != "" {
		t.Fatal("a body of 256 KiB is still a string in the ring")
	}
	if big.reqZip == nil || big.resZip == nil {
		t.Fatal("a body of 256 KiB wasn't compressed")
	}
	if held := len(big.reqZip) + len(big.resZip); held >= (len(request)+len(response))/2 {
		t.Fatalf("the ring holds %d bytes for %d", held, len(request)+len(response))
	}
	if small.RequestBody != "tiny" || small.resZip != nil {
		t.Fatal("a body too small to save by was compressed")
	}

	if c, ok := s.RecentCall(big.ID); !ok || c.RequestBody != request || c.ResponseBody != response || !c.RequestTruncated {
		t.Fatal("RecentCall doesn't give the body back")
	}
	recent := s.Recent()
	if len(recent) != 2 || recent[1].RequestBody != request || recent[1].ResponseBody != response {
		t.Fatal("Recent doesn't give the body back")
	}
	if meta := s.RecentMeta(); len(meta) != 2 || meta[1].RequestBody != "" || meta[1].ResponseBody != "" || meta[0].RequestBody != "" {
		t.Fatal("RecentMeta carries a body")
	}
}

// Forty calls of 256 KiB each way, as captureRequestBody creates them,
// are held compressed: 20 MB of strings were ~14 MB of an idle owl. The
// reading is taken in a process of its own (tasks/lessons.md, 11-14).
func TestRecentCallsMemory(t *testing.T) {
	const child = "OWL_TEST_RECENT_BODIES_HEAP"
	if os.Getenv(child) != "" {
		var before, after runtime.MemStats
		s := &Server{}
		runtime.GC()
		runtime.ReadMemStats(&before)
		for i := 0; i < recentMost; i++ {
			request := fmt.Sprintf(`{"i":%d,"messages":[{"role":"user","content":"%s"}]}`, i, strings.Repeat("the same question, asked again. ", 8000))
			response := fmt.Sprintf(`{"i":%d,"choices":[{"message":{"content":"%s"}}]}`, i, strings.Repeat("an answer that repeats itself. ", 8000))
			s.record(Call{Time: time.Now(), Model: "m", RequestBody: request, ResponseBody: response})
		}
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(s)
		held := int64(after.HeapAlloc) - int64(before.HeapAlloc)
		s.mu.Lock()
		zips := 0
		for _, c := range s.recent {
			zips += len(c.reqZip) + len(c.resZip)
		}
		s.mu.Unlock()
		t.Logf("%d calls of 256 KiB each way hold %d bytes (%d in S2)", recentMost, held, zips)
		if held > 8<<20 {
			t.Fatalf("%d calls hold %d bytes", recentMost, held)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecentCallsMemory$", "-test.v")
	cmd.Env = append(os.Environ(), child+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the measurement failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hold") {
		t.Fatalf("the measurement printed nothing: %s", out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}

// A reply read by the Routing page's request number gets its body back
// too.
func TestRouteCallGivesTheBodyBack(t *testing.T) {
	s := &Server{}
	body := `{"messages":[{"content":"` + strings.Repeat("x", 40000) + `"}]}`
	s.record(Call{Time: time.Now(), Route: 7, RequestBody: body})
	c, ok := s.RouteCall(7)
	if !ok || c.RequestBody != body {
		t.Fatal("RouteCall doesn't give the body back")
	}
}
