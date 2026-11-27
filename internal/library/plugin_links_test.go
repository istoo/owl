package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/settings"
)

// A plugin's files are read through inside: a skills folder, an agent or
// a manifest that is a link out of the plugin's folder is not read
// (#H24).
func TestPluginDoesNotReadThroughLinksOut(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret/SKILL.md"), "---\nname: leak\ndescription: Leak\n---\n")
	write(t, filepath.Join(outside, "agent.md"), "---\nname: leak\ndescription: Leak\n---\n")
	write(t, filepath.Join(outside, "mcp.json"), `{"mcpServers":{"leak":{"command":"/bin/sh"}}}`)
	write(t, filepath.Join(outside, "hooks.json"), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo leak"}]}]}}`)
	write(t, filepath.Join(dir, ".claude-plugin/plugin.json"), `{"name":"acme"}`)
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"skills/leak":      filepath.Join(outside, "secret"),
		"agents/leak.md":   filepath.Join(outside, "agent.md"),
		".mcp.json":        filepath.Join(outside, "mcp.json"),
		"hooks/hooks.json": filepath.Join(outside, "hooks.json"),
	} {
		if link == "hooks/hooks.json" {
			if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(link))); err != nil {
			t.Skipf("symlinks: %v", err)
		}
	}
	pl, err := readPlugin(dir, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.skills) != 0 || len(pl.agents) != 0 || len(pl.servers) != 0 || len(pl.hooks) != 0 {
		t.Fatalf("read through links out: %+v", pl)
	}
}

// A mirror leaves out a link that leads out of the folder it mirrors, as
// a copy does (#H25).
func TestMirrorLeavesOutALinkOut(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), "secret")
	write(t, filepath.Join(from, "own.txt"), "own")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(from, "escape")); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(from, "own.txt"), filepath.Join(from, "inside")); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	if err := mirrorDir(from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(to, "escape")); err == nil {
		t.Error("the mirror made a link out of the folder")
	}
	if got, err := os.Readlink(filepath.Join(to, "inside")); err != nil || got != filepath.Join(from, "own.txt") {
		t.Errorf("the mirror lost a link inside: %q %v", got, err)
	}
}

