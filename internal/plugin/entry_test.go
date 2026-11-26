package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// entryFixture is a plugin folder whose package.json names entry under
// the key ("middleware", "agent" or "hooks").
func entryFixture(t *testing.T, dir, key, entry string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := map[string]string{"middleware": "middleware", "agent": "agent", "hooks": "hooks"}[key]
	b, err := json.Marshal(map[string]any{"magpie": map[string]string{name: entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// entrySays is what the plugin at dir names for key: the file Middleware,
// Agent and Gateway return, "" for none.
func entrySays(key, dir string) string {
	switch key {
	case "middleware":
		f, _ := Middleware(dir)
		return f
	case "agent":
		f, _ := Agent(dir)
		return f
	case "hooks":
		return Gateway(dir)
	}
	return ""
}

// A package.json entry is only used when it stays inside the plugin: a
// "..", an absolute path, or a symlink leading out of the folder is no
// entry, whether it names a middleware, an agent or request hooks (#44).
func TestEntryStaysInsideThePlugin(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside.js")
	if err := os.WriteFile(outside, []byte("export default {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"middleware", "agent", "hooks"} {
		for name, entry := range map[string]string{
			"dotdot":    "../outside.js",
			"nested":    "./sub/../../outside.js",
			"absolute":  outside,
			"backslash": `..\outside.js`,
		} {
			dir := filepath.Join(base, key+"-"+name)
			entryFixture(t, dir, key, entry)
			if got := entrySays(key, dir); got != "" {
				t.Errorf("%s %s: entry %q came back as %q", key, name, entry, got)
			}
		}
		// a symlink out of the folder is refused too
		dir := filepath.Join(base, key+"-symlink")
		entryFixture(t, dir, key, "./link.js")
		if err := os.Symlink(outside, filepath.Join(dir, "link.js")); err != nil {
			t.Logf("symlinks aren't made here: %v", err)
			continue
		}
		if got := entrySays(key, dir); got != "" {
			t.Errorf("%s through a symlink out of the plugin came back as %q", key, got)
		}
	}
}

// An entry inside the folder is still taken, and so is one that isn't
// there yet: owl says that when it loads it, rather than calling the
// package no plugin at all.
func TestEntryInsideStillTaken(t *testing.T) {
	base := t.TempDir()
	for _, key := range []string{"middleware", "agent", "hooks"} {
		dir := filepath.Join(base, key)
		entryFixture(t, dir, key, "./entry.js")
		if err := os.WriteFile(filepath.Join(dir, "entry.js"), []byte("export default {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := entrySays(key, dir); got == "" {
			t.Errorf("%s: entry.js beside package.json wasn't taken", key)
		}

		dir = filepath.Join(base, key+"-missing")
		entryFixture(t, dir, key, "./not-there.js")
		if got := entrySays(key, dir); got == "" || filepath.Base(got) != "not-there.js" {
			t.Errorf("%s: a missing entry inside the folder = %q", key, got)
		}
	}
}
