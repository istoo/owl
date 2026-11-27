package gui

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/provider"
)

// H50: a key held to models, accounts, a budget, concurrency or issuing
// can't lift or widen its own limits through the management API, and
// can't add a key through it either; rotating or renaming its own key
// stays its to do.
func TestAdminAPIRestrictedKeyCannotWidenItself(t *testing.T) {
	sandboxHome(t)
	if _, err := access.Update("add-key", access.Change{Name: "Local"}); err != nil {
		t.Fatal(err)
	}
	keys, err := access.List()
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys: %v %v", keys, err)
	}
	id := keys[0].ID
	if _, err := access.Update("admin-key", access.Change{Key: id, Admin: access.AdminLocal}); err != nil {
		t.Fatal(err)
	}
	as := func(action, body string, who access.Identity) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/gateway-keys/"+action, strings.NewReader(body))
		r = r.WithContext(access.WithIdentity(r.Context(), who))
		w := httptest.NewRecorder()
		serveAdmin(w, r)
		return w
	}
	held := access.Identity{KeyID: id, Admin: access.AdminLocal, Models: []string{"openai/gpt-4o"}}
	for _, c := range []struct{ action, body string }{
		{"models-key", `{"key":"` + id + `","models":[]}`},
		{"accounts-key", `{"key":"` + id + `","accounts":[]}`},
		{"limit-key", `{"key":"` + id + `","limit":null}`},
		{"concurrency-key", `{"key":"` + id + `","maxConcurrency":0}`},
		{"access-key", `{"key":"` + id + `","readOnly":false}`},
		{"issuer-key", `{"key":"` + id + `","issues":true}`},
	} {
		if w := as(c.action, c.body, held); w.Code != http.StatusForbidden {
			t.Errorf("%s widened its own key: %d %s", c.action, w.Code, w.Body)
		}
	}
	// its own key can still be rotated and renamed
	if w := as("rotate-key", `{"key":"`+id+`"}`, held); w.Code != http.StatusOK {
		t.Errorf("rotate its own: %d %s", w.Code, w.Body)
	}
	if w := as("rename-key", `{"key":"`+id+`","name":"Mine"}`, held); w.Code != http.StatusOK {
		t.Errorf("rename its own: %d %s", w.Code, w.Body)
	}
	// and a held key can't add another through the API
	if w := as("add-key", `{"name":"Newer"}`, held); w.Code != http.StatusForbidden {
		t.Errorf("a held key added a key: %d %s", w.Code, w.Body)
	}
	// one with no limits can
	if w := as("add-key", `{"name":"Free"}`, access.Identity{KeyID: id, Admin: access.AdminAny}); w.Code != http.StatusOK {
		t.Errorf("an unrestricted key couldn't add: %d %s", w.Code, w.Body)
	}
	// a body that isn't JSON is refused, not let through (fail-closed)
	if w := as("rename-key", `{"key":`, access.Identity{KeyID: id}); w.Code != http.StatusBadRequest {
		t.Errorf("a malformed body: %d %s", w.Code, w.Body)
	}
}

// H51: a save that sends false or 0 turns the switch off or clears the
// number; only a save that leaves the key out keeps the old value.
func TestProviderSaveExplicitZeroAndFalse(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	const u = "http://127.0.0.1:1/v1"
	add := `{"id":"relay","name":"Relay","chat":"` + u + `","key":"sk-a","new":true,` +
		`"quotaRest":60,"restCap":20,"patience":30,"codexAsSent":true,"promptAsWritten":true,` +
		`"additionalTools":true,"additionalToolsOff":true,"geminiFilled":true,"quotaResetAt":"08:00"}`
	if w := post("save", add); w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	zero := `{"id":"relay","from":"relay","name":"Relay","chat":"` + u + `","key":"",` +
		`"quotaRest":0,"restCap":0,"patience":0,"codexAsSent":false,"promptAsWritten":false,` +
		`"additionalTools":false,"additionalToolsOff":false,"geminiFilled":false,"quotaResetAt":""}`
	if w := post("save", zero); w.Code != http.StatusOK {
		t.Fatalf("save zero: %d %s", w.Code, w.Body)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if p.QuotaRest != 0 || p.RestCap != 0 || p.Patience != 0 || p.CodexAsSent || p.PromptAsWritten ||
		p.AdditionalTools || p.AdditionalToolsOff || p.GeminiFilled || p.QuotaResetAt != "" {
		t.Fatalf("explicit zero and false were overwritten: %+v", p)
	}
	// a save that leaves them out keeps them
	q, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	q.QuotaRest, q.RestCap, q.Patience = 45, 10, 15
	q.CodexAsSent, q.PromptAsWritten = true, true
	if err := provider.Save(*q); err != nil {
		t.Fatal(err)
	}
	if w := post("save", `{"id":"relay","from":"relay","name":"Relay","chat":"`+u+`"}`); w.Code != http.StatusOK {
		t.Fatalf("save omitted: %d %s", w.Code, w.Body)
	}
	if p, _ = provider.Find("relay"); p.QuotaRest != 45 || p.RestCap != 10 || p.Patience != 15 || !p.CodexAsSent || !p.PromptAsWritten {
		t.Fatalf("a save that left them out lost them: %+v", p)
	}
}

// M41: a save over 8 MB is refused with 413 and writes nothing, rather
// than being cut off and stored as a form.
func TestProviderSaveTooLargeRefused(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	body := `{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","key":"k","new":true,"junk":"` +
		strings.Repeat("a", 8<<20+16) + `"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a save over the bound: %d %s", w.Code, w.Body)
	}
	if p, err := provider.Find("relay"); err == nil {
		t.Fatalf("a cut-off save was written: %+v", p)
	}
}

// H51: a preset save carries what its editor sends, rather than falling
// back to the preset's own values.
func TestProviderSavePresetCarriesRequestBody(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"or","name":"OR","preset":"openrouter","key":"k","new":true,` +
		`"keyPriceRates":{"sk-a":0.5},"extraBody":{"prompt_cache_key":"${sessionId}"},` +
		`"replyHeaders":["x-one"],"passHeaders":["x-two"]}`); w.Code != http.StatusOK {
		t.Fatalf("preset add: %d %s", w.Code, w.Body)
	}
	p, err := provider.Find("or")
	if err != nil {
		t.Fatal(err)
	}
	if p.KeyPriceRates["sk-a"] != 0.5 {
		t.Errorf("keyPriceRates lost: %v", p.KeyPriceRates)
	}
	if string(p.ExtraBody["prompt_cache_key"]) != `"${sessionId}"` {
		t.Errorf("extraBody lost: %v", p.ExtraBody)
	}
	if len(p.ReplyHeaders) != 1 || !strings.EqualFold(p.ReplyHeaders[0], "x-one") || len(p.PassHeaders) != 1 || !strings.EqualFold(p.PassHeaders[0], "x-two") {
		t.Errorf("headers lost: %v %v", p.ReplyHeaders, p.PassHeaders)
	}
}

