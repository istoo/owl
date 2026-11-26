package provider

import (
	"fmt"
	"sync"
	"testing"
)

// Providers saved at once each stay saved: without the lock a read of an
// older providers.json was written back over a newer one, and the newer
// writer's provider (or key, or header) was gone (#10).
func TestProvidersConcurrentWritesKeepEveryChange(t *testing.T) {
	limitsHome(t)
	const n = 24
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%d", i)
		if err := Save(Provider{ID: id, Name: id, Chat: "https://" + id + ".example/v1", Key: "was"}); err != nil {
			t.Fatalf("setup %s: %v", id, err)
		}
	}
	// the provider whose header a refused request narrows
	if err := Save(Provider{ID: "auth", Name: "auth", Chat: "https://auth.example/v1", Key: "k"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%d", i)
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			if i%2 == 0 {
				// each its own provider: a Save replaces its record in the
				// file read afresh, so no other provider's must be lost
				if err := Save(Provider{ID: id, Name: id, Chat: "https://" + id + ".example/v1", Key: fmt.Sprintf("k%d", i)}); err != nil {
					t.Errorf("Save %s: %v", id, err)
				}
				return
			}
			if err := AddKey(id, "second", fmt.Sprintf("sk-%d", i), Chat); err != nil {
				t.Errorf("AddKey %s: %v", id, err)
			}
		}(i, id)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		LearnAuthHeader("auth", AuthXAPIKey)
	}()
	wg.Wait()

	f, err := read()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Provider{}
	for _, p := range f.Providers {
		byID[p.ID] = p
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%d", i)
		p, ok := byID[id]
		if !ok {
			t.Fatalf("%s is gone: one write overwrote another", id)
		}
		if i%2 == 0 {
			if want := fmt.Sprintf("k%d", i); p.Key != want {
				t.Errorf("%s: key %q, want %q (a concurrent write was lost)", id, p.Key, want)
			}
			continue
		}
		if want := fmt.Sprintf("sk-%d", i); !p.HasKey(want) {
			t.Errorf("%s: key %q missing after a concurrent AddKey: %+v", id, want, p.Keys)
		}
	}
	if p := byID["auth"]; p.AuthHeader != AuthXAPIKey {
		t.Errorf("auth: header %q, want %q (a concurrent write was lost)", p.AuthHeader, AuthXAPIKey)
	}
}
