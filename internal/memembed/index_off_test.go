package memembed

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/istoo/owl/internal/memory"
)

// gateEmbed blocks the first read until the test lets it go, so a read is
// in flight when the switch is turned off.
type gateEmbed struct {
	*fake
	once    sync.Once
	started chan struct{}
	gate    chan struct{}
}

func (g *gateEmbed) Embed(text string) []float32 {
	g.once.Do(func() { close(g.started) })
	<-g.gate
	return g.fake.Embed(text)
}

// Switching the search off while a read runs waits for the read to end and
// does not let it write the folder back after it was deleted (#48).
func TestSetOffWaitsForIndex(t *testing.T) {
	f := sandbox(t, true)
	if !On() {
		t.Fatal("not on after the sandbox switched it on")
	}
	save(t, "Releases go out on Tuesdays", memory.Active)

	g := &gateEmbed{fake: f, started: make(chan struct{}), gate: make(chan struct{})}
	loadModel = func(string) (embedder, error) { return g, nil }

	indexed := make(chan error, 1)
	go func() {
		_, err := Index(context.Background())
		indexed <- err
	}()
	<-g.started

	set := make(chan error, 1)
	go func() { set <- Set(false) }()
	// let the read go on and meet the cancel
	close(g.gate)

	select {
	case err := <-indexed:
		if err != nil {
			t.Fatalf("index: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Index didn't stop")
	}
	select {
	case err := <-set:
		if err != nil {
			t.Fatalf("set off: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Set(false) didn't return")
	}
	for _, p := range []string{Root(), dir(), filepath.Join(dir(), "vectors.bin")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s is still there after switching off: %v", p, err)
		}
	}
	// a read after it is off recreates nothing
	if n, err := Index(context.Background()); err != nil || n != 0 {
		t.Fatalf("index while off: %d %v", n, err)
	}
	if _, err := os.Stat(Root()); !os.IsNotExist(err) {
		t.Fatalf("the folder was rebuilt: %v", err)
	}
}
