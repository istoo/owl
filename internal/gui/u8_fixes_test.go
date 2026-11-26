package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/istoo/owl/internal/davsync"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/settings"
)

// u8Home gives the test a machine of its own.
func u8Home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	return h
}

func u8Post(t *testing.T, h http.Handler, path, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// A v0.2.27 form sends only the address, the password and the passphrase:
// the parts' own switches and the subscriptions left out are not in it,
// and saving must keep them, not turn them into "yes" (#66).
func TestDavSyncSaveKeepsPartsTheFormDoesNotSend(t *testing.T) {
	u8Home(t)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.NotFound(rw, r)
	}))
	defer srv.Close()

	no, yes := false, true
	base := davsync.Config{
		URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "p",
		Providers: &no, Settings: &yes, Profiles: &no, Memory: &no,
		SignInsExcept: []string{"claude"},
	}
	if err := davsync.Configure(base); err != nil {
		t.Fatal(err)
	}

	h := Handler(nil, nil)
	// the old form's body: no providers, settings, profiles, memory or
	// signInsExcept at all
	code, body := u8Post(t, h, "/api/davsync/save", `{"url":"`+srv.URL+`/dav/","user":"me","passphrase":"p"}`)
	if code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	got, ok := davsync.Load()
	if !ok {
		t.Fatal("sync is off after a save")
	}
	if got.Providers == nil || *got.Providers {
		t.Errorf("providers = %v, want false kept", got.Providers)
	}
	if got.Settings == nil || !*got.Settings {
		t.Errorf("settings = %v, want true kept", got.Settings)
	}
	if got.Profiles == nil || *got.Profiles {
		t.Errorf("profiles = %v, want false kept", got.Profiles)
	}
	if got.Memory == nil || *got.Memory {
		t.Errorf("memory = %v, want false kept", got.Memory)
	}
	if len(got.SignInsExcept) != 1 || got.SignInsExcept[0] != "claude" {
		t.Errorf("signInsExcept = %v, want claude kept", got.SignInsExcept)
	}

	// what the form does send stands, an empty list included
	if code, body := u8Post(t, h, "/api/davsync/save", `{"url":"`+srv.URL+`/dav/","user":"me","passphrase":"p","providers":true,"signInsExcept":[]}`); code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	got, _ = davsync.Load()
	if got.Providers == nil || !*got.Providers {
		t.Errorf("providers = %v, want true sent", got.Providers)
	}
	if len(got.SignInsExcept) != 0 {
		t.Errorf("signInsExcept = %v, want none sent", got.SignInsExcept)
	}
}

// Routing's editor in v0.2.27 never carried the group's note, NoSink,
// intent sure, compact, ultra or effort maps: saving its routing must not
// clear them (#67).
func TestGroupSaveKeepsFieldsTheFormDoesNotSend(t *testing.T) {
	u8Home(t)
	if err := provider.Save(provider.Provider{ID: "p", Name: "P", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"gpt-4o", "m2"}}); err != nil {
		t.Fatal(err)
	}
	sure := 0.7
	g := provider.Group{
		ID: "g", Name: "G",
		Members:    []string{"p/gpt-4o:fast", "p/m2"},
		Fast:       []string{"p/gpt-4o:fast"},
		Ultra:      []string{"p/gpt-4o"},
		Note:       "keep me",
		NoSink:     true,
		IntentSure: &sure,
		Compact:    120000,
		EffortMaps: map[string]map[string]string{"p/m2": {"high": "xhigh"}},
	}
	if err := provider.SaveGroup(g); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	// the old editor's Save: routing picked, the six fields not sent
	code, body := u8Post(t, h, "/api/groups/save", `{"id":"g","from":"g","name":"G","members":["p/gpt-4o:fast","p/m2"],"fast":["p/gpt-4o:fast"],"routing":"rotate"}`)
	if code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	stored, err := provider.StoredGroups()
	if err != nil {
		t.Fatal(err)
	}
	var got provider.Group
	found := false
	for _, x := range stored {
		if x.ID == "g" {
			got, found = x, true
		}
	}
	if !found {
		t.Fatal("group gone")
	}
	if got.Note != "keep me" {
		t.Errorf("note = %q, want kept", got.Note)
	}
	if !got.NoSink {
		t.Error("noSink cleared")
	}
	if got.IntentSure == nil || *got.IntentSure != 0.7 {
		t.Errorf("intentSure = %v, want 0.7 kept", got.IntentSure)
	}
	if got.Compact != 120000 {
		t.Errorf("compact = %d, want kept", got.Compact)
	}
	if len(got.Ultra) != 1 {
		t.Errorf("ultra = %v, want kept", got.Ultra)
	}
	if got.EffortMaps["p/m2"]["high"] != "xhigh" {
		t.Errorf("effortMaps = %v, want kept", got.EffortMaps)
	}
	if got.Routing != provider.Rotate {
		t.Errorf("routing = %q, want the one sent", got.Routing)
	}

	// one sent as empty is cleared, not kept
	code, body = u8Post(t, h, "/api/groups/save", `{"id":"g","from":"g","name":"G","members":["p/gpt-4o:fast","p/m2"],"fast":["p/gpt-4o:fast"],"routing":"rotate","note":""}`)
	if code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	stored, _ = provider.StoredGroups()
	for _, x := range stored {
		if x.ID == "g" {
			got = x
		}
	}
	if got.Note != "" {
		t.Errorf("note = %q, want cleared", got.Note)
	}
}

