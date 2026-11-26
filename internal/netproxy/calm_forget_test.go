package netproxy

import (
	"fmt"
	"net/http"
	"testing"
)

// calmedCount is how many transports calmed keeps now.
func calmedCount() int {
	n := 0
	calmed.Range(func(any, any) bool { n++; return true })
	return n
}

// A transport dropped from a dispatch's pool is forgotten by calmed:
// proxies come and go as they are edited, and a pool that drops its old
// transports must not leave an entry for each of them behind.
func TestDroppedTransportsAreForgotten(t *testing.T) {
	base := &http.Transport{}
	if err := CalmHTTP2(base); err != nil {
		t.Fatal(err)
	}
	d := &dispatch{base: base, own: map[route]*http.Transport{}, moved: func() bool { return false }}
	t.Cleanup(func() {
		base.CloseIdleConnections()
		d.mu.Lock()
		for _, tr := range d.own {
			uncalm(tr)
		}
		d.mu.Unlock()
		uncalm(base)
	})
	before := calmedCount()
	for i := range 40 {
		tr := d.transport(route{choice: fmt.Sprintf("proxy-%d", i)})
		if !isCalm(tr) {
			t.Fatalf("route %d isn't a calmed transport", i)
		}
	}
	// the pool keeps 32 at most, and the last eviction left one
	if after := calmedCount(); after > before+33 {
		t.Fatalf("calmed holds %d transports after 40 dropped routes, %d before", after, before)
	}
}
