package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The hourly keep-alive passes over a Codex account the vendor has already
// refused: left in, it took the one renewal each look has, and the healthy
// account behind it never had its own (#17).
func TestKeepAliveSkipsLapsedCodex(t *testing.T) {
	signIn(t)
	rememberLogins(true) // me@ is Codex's own, left to Codex

	var got []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": body["refresh_token"] + "-new",
		})
	}))
	defer fake.Close()
	oldURL := codexTokenURL
	codexTokenURL = fake.URL
	renewFailed.Lock()
	renewFailed.m = nil
	renewFailed.Unlock()
	t.Cleanup(func() {
		codexTokenURL = oldURL
		renewFailed.Lock()
		renewFailed.m = nil
		renewFailed.Unlock()
	})

	old := time.Now().Add(-9 * 24 * time.Hour)
	auth := func(refresh string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{
			"auth_mode": "chatgpt",
			"tokens": map[string]any{
				"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
				"refresh_token": refresh, "account_id": "acct-" + refresh,
			},
			"last_refresh": old.Format(time.RFC3339Nano),
		})
		return b
	}
	loginsMu.Lock()
	ls := append(readLogins(),
		savedLogin{Agent: "codex", User: "gone@example.com", Auth: auth("r-gone"), Seen: old, Lapsed: "the vendor refused it"},
		savedLogin{Agent: "codex", User: "fresh@example.com", Auth: auth("r-fresh"), Seen: old},
	)
	err := writeLogins(ls)
	loginsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	rs := renewLogins(context.Background(), keepAliveEvery, 1)
	if len(got) != 1 || got[0] != "r-fresh" {
		t.Fatalf("the look refreshed %v, want the healthy account only", got)
	}
	renewed := false
	for _, r := range rs {
		if r.User == "fresh@example.com" && r.Renewed {
			renewed = true
		}
		if r.User == "gone@example.com" {
			t.Errorf("the lapsed account was looked at: %+v", r)
		}
	}
	if !renewed {
		t.Fatalf("the healthy account behind the lapsed one wasn't renewed: %+v", rs)
	}
	// a person asking (every 0) still retries a lapsed one
	got = nil
	_ = renewLogins(context.Background(), 0, 0)
	found := false
	for _, r := range got {
		if r == "r-gone-new" || r == "r-gone" {
			found = true
		}
	}
	if !found {
		t.Errorf("RenewLogins(ctx, 0) didn't retry the lapsed account: %v", got)
	}
}
