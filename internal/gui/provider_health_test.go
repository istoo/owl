package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/istoo/owl/internal/provider"
)

// The Providers list's Check all asks one model list with the saved key,
// and writes nothing: the check is a look, not a setting.
func TestProviderHealthChecksTheListAndWritesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var mu sync.Mutex
	var asked []string
	var status atomic.Int32
	status.Store(http.StatusOK)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if status.Load() != http.StatusOK {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, int(status.Load()))
			return
		}
		w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer up.Close()
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	if w := post("save", `{"id":"relay","name":"Relay","key":"sk-saved","chat":"`+up.URL+`/v1","models":["m1"],"new":true}`); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	before, err := os.ReadFile(provider.Path())
	if err != nil {
		t.Fatal(err)
	}
	// the save asked its list; the checks are what is counted from here
	mu.Lock()
	asked = nil
	mu.Unlock()
	check := func(want int) provider.Health {
		t.Helper()
		w := post("test", `{"id":"relay","health":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("health: %d %s", w.Code, w.Body)
		}
		var h provider.Health
		if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
			t.Fatal(err)
		}
		if h.Status != want {
			t.Fatalf("health: %+v", h)
		}
		return h
	}
	if h := check(http.StatusOK); !h.OK || h.Kind != provider.HealthOK {
		t.Fatalf("health: %+v", h)
	}
	status.Store(http.StatusUnauthorized)
	if h := check(http.StatusUnauthorized); h.OK || h.Kind != provider.HealthAuth {
		t.Fatalf("refused: %+v", h)
	}
	mu.Lock()
	got := append([]string(nil), asked...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "GET /v1/models Bearer sk-saved" || got[1] != got[0] {
		t.Fatalf("asked %v", got)
	}
	after, err := os.ReadFile(provider.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("the check changed providers.json:\n%s\n%s", before, after)
	}
}

// Stop ends a check under way: its request is cancelled on the server, so
// no vendor keeps being asked after the page stopped listening.
func TestProviderHealthStopEndsTheRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got, ended := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		close(got)
		<-r.Context().Done()
		close(ended)
	}))
	defer up.Close()
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	if w := post("save", `{"id":"relay","name":"Relay","key":"sk-saved","chat":"`+up.URL+`/v1","models":["m1"],"new":true}`); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	done := make(chan provider.Health, 1)
	go func() {
		w := post("test", `{"id":"relay","health":true,"run":"r1"}`)
		var h provider.Health
		json.Unmarshal(w.Body.Bytes(), &h)
		done <- h
	}()
	<-got
	if w := post("teststop", `{"run":"r1"}`); w.Code != http.StatusNoContent {
		t.Fatalf("teststop: %d %s", w.Code, w.Body)
	}
	if h := <-done; h.Kind != provider.HealthNetwork || h.OK {
		t.Fatalf("stopped: %+v", h)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the vendor's request was not ended")
	}
}
