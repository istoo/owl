package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/istoo/owl/internal/agentenv"
)

func azureHome(t *testing.T) {
	t.Helper()
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("APPDATA", filepath.Join(h, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
}

// Whatever the portal or another app gave — the resource's endpoint, its
// /openai, the v1 base, a classic deployment's URL, a newer resource's
// host, or only its name — owl asks the resource's v1 API.
func TestAzureBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://contoso.openai.azure.com":              "https://contoso.openai.azure.com/openai/v1",
		"https://contoso.openai.azure.com/":             "https://contoso.openai.azure.com/openai/v1",
		"https://contoso.openai.azure.com/openai":       "https://contoso.openai.azure.com/openai/v1",
		" https://Contoso.openai.azure.com/openai/v1/ ": "https://contoso.openai.azure.com/openai/v1",
		"https://contoso.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-10-21": "https://contoso.openai.azure.com/openai/v1",
		"contoso.openai.azure.com":                       "https://contoso.openai.azure.com/openai/v1",
		"https://eastus2-x.cognitiveservices.azure.com/": "https://eastus2-x.cognitiveservices.azure.com/openai/v1",
		"https://proj.services.ai.azure.com/openai/v1":   "https://proj.services.ai.azure.com/openai/v1",
		"contoso": "https://contoso.openai.azure.com/openai/v1",
	} {
		if got, ok := AzureBase(in); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "https://api.openai.com/v1", "https://openai.azure.com", "https://apim.example.com/openai"} {
		if got, ok := AzureBase(in); ok {
			t.Errorf("%q taken for a resource: %q", in, got)
		}
	}
	if !(Provider{Chat: "https://x.openai.azure.com/openai/v1"}).IsAzure() || (Provider{Chat: "https://api.openai.com/v1"}).IsAzure() {
		t.Error("IsAzure by host")
	}
}

// The preset is filled in with the resource's endpoint alone: saved, both
// chat completions and Responses are on its v1 API. Without one it isn't
// saved. The key goes in api-key alone, never as a Bearer.
func TestAzurePresetSaved(t *testing.T) {
	azureHome(t)
	p, err := FromPreset(AzurePreset)
	if err != nil {
		t.Fatal(err)
	}
	pr := Preset(AzurePreset)
	if pr.Endpoint == "" || pr.EndpointHint == "" || p.Chat != "" || p.Responses != "" {
		t.Fatalf("preset: %+v %+v", pr, p)
	}
	p.Key = "k"
	if err := Save(p); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("saved without an endpoint: %v", err)
	}
	p.Chat, p.Responses = "https://contoso.openai.azure.com/", "https://contoso.openai.azure.com/"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := Find(AzurePreset)
	if err != nil {
		t.Fatal(err)
	}
	if q.Chat != "https://contoso.openai.azure.com/openai/v1" || q.Responses != q.Chat || q.Anthropic != "" || !q.IsAzure() {
		t.Fatalf("saved: %+v", q)
	}
	for _, proto := range []Protocol{Chat, Responses} {
		if h := AuthHeaders(*q, proto); h["api-key"] != "k" || len(h) != 1 {
			t.Errorf("%s: %v", proto, h)
		}
	}
	// a deployment named for a GPT model on Responses, like OpenAI's own;
	// its test asks max_completion_tokens
	if q.Native("gpt-5-codex") != Responses || q.Native("my-deploy") != Chat {
		t.Errorf("native: %s %s", q.Native("gpt-5-codex"), q.Native("my-deploy"))
	}
	url, body := tinyBody(*q, Chat, "my-deploy")
	if url != "https://contoso.openai.azure.com/openai/v1/chat/completions" || !strings.Contains(body, `"max_completion_tokens":16`) {
		t.Errorf("test: %s %s", url, body)
	}
	if url, _ := tinyBody(*q, Responses, "gpt-5-codex"); url != "https://contoso.openai.azure.com/openai/v1/responses" {
		t.Errorf("test: %s", url)
	}
	// nothing offered from the catalog before the resource's list: a
	// model not deployed there would be turned away
	if ms := q.Available(); len(ms) != 0 {
		t.Errorf("available before a list: %d", len(ms))
	}
}

