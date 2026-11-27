package backup

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/istoo/owl/internal/filelock"
	"github.com/istoo/owl/internal/settings"
)

// A backup without keys carries no proxy login from settings' six inline
// proxy choices either: the file isn't sealed, and a typed
// socks5://user:pass@host is a password in clear (#H45).
func TestCollectWithoutKeysStripsSettingsProxyLogins(t *testing.T) {
	home(t)
	s := settings.Settings{
		Proxy:       "socks5://alice:alice-secret@proxy.example:1080",
		PluginProxy: "http://bob:bob-secret@plugin.example:8080",
		UpdateProxy: "http://carol:carol-secret@update.example:9090",
	}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	for _, keys := range []bool{false, true} {
		b, err := Collect(keys, "test")
		if err != nil {
			t.Fatal(err)
		}
		if b.Settings == nil {
			t.Fatal("no settings in the bundle")
		}
		got := []string{
			b.Settings.Proxy, b.Settings.ProxyCustom,
			b.Settings.PluginProxy, b.Settings.PluginProxyCustom,
			b.Settings.UpdateProxy, b.Settings.UpdateProxyCustom,
		}
		for _, v := range got {
			has := strings.Contains(v, "alice-secret") || strings.Contains(v, "bob-secret") || strings.Contains(v, "carol-secret")
			if keys != has {
				t.Fatalf("keys=%v: proxy %q has a login %v", keys, v, has)
			}
			if !keys && strings.Contains(v, "@") {
				t.Fatalf("a keyless backup keeps a login in %q", v)
			}
		}
		if keys && !strings.Contains(b.Settings.Proxy, "alice:alice-secret@") {
			t.Fatalf("a keyed backup lost the proxy's login: %q", b.Settings.Proxy)
		}
	}
}

// Restoring settings merges with what the file holds when it saves, under
// the file's lock: a field another process changed while Restore waited is
// kept, not written over from a read made before (#H43).
func TestRestoreSettingsMergesUnderTheLock(t *testing.T) {
	home(t)
	if err := settings.Save(settings.Settings{Theme: "light", Name: "old"}); err != nil {
		t.Fatal(err)
	}
	incoming := settings.Settings{Theme: "dark"}
	b := Bundle{Version: 1, Settings: &incoming}
	unlock, held := filelock.Lock(settings.Path()+".lock", time.Second)
	if !held {
		t.Fatal("the test could not take the file's lock")
	}
	done := make(chan error, 1)
	go func() {
		_, err := Restore(b, Parts{Settings: true})
		done <- err
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Restore saved while another owl held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(settings.Path(), []byte(`{"theme":"light","name":"changed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Restore never went on after the lock was let go")
	}
	got := settings.Load()
	if got.Theme != "dark" {
		t.Fatalf("theme = %q, want dark", got.Theme)
	}
	if got.Name != "changed" {
		t.Fatalf("name = %q: Restore wrote over the other owl's change", got.Name)
	}
}

// The derived-key cache is bounded: a gateway syncing with many computers
// and passphrases does not keep every PBKDF2 result forever (#L14).
func TestDerivedKeysAreBounded(t *testing.T) {
	keysMu.Lock()
	keys = map[[32]byte][]byte{}
	keysMu.Unlock()
	t.Cleanup(func() {
		keysMu.Lock()
		keys = map[[32]byte][]byte{}
		keysMu.Unlock()
	})
	for i := 0; i < maxKeys*2; i++ {
		var k [32]byte
		k[0], k[1] = byte(i), byte(i>>8)
		rememberKey(k, []byte("key"))
	}
	keysMu.Lock()
	n := len(keys)
	keysMu.Unlock()
	if n > maxKeys {
		t.Fatalf("the cache holds %d keys, want at most %d", n, maxKeys)
	}
}
