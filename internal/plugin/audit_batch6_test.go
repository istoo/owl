package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A package's own name comes from its package.json, which a git repository
// writes: one with .. in it must not name a folder outside node_modules
// (L13).
func TestTargetStaysInNodeModules(t *testing.T) {
	storeSandbox(t)
	root := filepath.Join(Dir(), "node_modules")
	for _, spec := range []string{"abc/../../../x", "@scope/../../etc", "@scope/.."} {
		if IsPath(spec) {
			continue // a path is the user's own folder, not this
		}
		at := Target(spec)
		rel, err := filepath.Rel(root, at)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Errorf("Target(%q) = %q, outside node_modules", spec, at)
		}
	}
}

// A repository is owner/repo of GitHub's own letters: one with ? or # would
// put a query or a fragment into the URL built from it (L13).
func TestRepoPathIsGitHubsLetters(t *testing.T) {
	for spec, want := range map[string]string{
		"a/b": "a/b", "a/b/c": "a/b", "A-1_b.c/repo-2": "A-1_b.c/repo-2",
		"a/b?x=1": "", "a/b#tag": "", "a/../b": "", "a/..": "",
		"a": "", "/a/b": "a/b",
	} {
		if got := repoPath(spec); got != want {
			t.Errorf("repoPath(%q) = %q, want %q", spec, got, want)
		}
	}
	for spec, want := range map[string]string{
		"github:a/b": "a/b", "github:a/b?x": "", "https://github.com/a/b/tree/main": "a/b",
		"https://github.com/a/b?tab=readme": "", "gitlab:a/b": "",
	} {
		if got := githubRepo(spec); got != want {
			t.Errorf("githubRepo(%q) = %q, want %q", spec, got, want)
		}
	}
	if gh, _, _ := gitSource("github:a/b?x"); gh != "" {
		t.Errorf("gitSource kept %q", gh)
	}
}

// A tarball's files are bounded, one and together, so a gzip bomb can't be
// read into memory (M40b).
func TestTarFilesAreBounded(t *testing.T) {
	var total int64
	if err := checkTarSize(maxTarFile, &total); err != nil {
		t.Fatalf("a file at the limit: %v", err)
	}
	if err := checkTarSize(maxTarFile+1, &total); err == nil {
		t.Fatal("a file past the limit was taken")
	}
	total = 0
	if err := checkTarSize(maxTarFile/2+1, &total); err != nil {
		t.Fatalf("the first half: %v", err)
	}
	if err := checkTarSize(maxTarFile/2+1, &total); err == nil {
		t.Fatal("files past the total were taken")
	}
}

// The auth lock is the holder's: one that timed out must not take the next
// holder's lock away (M40c).
func TestLockAuthUnlocksOnlyItsOwn(t *testing.T) {
	storeSandbox(t)
	unlock := lockAuth()
	lock := AuthPath() + ".lock"
	t.Cleanup(func() { os.Remove(lock) })
	b, err := os.ReadFile(lock)
	if err != nil || !strings.Contains(string(b), `"token"`) {
		t.Fatalf("the lock says %q, %v: want its holder's pid and token", b, err)
	}
	// another holder has it now (this one timed out and the file was taken
	// and made again)
	if err := os.WriteFile(lock, []byte(`{"pid":1,"token":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("the unlock took another holder's lock away")
	}
}