// The resource's deployments are its model ids: asked on the data plane
// with the key in api-key, no Authorization, and the v1 API's model list
// where that is refused.
func TestAzureModels(t *testing.T) {
	azureHome(t)
	var mu sync.Mutex
	var asked []string
	deployments := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RequestURI())
		mu.Unlock()
		if r.Header.Get("api-key") != "k" || r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/openai/deployments" && r.URL.Query().Get("api-version") == "2022-12-01" && deployments:
			w.Write([]byte(`{"data":[{"id":"team-gpt","model":"gpt-5","object":"deployment","status":"succeeded"},{"id":"gpt-5-codex","model":"gpt-5-codex","object":"deployment","status":"succeeded"}],"object":"list"}`))
		case r.URL.Path == "/openai/v1/models":
			w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1","object":"model"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"404","message":"Resource not found"}}`))
		}
	}))
	defer srv.Close()

	p, _ := FromPreset(AzurePreset)
	p.Key = "k"
	p.Chat, p.Responses = srv.URL+"/openai/v1", srv.URL+"/openai/v1"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	q, _ := Find(AzurePreset)
	ms, err := q.Fetch(context.Background())
	if err != nil || len(ms) != 2 || ms[0].ID != "team-gpt" || ms[1].ID != "gpt-5-codex" {
		t.Fatalf("deployments: %v %+v", err, ms)
	}
	if asked[0] != "/openai/deployments?api-version=2022-12-01" {
		t.Errorf("asked %v", asked)
	}
	if got := q.Available(); len(got) != 2 {
		t.Errorf("available %+v", got)
	}

	deployments = false
	asked = nil
	ms, err = q.Fetch(context.Background())
	if err != nil || len(ms) != 1 || ms[0].ID != "gpt-4.1" {
		t.Fatalf("v1 models: %v %+v", err, ms)
	}
	if len(asked) != 2 || asked[1] != "/openai/v1/models" {
		t.Errorf("asked %v", asked)
	}

	q.Key = "wrong"
	if _, err := q.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "deployments' names") {
		t.Errorf("refused: %v", err)
	}
}

// Another app's Azure OpenAI entry is brought in as the preset, at its
// resource's v1 API, where owl used to leave it out.
func TestImportAzure(t *testing.T) {
	azureHome(t)
	cfg, _ := os.UserConfigDir()
	sqliteFixture(t, filepath.Join(cfg, "alma", "chat_threads.db"),
		`CREATE TABLE providers (id TEXT PRIMARY KEY, name TEXT, type TEXT, api_key TEXT, models TEXT, base_url TEXT, enabled INTEGER, created_at TEXT, api_format TEXT, is_response_api INTEGER, custom_headers TEXT)`,
		`INSERT INTO providers VALUES ('z1','Azure','azure','az-key','["team-gpt"]','https://contoso.openai.azure.com/openai',1,'1',NULL,0,NULL)`,
		`INSERT INTO providers VALUES ('z2','Work Azure','azure','az-2','[]','contoso2',1,'2',NULL,0,NULL)`,
		`INSERT INTO providers VALUES ('z3','Azure','azure','az-3','[]',NULL,1,'3',NULL,0,NULL)`,
	)
	items := itemsOf(t, "alma")
	it := items["z1"]
	if it.Skip != "" || it.Provider.Preset != AzurePreset || it.Provider.Name != "Azure OpenAI" || it.Provider.Key != "az-key" ||
		it.Provider.Chat != "https://contoso.openai.azure.com/openai/v1" || it.Provider.Responses != it.Provider.Chat ||
		len(it.Provider.Models) != 1 || it.Provider.Icon != "azure-color" {
		t.Fatalf("azure: %+v", it)
	}
	if it := items["z2"]; it.Skip != "" || it.Provider.Name != "Work Azure" || it.Provider.Chat != "https://contoso2.openai.azure.com/openai/v1" {
		t.Fatalf("by name: %+v", it)
	}
	if it := items["z3"]; it.Skip == "" {
		t.Fatalf("no endpoint offered: %+v", it)
	}

	// an entry at a resource's host from an app that doesn't say Azure
	im, skip := imported("My Azure", "k", endpoints{chat: "https://contoso.openai.azure.com/openai/deployments/gpt-4o?api-version=2024-10-21"}, nil)
	if skip != "" || im.Preset != AzurePreset || im.Chat != "https://contoso.openai.azure.com/openai/v1" || im.Responses != im.Chat {
		t.Fatalf("imported: %q %+v", skip, im)
	}
}

// azureServer saves an Azure provider at srv and returns it, so a test
// asks the v1 API the way a real resource is asked.
func azureServer(t *testing.T, srv *httptest.Server) Provider {
	t.Helper()
	p, _ := FromPreset(AzurePreset)
	p.Key = "k"
	p.Chat, p.Responses = srv.URL+"/openai/v1", srv.URL+"/openai/v1"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := Find(AzurePreset)
	if err != nil {
		t.Fatal(err)
	}
	return *q
}

