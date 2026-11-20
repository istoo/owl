package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// CheckHealth asks the model list once and says whether the key was
// taken, keeping an auth refusal apart from an address nothing answers.
func TestCheckHealthAsksTheListOnce(t *testing.T) {
	detectHome(t)
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Key: "sk-test", Chat: srv.URL + "/v1"}
	h := p.CheckHealth(context.Background())
	if !h.OK || h.Kind != HealthOK {
		t.Fatalf("health: %+v", h)
	}
	if h.Millis < 0 {
		t.Fatalf("latency %d", h.Millis)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "GET /v1/models Bearer sk-test" {
		t.Fatalf("asked %v", asked)
	}
	// nothing about the check is kept: no providers.json is written
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Fatalf("the check wrote %s: %v", Path(), err)
	}
}

func TestCheckHealthTellsARefusedKeyFromAnUnreachableAddress(t *testing.T) {
	detectHome(t)
	t.Run("refused key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
		}))
		defer srv.Close()
		h := Provider{ID: "relay", Key: "sk-wrong", Chat: srv.URL + "/v1"}.CheckHealth(context.Background())
		if h.OK || h.Kind != HealthAuth || h.Status != http.StatusUnauthorized {
			t.Fatalf("health: %+v", h)
		}
		if h.Error == "" {
			t.Fatal("no words for the refusal")
		}
	})
	t.Run("a host nothing answers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL + "/v1"
		srv.Close()
		h := Provider{ID: "relay", Key: "sk-test", Chat: url}.CheckHealth(context.Background())
		if h.OK || h.Kind != HealthNetwork {
			t.Fatalf("health: %+v", h)
		}
	})
	t.Run("a server error is not the key's", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "down", http.StatusBadGateway)
		}))
		defer srv.Close()
		h := Provider{ID: "relay", Key: "sk-test", Chat: srv.URL + "/v1"}.CheckHealth(context.Background())
		if h.OK || h.Kind != HealthServer || h.Status != http.StatusBadGateway {
			t.Fatalf("health: %+v", h)
		}
	})
}

// A check the page cancels ends at once, as the network kind: the
// vendor's silence is not the key's fault.
func TestCheckHealthEndsWhenCancelled(t *testing.T) {
	detectHome(t)
	got := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(got)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Health, 1)
	go func() { done <- Provider{ID: "relay", Key: "sk-test", Chat: srv.URL + "/v1"}.CheckHealth(ctx) }()
	<-got
	cancel()
	h := <-done
	if h.Kind != HealthNetwork || h.OK {
		t.Fatalf("health: %+v", h)
	}
}

// An Anthropic base is the root /v1/messages is asked at, so its list is
// /v1/models, with the key header the API takes.
func TestCheckHealthAsksAnAnthropicBaseAtV1Models(t *testing.T) {
	detectHome(t)
	var mu sync.Mutex
	var path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path, key = r.URL.Path, r.Header.Get("x-api-key")
		mu.Unlock()
		w.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
	}))
	defer srv.Close()
	h := Provider{ID: "relay", Key: "sk-a", Anthropic: srv.URL}.CheckHealth(context.Background())
	if !h.OK || h.Kind != HealthOK {
		t.Fatalf("health: %+v", h)
	}
	mu.Lock()
	defer mu.Unlock()
	if path != "/v1/models" || key != "sk-a" {
		t.Fatalf("asked %q with x-api-key %q", path, key)
	}
}

// A sign-in owl has no list for says so, and asks nobody.
func TestCheckHealthSaysWhenItCantAsk(t *testing.T) {
	detectHome(t)
	var asked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Store(true) }))
	defer srv.Close()
	p := Provider{ID: "claude", Anthropic: srv.URL, Account: &Account{User: "ann"}}
	h := p.CheckHealth(context.Background())
	if h.OK || h.Kind != HealthUnsupported || h.Error == "" {
		t.Fatalf("health: %+v", h)
	}
	if asked.Load() {
		t.Fatal("a check it can't make asked the server")
	}
}

// ModelsURL is asked as the user set it, not guessed from the base.
func TestCheckHealthAsksModelsURLAsSet(t *testing.T) {
	detectHome(t)
	var mu sync.Mutex
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		path = r.URL.Path
		mu.Unlock()
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	h := Provider{ID: "relay", Key: "sk-test", Chat: srv.URL + "/v1", ModelsURL: srv.URL + "/custom/list"}.CheckHealth(context.Background())
	if !h.OK || h.Kind != HealthOK {
		t.Fatalf("health: %+v", h)
	}
	mu.Lock()
	defer mu.Unlock()
	if path != "/custom/list" {
		t.Fatalf("asked %q", path)
	}
}