// The v0.2.27 provider editor doesn't send billing: a save keeps the mark
// it didn't carry (#68).
func TestProviderSaveKeepsBillingTheEditorDoesNotSend(t *testing.T) {
	u8Home(t)
	if err := provider.Save(provider.Provider{ID: "box", Name: "Box", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"m"}, Billing: "plan"}); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	code, body := u8Post(t, h, "/api/provider/save", `{"id":"box","from":"box","name":"Box","chat":"http://127.0.0.1:1/v1","models":["m"]}`)
	if code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	p, err := provider.Find("box")
	if err != nil {
		t.Fatal(err)
	}
	if p.Billing != "plan" {
		t.Fatalf("billing = %q, want plan kept", p.Billing)
	}
	// and one sent stands
	if code, body := u8Post(t, h, "/api/provider/save", `{"id":"box","from":"box","name":"Box","chat":"http://127.0.0.1:1/v1","models":["m"],"billing":"payg"}`); code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	if p, _ := provider.Find("box"); p.Billing != "payg" {
		t.Fatalf("billing = %q, want payg sent", p.Billing)
	}
}

// A folder typed into the automatic backup's row is saved, one that can't
// be written to is refused, and "" is the default folder (#76).
func TestAutoBackupSetTakesATypedFolder(t *testing.T) {
	u8Home(t)
	mux := http.NewServeMux()
	backupRoutes(mux, pickFolder{dir: t.TempDir()})
	picked := t.TempDir()
	code, body := u8Post(t, mux, "/api/autobackup/set", `{"dir":"`+picked+`"}`)
	if code != http.StatusOK {
		t.Fatalf("set: %d %s", code, body)
	}
	var v struct {
		Custom bool   `json:"custom"`
		Dir    string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || !v.Custom || v.Dir != tilde(picked) {
		t.Fatalf("set: %s", body)
	}
	// a file in the way: the folder can't be written to
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, body := u8Post(t, mux, "/api/autobackup/set", `{"dir":"`+filepath.Join(file, "sub")+`"}`); code == http.StatusOK {
		t.Fatalf("an unwritable folder took: %s", body)
	}
	// "" is the default folder again
	code, body = u8Post(t, mux, "/api/autobackup/set", `{"dir":""}`)
	if code != http.StatusOK {
		t.Fatalf("default: %d %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.Custom {
		t.Fatalf("default folder kept: %s", body)
	}
}

// ?k= with no path is another site, not where the sign-in comes back to
// (#77).
func TestWebGuardKRedirectStaysOnThisServer(t *testing.T) {
	const key = "the-key"
	h := webGuard("owl_web", key, time.Hour, nil, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	}))
	for _, c := range []struct {
		path string
		want string
	}{
		{"/", "/"},
		{"/settings", "/settings"},
		{"//evil.com/", "/"},
		{`/\evil.com/`, "/"},
	} {
		r := httptest.NewRequest("GET", "/?k="+key, nil)
		r.URL.Path = c.path
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%q: %d", c.path, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != c.want {
			t.Errorf("%q: Location = %q, want %q", c.path, got, c.want)
		}
	}
}

// Two settings routes changing one field each at the same time both stand:
// neither writes the other's change back to what it read before (#72).
func TestSettingsRoutesDoNotLoseEachOther(t *testing.T) {
	u8Home(t)
	h := Handler(nil, nil)
	for i := 0; i < 60; i++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			codes[0], _ = u8Post(t, h, "/api/settings/quota-left", `{"on":true}`)
		}()
		go func() {
			defer wg.Done()
			<-start
			codes[1], _ = u8Post(t, h, "/api/settings/guide", `{"hidden":true}`)
		}()
		close(start)
		wg.Wait()
		if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
			t.Fatalf("round %d: %v", i, codes)
		}
		if s := settings.Load(); !s.QuotaLeft || !s.GuideHidden {
			t.Fatalf("round %d: quotaLeft %v, guideHidden %v: one change lost the other", i, s.QuotaLeft, s.GuideHidden)
		}
	}
}
