package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// With the pi-owl extension installed, owl's own providers.owl
// block in models.json is taken out (a copy kept), the user's other
// providers stay, and a block that is not owl's stays too.
func TestPiOwlExtensionTakesOverTheBlock(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	os.MkdirAll(dir, 0o755)
	models := filepath.Join(dir, "models.json")
	settings := filepath.Join(dir, "settings.json")
	os.WriteFile(settings, []byte(`{"packages":[]}`), 0o644)
	os.WriteFile(models, []byte(`{"providers":{"mine":{"baseUrl":"http://x/v1"}}}`), 0o644)
	if err := provider.Save(provider.Provider{ID: "p", Name: "P", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	// plain Pi: the block is written
	if err := pi(home).Field("model").Set("owl/p/m"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(models), `"owl"`) {
		t.Fatalf("without the extension the block is written: %s", readFile(models))
	}
	// the extension installed: the next sync drops it
	os.WriteFile(settings, []byte(`{"packages":["npm:pi-owl"],"defaultProvider":"owl"}`), 0o644)
	if err := pi(home).Sync(); err != nil {
		t.Fatal(err)
	}
	got := readFile(models)
	if strings.Contains(got, `"owl"`) || !strings.Contains(got, `"mine"`) {
		t.Fatalf("block not dropped or mine lost: %s", got)
	}
	if st, err := os.Stat(models + ".before-pi-owl"); err != nil {
		t.Fatalf("no backup: %v", err)
	} else if runtime.GOOS != "windows" && st.Mode().Perm()&^0o600 != 0 {
		t.Fatalf("backup mode %v", st.Mode())
	}
	if msg := pi(home).Check(); msg != "" {
		t.Fatalf("check: %s", msg)
	}
	// a block the user wrote (another address) is left alone
	os.WriteFile(models, []byte(`{"providers":{"owl":{"baseUrl":"http://example.com/v1","apiKey":"theirs"}}}`), 0o644)
	if err := pi(home).Sync(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(models), "example.com") {
		t.Fatalf("user's block removed: %s", readFile(models))
	}
}
