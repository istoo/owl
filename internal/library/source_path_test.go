package library

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A link a user pasted may not name a folder outside the repository owl
// fetches: .., an absolute path or a backslash is refused (#H23, #M35b).
func TestSourcePathStaysInTheRepository(t *testing.T) {
	if _, ok := githubSource("https://github.com/o/r/tree/main/../../../etc"); ok {
		t.Error("githubSource took a path that climbs out")
	}
	if _, ok := githubSource("https://github.com/o/r/tree/main//etc/passwd"); ok {
		t.Error("githubSource took an absolute path")
	}
	if _, ok := githubSource("https://github.com/o/r/tree/main/x\\..\\y"); ok {
		t.Error("githubSource took a path with backslashes")
	}
	if src, ok := githubSource("https://github.com/o/r/tree/main/skills/pdf"); !ok || src.Path != "skills/pdf" {
		t.Errorf("githubSource refused a plain folder: %+v %v", src, ok)
	}
	if _, _, err := gitSource("https://gitlab.example.com/g/r/-/tree/main/../secret"); err == nil {
		t.Error("gitSource took a path that climbs out")
	}
	if _, _, err := gitSource("https://gitlab.example.com/g/r/-/tree/main/x\\..\\y"); err == nil {
		t.Error("gitSource took a path with backslashes")
	}
	if src, ok, err := gitSource("https://gitlab.example.com/g/r/-/tree/main/x/y"); !ok || err != nil || src.Path != "x/y" {
		t.Errorf("gitSource refused a plain folder: %+v %v %v", src, ok, err)
	}
}

// The probe and the updater guard a fetched folder with inside: one that
// climbs out, or a link that leads out, is never read (#H23).
func TestInsideRefusesAFolderOutsideTheDownload(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, ok := inside(root, "../x"); ok {
		t.Error("inside took a path that climbs out")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	if _, ok := inside(root, "escape"); ok {
		t.Error("inside took a link out of the download")
	}
	if _, ok := inside(root, "plain"); !ok {
		t.Error("inside refused a path in the download")
	}
}

// A tarball that claims a size larger than it has, or a negative one, is
// too big rather than added past int64's end (#M35a).
func TestUntarRefusesAnOverflowingSize(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "repo/x", Typeflag: tar.TypeReg, Size: maxFetch + 1, Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	if err := untar(bytes.NewReader(b.Bytes()), t.TempDir()); err != errTooBig {
		t.Fatalf("untar = %v, want errTooBig", err)
	}
	// a header that lies about its size: the bytes read are bounded
	var b2 bytes.Buffer
	gz2 := gzip.NewWriter(&b2)
	tw2 := tar.NewWriter(gz2)
	body := []byte("hello")
	if err := tw2.WriteHeader(&tar.Header{Name: "repo/y", Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	tw2.Write(body)
	tw2.Close()
	gz2.Close()
	dst := t.TempDir()
	if err := untar(bytes.NewReader(b2.Bytes()), dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "y")); got != "hello" {
		t.Errorf("unpacked %q", got)
	}
}

// A name that plus a long owner's name would be too long still gets a
// name: the loop takes a number alone instead of never ending (#H26).
func TestFreeSkillNameWithALongBase(t *testing.T) {
	sandbox(t)
	long := strings.Repeat("o", 70)
	repo := long + "/skills"
	owner, repoName := sourceParts(Source{Kind: "github", Repo: repo})
	l := &Library{}
	// every name made from the source's owner and repository is taken, so
	// the numeric loop is the one that answers
	for _, n := range []string{"pdf", owner, owner + "-" + repoName, repoName, "pdf-" + owner, "pdf-" + repoName} {
		if n != "" {
			l.Skills = append(l.Skills, &Skill{Name: n})
		}
	}
	done := make(chan string, 1)
	go func() {
		n, err := l.freeSkillName("pdf", Source{Kind: "github", Repo: repo})
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- n
	}()
	select {
	case got := <-done:
		if !nameRe.MatchString(got) || got != "pdf-2" {
			t.Errorf("freeSkillName = %q, want pdf-2", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("freeSkillName never returned: a name too long for its suffix loops")
	}
}