// A deployment made after the data plane's list last refreshed is not in
// that list, but the model's own empty chat says the resource serves it.
// The catalog's older, dated and non-chat models are not asked about, and
// a model the resource does not serve is not added.
func TestAzureProbesDeploymentsTheListOmits(t *testing.T) {
	azureHome(t)
	future := time.Now().Add(365 * 24 * time.Hour).Unix()
	var mu sync.Mutex
	posted := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/openai/deployments":
			w.Write([]byte(`{"object":"list","data":[{"id":"gpt-6.1-terra","model":"gpt-6.1-terra","object":"deployment","status":"succeeded"}]}`))
		case r.URL.Path == "/openai/v1/models":
			fmt.Fprintf(w, `{"object":"list","data":[
				{"id":"gpt-6.1-terra","created_at":1000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-6.2-sol","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-6.2-astra","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-6.2-sol-2026-11-17","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-5-old","created_at":500,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-embed","created_at":2000,"capabilities":{"chat_completion":false},"deprecation":{"inference":%d}},
				{"id":"claude-opus-5-5","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-gone","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},
				{"id":"gpt-broken","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}}
			]}`, future, future, future, future, future, future, future, future, future)
		case r.URL.Path == "/openai/v1/chat/completions":
			var body struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			posted[body.Model]++
			mu.Unlock()
			switch body.Model {
			case "gpt-6.2-sol", "gpt-6.2-astra":
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"message":"Invalid empty chat. Expected at least one message.","type":"invalid_request_error"}}`))
			case "claude-opus-5-5":
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"message":"The model 'claude-opus-5-5' version '2' does not support deploymentless inference."}}`))
			case "gpt-gone":
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":{"code":"DeploymentNotFound","message":"The API deployment for this resource does not exist."}}`))
			default:
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	q := azureServer(t, srv)
	ms, err := q.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range ms {
		got[m.ID] = true
	}
	for _, id := range []string{"gpt-6.1-terra", "gpt-6.2-sol", "gpt-6.2-astra"} {
		if !got[id] {
			t.Errorf("missing %q in %v", id, got)
		}
	}
	for _, id := range []string{"gpt-5-old", "gpt-6.2-sol-2026-11-17", "gpt-embed", "claude-opus-5-5", "gpt-gone", "gpt-broken"} {
		if got[id] {
			t.Errorf("%q added though the resource does not serve it: %v", id, got)
		}
	}
	if len(ms) != 3 {
		t.Errorf("listed %+v", ms)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 5 || posted["gpt-6.2-sol"] != 1 || posted["gpt-6.2-astra"] != 1 {
		t.Errorf("asked %v", posted)
	}
	for _, id := range []string{"gpt-6.1-terra", "gpt-6.2-sol-2026-11-17", "gpt-5-old", "gpt-embed"} {
		if posted[id] != 0 {
			t.Errorf("asked about %q: %v", id, posted)
		}
	}
}

// A probe that never answers is unknown, not a deployment: the list the
// data plane sent is returned as it is, and the new model is not added.
func TestAzureProbeTimeoutKeepsList(t *testing.T) {
	azureHome(t)
	future := time.Now().Add(365 * 24 * time.Hour).Unix()
	asked := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/openai/deployments":
			w.Write([]byte(`{"object":"list","data":[{"id":"gpt-6.1-terra","model":"gpt-6.1-terra","object":"deployment"}]}`))
		case r.URL.Path == "/openai/v1/models":
			fmt.Fprintf(w, `{"data":[{"id":"gpt-6.1-terra","created_at":1000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}},{"id":"gpt-6.2-sol","created_at":2000,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}}]}`, future, future)
		case r.URL.Path == "/openai/v1/chat/completions":
			select {
			case asked <- struct{}{}:
			default:
			}
			// never answers until the test lets it: the probe must end on
			// its own context, not on a reply
			<-release
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	defer close(release)

	q := azureServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-asked:
			cancel()
		case <-ctx.Done():
		}
	}()
	ms, err := q.Fetch(ctx)
	if err != nil {
		t.Fatalf("a probe's timeout failed the list: %v", err)
	}
	if len(ms) != 1 || ms[0].ID != "gpt-6.1-terra" {
		t.Fatalf("timeout: %+v", ms)
	}
}

// The number of deployments a refresh asks about is bounded, and the
// newest come first, so the cap never drops a model made after the ones
// kept.
func TestAzureProbeMost(t *testing.T) {
	azureHome(t)
	future := time.Now().Add(365 * 24 * time.Hour).Unix()
	var mu sync.Mutex
	asks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/openai/deployments":
			w.Write([]byte(`{"object":"list","data":[{"id":"base","model":"base","object":"deployment"}]}`))
		case r.URL.Path == "/openai/v1/models":
			var b strings.Builder
			fmt.Fprintf(&b, `{"data":[{"id":"base","created_at":100,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}}`, future)
			for i := 0; i < azureProbeMost+8; i++ {
				fmt.Fprintf(&b, `,{"id":"new-%02d","created_at":%d,"capabilities":{"chat_completion":true},"deprecation":{"inference":%d}}`, i, 200+i, future)
			}
			b.WriteString(`]}`)
			w.Write([]byte(b.String()))
		case r.URL.Path == "/openai/v1/chat/completions":
			mu.Lock()
			asks++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"Invalid empty chat."}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	q := azureServer(t, srv)
	ms, err := q.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := asks
	mu.Unlock()
	if n != azureProbeMost {
		t.Errorf("asked about %d deployments, want %d", n, azureProbeMost)
	}
	if len(ms) != 1+azureProbeMost {
		t.Errorf("listed %d models, want %d", len(ms), 1+azureProbeMost)
	}
}