// A version whose every part clashes is refused before the old one is
// touched: the plugin and its files stay as they are (#M37b).
func TestPluginUpgradeConflictLeavesTheOldOne(t *testing.T) {
	h := sandbox(t)
	pluginAgents(t, h)
	src := filepath.Join(t.TempDir(), "acme")
	writePlugin(t, src)
	ok(t)(InstallPlugins(src, nil, []string{"claude"}))
	oldDir := filepath.Join(pluginDir("acme-kit"), "skills/tdd/SKILL.md")
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("the first install has no files: %v", err)
	}

	// skills of the user's by both names the plugin would try
	write(t, filepath.Join(skillDir("mine"), "SKILL.md"), "---\nname: mine\ndescription: Mine\n---\n")
	write(t, filepath.Join(skillDir("acme-kit-mine"), "SKILL.md"), "---\nname: acme-kit-mine\ndescription: Mine\n---\n")
	if _, err := change(func(l *Library) error {
		l.Skills = append(l.Skills, &Skill{Name: "mine", Agents: []string{}}, &Skill{Name: "acme-kit-mine", Agents: []string{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// a version whose only part is the user's name: nothing can be put in
	v2 := filepath.Join(t.TempDir(), "acme2")
	write(t, filepath.Join(v2, ".claude-plugin/plugin.json"), `{"name":"acme-kit","version":"2"}`)
	write(t, filepath.Join(v2, "skills/mine/SKILL.md"), "---\nname: mine\ndescription: Theirs\n---\n")
	if _, err := InstallPlugins(v2, nil, nil); err == nil {
		t.Fatal("an upgrade with no part to install was taken")
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("the old plugin's files were taken: %v", err)
	}
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if l.plugin("acme-kit") == nil || l.skill("tdd") == nil {
		t.Error("the old plugin left the library")
	}
	if s := l.skill("mine"); s == nil || s.Bundle != "" {
		t.Errorf("the user's skill was replaced: %+v", s)
	}
}

// A server and a skill of one name are two items in two places: neither
// is renamed because the other has the name (#M37b).
func TestPluginSkillAndServerKeepOneName(t *testing.T) {
	h := sandbox(t)
	pluginAgents(t, h)
	src := filepath.Join(t.TempDir(), "acme")
	write(t, filepath.Join(src, ".claude-plugin/plugin.json"), `{"name":"acme-kit"}`)
	write(t, filepath.Join(src, "skills/one/SKILL.md"), "---\nname: one\ndescription: One\n---\n")
	write(t, filepath.Join(src, ".mcp.json"), `{"mcpServers":{"one":{"command":"/bin/true"}}}`)
	ok(t)(InstallPlugins(src, nil, nil))
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if l.skill("one") == nil || l.server("one") == nil {
		t.Errorf("a skill and a server named one: skill=%v server=%v", l.skill("one") != nil, l.server("one") != nil)
	}
	if l.skill("acme-kit-one") != nil || l.server("acme-kit-one") != nil {
		t.Error("one item was renamed for the other's name")
	}
}

// A save that fails puts the old version back: the backup is kept until
// library.json names the new one (#M37b).
func TestPluginSaveFailureKeepsTheOldVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder permissions don't keep library.json from being written")
	}
	h := sandbox(t)
	pluginAgents(t, h)
	src := filepath.Join(t.TempDir(), "acme")
	write(t, filepath.Join(src, ".claude-plugin/plugin.json"), `{"name":"acme-kit","version":"1"}`)
	write(t, filepath.Join(src, "skills/tdd/SKILL.md"), "---\nname: tdd\ndescription: First\n---\n")
	ok(t)(InstallPlugins(src, nil, nil))

	v2 := filepath.Join(t.TempDir(), "acme2")
	write(t, filepath.Join(v2, ".claude-plugin/plugin.json"), `{"name":"acme-kit","version":"2"}`)
	write(t, filepath.Join(v2, "skills/next/SKILL.md"), "---\nname: next\ndescription: Next\n---\n")
	write(t, filepath.Join(v2, "v2-marker"), "two")

	dir := settings.Dir()
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, before.Mode().Perm()&^0o222); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, before.Mode().Perm()) })
	if _, err := InstallPlugins(v2, nil, nil); err == nil {
		t.Fatal("the install was taken while library.json couldn't be written")
	}
	os.Chmod(dir, before.Mode().Perm())

	if _, err := os.Stat(filepath.Join(pluginDir("acme-kit"), "v2-marker")); !os.IsNotExist(err) {
		t.Error("the new version's files stayed without the library naming them")
	}
	if _, err := os.Stat(filepath.Join(pluginDir("acme-kit"), "skills/tdd/SKILL.md")); err != nil {
		t.Errorf("the old version was lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir("next"), "SKILL.md")); !os.IsNotExist(err) {
		t.Error("the new version's skill stayed after the failed save")
	}
	if l, err := load(); err != nil || l.plugin("acme-kit") == nil || l.plugin("acme-kit").Version != "1" {
		t.Errorf("the library names another version: %+v %v", l.plugin("acme-kit"), err)
	}
	if es, err := os.ReadDir(pluginsDir()); err == nil {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), ".new-") || strings.HasPrefix(e.Name(), ".swap-") {
				t.Errorf("a transaction folder was left: %s", e.Name())
			}
		}
	}
}

// A version installed into the saved library leaves no transaction folder
// behind (#M37b).
func TestPluginInstallLeavesNoTransactionFolder(t *testing.T) {
	h := sandbox(t)
	pluginAgents(t, h)
	src := filepath.Join(t.TempDir(), "acme")
	writePlugin(t, src)
	ok(t)(InstallPlugins(src, nil, nil))
	if es, err := os.ReadDir(pluginsDir()); err != nil {
		t.Fatal(err)
	} else {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), ".new-") || strings.HasPrefix(e.Name(), ".swap-") {
				t.Errorf("a transaction folder was left: %s", e.Name())
			}
		}
	}
}
