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
	writeZipLinkThen(t, path, link, target, nil)
}

// writeZipLinkThen is writeZipLink with files written after the link.
func writeZipLinkThen(t *testing.T, path, link, target string, after []string) {
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
	for _, a := range after {
		w, err = z.Create(a)
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

// A .app's internal relative symlinks (Versions/Current -> A) are kept;
// one that resolves outside the download, or is absolute, is refused. A
// path written under a link is ambiguous and refused even when the link
// itself stays inside (#C3).
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
	bad := []struct{ link, target string }{
		{"owl.app/Contents/link", "/tmp/evil"},
		{"owl.app/Contents/link", "../../../../etc"},
		{"owl.app/Contents/link", `..\..\..\..\evil`},
		{"owl.app/Contents/link", ""},
	}
	for i, b := range bad {
		p := filepath.Join(dir, "bad"+string(rune('a'+i))+".zip")
		writeZipLink(t, p, b.link, b.target)
		if err := zipSafe(p); err == nil {
			t.Errorf("link %q -> %q was accepted", b.link, b.target)
		}
	}
	// an inside link with a file written through it: which path lands
	// depends on the extractor's order, so it is refused
	p := filepath.Join(dir, "through.zip")
	writeZipLinkThen(t, p, "owl.app/Contents/link", "A", []string{"owl.app/Contents/link/after"})
	if err := zipSafe(p); err == nil {
		t.Error("a file written through an inside link was accepted")
	}
}