// C6: a page's service never points at the port the server itself
// listens on, where /api is reachable without a key.
func TestPluginPageServiceRefusesOwnPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	servicePlugins(t, `{"port": `+strconv.Itoa(port)+`}`, nil)
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: serviceMux(true)}}
	srv.Start()
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/plugin-pages")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var pages []pluginPage
	if err := json.NewDecoder(res.Body).Decode(&pages); err != nil || len(pages) != 1 {
		t.Fatalf("pages: %v", err)
	}
	if pages[0].Service != "" {
		t.Fatalf("a service on owl's own port was listed: %q", pages[0].Service)
	}
	svc := srv.URL + strings.TrimSuffix(pages[0].URL, "index.html") + pageServicePath
	if r, err := http.Get(svc); err == nil && r.StatusCode != http.StatusNotFound {
		r.Body.Close()
		t.Errorf("the service on owl's own port answered %d", r.StatusCode)
	}
}

// C6: a page may not move its own service port through owl.options.set.
func TestPluginPageOptionsRefuseServicePort(t *testing.T) {
	servicePlugins(t, `{"port": 1234, "option": "port"}`, nil)
	pages := pluginPages()
	if len(pages) != 1 {
		t.Fatalf("pages: %+v", pages)
	}
	spec, _ := json.Marshal(pages[0].Spec)
	body := `{"spec":` + string(spec) + `,"options":{"port":4321}}`
	r := httptest.NewRequest("POST", "/api/plugin-pages/options", strings.NewReader(body))
	if w := do(serviceMux(true), r); w.Code != http.StatusForbidden {
		t.Fatalf("a page moved its own service port: %d %s", w.Code, w.Body)
	}
}

// H52: the web guard lets a plugin's page files through to servePluginPage
// alone, and not a path that walks out of them.
func TestWebGuardPluginPathTraversal(t *testing.T) {
	var reached bool
	h := webGuard("c", "k", 0, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, c := range []struct {
		path string
		want int
	}{
		{"/_owl/plugin/" + strings.Repeat("a", 32) + "/index.html", http.StatusOK},
		{"/_owl/plugin/" + strings.Repeat("a", 32) + "/../api/state", http.StatusNotFound},
	} {
		reached = false
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.Path = c.path
		w := do(h, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.path, w.Code, c.want)
		}
		if got := reached; got != (c.want == http.StatusOK) {
			t.Errorf("%s reached the page handler: %v", c.path, got)
		}
	}
}

// L15: the redact-actions route takes only actions owl knows, and a map
// it may hold.
func TestSettingsRedactActionsValidated(t *testing.T) {
	sandboxHome(t)
	h := Handler(nil, nil)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/settings/redact-actions", strings.NewReader(body)))
		return w
	}
	if w := post(`{"actions":{"API_KEY":"burn"}}`); w.Code == http.StatusOK {
		t.Errorf("an unknown action was taken: %d %s", w.Code, w.Body)
	}
	many := map[string]string{}
	for i := 0; i < 200; i++ {
		many["K"+strconv.Itoa(i)] = "warn"
	}
	b, _ := json.Marshal(map[string]any{"actions": many})
	if w := post(string(b)); w.Code == http.StatusOK {
		t.Errorf("an unbounded map was taken: %d", w.Code)
	}
	if w := post(`{"actions":{"API_KEY":"warn"}}`); w.Code != http.StatusOK {
		t.Errorf("a known action was refused: %d %s", w.Code, w.Body)
	}
}
