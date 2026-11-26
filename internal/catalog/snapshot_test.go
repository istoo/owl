package catalog

import (
	"sync"
	"testing"
	"time"
)

// The catalog's tables are one immutable snapshot published whole. A Reset
// or the idle drop while a call reads them only drops the package's pointer:
// the call keeps the snapshot it took, complete, and the next load builds
// another. Run under -race, this catches a map written under a reader.
func TestCatalogSnapshotSurvivesResetAndIdleDrop(t *testing.T) {
	writeCatalog(t, `{"openai":{"models":{"gpt-6.1-sol":{"id":"gpt-6.1-sol","name":"GPT-6.1 Sol",
		"limit":{"context":800000,"output":100000},
		"modalities":{"input":["text","image"],"output":["text"]},
		"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}],
		"cost":{"input":1,"output":2}}}}}`)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var bad int64
	var mu sync.Mutex
	read := func() {
		if ContextOf("gpt-6.1-sol") != 800000 ||
			OutputOf("gpt-6.1-sol") != 100000 ||
			!Thinks("gpt-6.1-sol") ||
			!Lists("gpt-6.1-sol") ||
			!SeesImages("gpt-6.1-sol") ||
			len(EffortsOf("gpt-6.1-sol")) == 0 ||
			len(InputsOf("gpt-6.1-sol")) == 0 ||
			NameOf("gpt-6.1-sol") != "GPT-6.1 Sol" {
			mu.Lock()
			bad++
			mu.Unlock()
		}
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					read()
				}
			}
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					Reset()
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				loadKept.last.Store(time.Now().Add(-2 * loadIdle).UnixNano())
				dropIdleLoad()
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
	if bad != 0 {
		t.Fatalf("%d reads of the catalog came back wrong while it was reset and dropped", bad)
	}
}
