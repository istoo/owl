package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, n := range names {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// The bundle is unpacked before its signature is checked: an entry that
// leaves the folder is refused first (#4134).
func TestZipSafeRefusesEscapingEntry(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.zip")
	writeZip(t, good, "owl.app/", "owl.app/Contents/", "owl.app/Contents/Info.plist", "owl.app/Contents/MacOS/owl")
	if err := zipSafe(good); err != nil {
		t.Fatalf("a plain bundle: %v", err)
	}
	for i, name := range []string{
		"../evil", "/tmp/evil", "owl.app/../../evil", `..\evil`, `\evil`,
		"owl.app/..", "owl.app/Contents/..", "owl.app/Contents/../../evil",
	} {
		p := filepath.Join(dir, "bad"+string(rune('a'+i))+".zip")
		writeZip(t, p, name)
		if err := zipSafe(p); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
}

// writeZipLink writes a zip with a symlink entry named link, pointing at
// target.
func writeZipLink(t *testing.T, path, link, target string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	h := &zip.FileHeader{Name: link}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err := z.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(target))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// A .app's standard internal framework links are kept (#C3).
func TestZipSafeSymlinks(t *testing.T) {
	dir := t.TempDir()
	good := map[string]string{
		"owl.app/Contents/Frameworks/A.framework/Versions/Current": "A",
		"owl.app/Contents/Resources":                               "Frameworks/A.framework/Versions/Current/Resources",
		"owl.app/Contents/MacOS/owl-link":                          "../MacOS/owl",
	}
	i := 0
	for link, target := range good {
		p := filepath.Join(dir, "good"+string(rune('a'+i))+".zip")
		i++
		writeZipLink(t, p, link, target)
		if err := zipSafe(p); err != nil {
			t.Errorf("internal link %q -> %q: %v", link, target, err)
		}
	}
}

// zipNames folds each entry name as a Mac may, for resolveZipPath.
func zipNames(names ...string) map[string]string {
	out := map[string]string{}
	for _, n := range names {
		out[zipFold(n)] = n
	}
	return out
}

// A standard .app framework layout resolves through its links to the real
// files (#C3).
func TestResolveZipPathStandardFramework(t *testing.T) {
	links := map[string]string{
		"owl.app/Contents/Frameworks/A.framework/Versions/Current": "A",
		"owl.app/Contents/Resources":                               "Frameworks/A.framework/Versions/Current/Resources",
	}
	z := zipPaths{links: links, names: zipNames(
		"owl.app", "owl.app/Contents", "owl.app/Contents/Resources",
		"owl.app/Contents/Frameworks", "owl.app/Contents/Frameworks/A.framework",
		"owl.app/Contents/Frameworks/A.framework/Versions",
		"owl.app/Contents/Frameworks/A.framework/Versions/A",
		"owl.app/Contents/Frameworks/A.framework/Versions/A/Resources",
		"owl.app/Contents/Frameworks/A.framework/Versions/A/Resources/Info.plist",
	)}
	for path, want := range map[string]string{
		"owl.app/Contents/Frameworks/A.framework/Versions/Current/Resources/Info.plist": "owl.app/Contents/Frameworks/A.framework/Versions/A/Resources/Info.plist",
		"owl.app/Contents/Resources/Info.plist":                                         "owl.app/Contents/Frameworks/A.framework/Versions/A/Resources/Info.plist",
	} {
		got, err := resolveZipPath(path, z)
		if err != nil || got != want {
			t.Errorf("resolve(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

// A ".." after a link is applied inside the folder that link landed in,
// after its own links are expanded, not folded lexically beforehand (#C3).
func TestResolveZipPathAppliesDotsAfterTheLink(t *testing.T) {
	z := zipPaths{
		links: map[string]string{"a": "x", "x": "d/e"},
		names: zipNames("a", "x", "d", "d/e"),
	}
	got, err := resolveZipPath("a/../y", z)
	if err != nil || got != "d/y" {
		t.Fatalf("resolve(a/../y) = %q, %v; want d/y", got, err)
	}
}

// A path that reaches an entry under another case or normal form is
// ambiguous: which one is written cannot be told (#C3).
func TestResolveZipPathRefusesAnAlias(t *testing.T) {
	z := zipPaths{
		links: map[string]string{"owl.app/Link": "A"},
		names: zipNames("owl.app/Link", "owl.app/A"),
	}
	if got, err := resolveZipPath("owl.app/link/after", z); err == nil {
		t.Fatalf("the case alias resolved to %q", got)
	}
}

// An absolute link target is refused, and one that names nothing (#C3).
func TestCleanLinkTarget(t *testing.T) {
	for _, raw := range []string{"/tmp/x", `\\server\share`, ""} {
		if got, err := cleanLinkTarget([]byte(raw)); err == nil {
			t.Errorf("cleanLinkTarget(%q) = %q, accepted", raw, got)
		}
	}
	if got, err := cleanLinkTarget([]byte(`Versions/Current`)); err != nil || got != "Versions/Current" {
		t.Errorf("cleanLinkTarget = %q, %v", got, err)
	}
}
