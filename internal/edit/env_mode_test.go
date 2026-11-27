package edit

import (
	"os"
	"path/filepath"
	"testing"
)

// A dotenv file SetEnvFile makes or rewrites holds a key, so it is the
// user's alone (0600), and an older 0644 one is narrowed.
func TestSetEnvFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := SetEnvFile(p, KV{Path: "GEMINI_API_KEY", Value: "sk-secret"}); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetEnvFile(p, KV{Path: "GEMINI_API_KEY", Value: "sk-secret"}); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
	if err := SetEnvFile(p, KV{Path: "GOOGLE_GEMINI_BASE_URL", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, p, 0o600)
}
