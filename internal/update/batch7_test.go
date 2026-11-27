package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// githubRoute sends github.com's requests to a test server instead, so a
// test can serve GitHub's answers without its address.
type githubRoute struct {
	to   string
	next http.RoundTripper
}

func (r githubRoute) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.EqualFold(req.URL.Hostname(), "github.com") {
		to, err := url.Parse(r.to)
		if err != nil {
			return nil, err
		}
		u := *req.URL
		u.Scheme, u.Host = to.Scheme, to.Host
		out := req.Clone(req.Context())
		out.URL, out.Host = &u, ""
		return http.DefaultTransport.RoundTrip(out)
	}
	return r.next.RoundTrip(req)
}

// The manifest latest.json is not signed while signingOn is false: it is
// read from GitHub alone, never through an update mirror, whose answer is
// not checked by anything (#C2). A mirror that answers instead is not
// asked at all.
func TestGitHubLatestFileNeverComesThroughAMirror(t *testing.T) {
	updateHome(t)
	manifest, err := json.Marshal(Release{
		Version: "0.1.4000",
		Notes:   "genuine",
		Assets:  map[string]Asset{"owl-cli-linux-amd64": {URL: "https://github.com/istoo/owl/releases/download/v0.1.4000/owl-cli-linux-amd64", SHA256: strings.Repeat("ab", 32)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var githubHits, mirrorHits atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubHits.Add(1)
		w.Write(manifest)
	}))
	defer gh.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		w.Write([]byte(`{"version":"0.1.4000","notes":"from the mirror","assets":{}}`))
	}))
	defer mirror.Close()

	old := client
	client = &http.Client{Transport: githubRoute{to: gh.URL, next: http.DefaultTransport}}
	t.Cleanup(func() { client = old })

	ctx := WithMirror(context.Background(), mirror.URL)
	base := "https://github.com/istoo/owl/releases/download/v0.1.4000/"
	rel, err := githubLatestFile(ctx, base, "https://github.com/istoo/owl/releases/tag/v0.1.4000", "", map[string]ghFile{})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.1.4000" || rel.Notes != "genuine" {
		t.Fatalf("release %+v", rel)
	}
	if mirrorHits.Load() != 0 {
		t.Fatalf("the unsigned manifest came through the mirror %d times", mirrorHits.Load())
	}
	if githubHits.Load() == 0 {
		t.Fatal("the manifest was not read from GitHub")
	}
}

// Every unsigned thing that decides a version, a hash or a file's address
// (latest.json, the feed, GitHub's tag metadata, the API's asset address)
// is read from the official address alone: with the global mirrors on and
// with an update mirror given, a failed official read is an error, never a
// mirror's answer (#C2).
func TestUnsignedUpdateMetadataNeverComesFromAMirror(t *testing.T) {
	updateHome(t)
	t.Setenv("OWL_UPDATE_FEED", "")
	body := []byte("the new owl")
	manifest, err := json.Marshal(Release{
		Version: "0.1.4000",
		Notes:   "genuine",
		Assets:  map[string]Asset{BinaryAsset(): {URL: "https://github.com/istoo/owl/releases/download/v0.1.4000/" + BinaryAsset(), SHA256: strings.Repeat("ab", 32)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag := "v0.1.4000"
	var officialFail atomic.Bool
	var officialHits, mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialHits.Add(1)
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/"+feedAsset):
			if officialFail.Load() {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			w.Write(manifest)
		case strings.HasSuffix(p, "/repos/"+releaseRepo+"/releases/latest"):
			if officialFail.Load() {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			fmt.Fprintf(w, `{"tag_name":%q,"body":"notes","html_url":"https://github.com/%s/releases/tag/%s","assets":[{"name":%q,"size":%d,"url":%q}]}`,
				tag, releaseRepo, tag, BinaryAsset(), len(body), "https://github.com/repos/"+releaseRepo+"/releases/assets/1")
		case strings.HasSuffix(p, "/"+releaseRepo+"/releases/latest"):
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		w.Write([]byte(`{"version":"0.1.4000","notes":"from the mirror","assets":{}}`))
	}))
	defer mirror.Close()

	oldClient, oldWeb, oldAPI := client, githubWeb, githubAPI
	client = &http.Client{Transport: githubRoute{to: official.URL, next: http.DefaultTransport}}
	githubWeb, githubAPI = "https://github.com", "https://github.com"
	t.Cleanup(func() { client, githubWeb, githubAPI = oldClient, oldWeb, oldAPI })
	t.Setenv("OWL_MIRRORS", "on")
	t.Setenv("OWL_GITHUB_MIRROR", mirror.URL)

	base := "https://github.com/" + releaseRepo + "/releases/download/" + tag + "/"
	page := "https://github.com/" + releaseRepo + "/releases/tag/" + tag
	for _, c := range []struct {
		name string
		ctx  context.Context
	}{
		{"global mirrors only", context.Background()},
		{"with an update mirror", WithMirror(context.Background(), mirror.URL)},
	} {
		t.Run(c.name, func(t *testing.T) {
			// the official address fails: every read is an error and the
			// mirror is not asked at all
			officialFail.Store(true)
			mirrorHits.Store(0)
			if rel, err := githubLatestFile(c.ctx, base, page, "", map[string]ghFile{}); err == nil {
				t.Fatalf("latest.json came from a mirror: %+v", rel)
			}
			if got, _, _, _, err := githubTag(c.ctx); err == nil {
				t.Fatalf("GitHub's tag metadata came from a mirror: %q", got)
			}
			if rel, err := latestFrom(c.ctx, base+feedAsset, ""); err == nil {
				t.Fatalf("the feed came from a mirror: %+v", rel)
			}
			if n := mirrorHits.Load(); n != 0 {
				t.Fatalf("the mirror was asked %d times when the official read failed", n)
			}
			if officialHits.Load() == 0 {
				t.Fatal("the official address was never asked")
			}

			// and when it answers, the mirror is still not asked
			officialFail.Store(false)
			mirrorHits.Store(0)
			rel, err := githubLatestFile(c.ctx, base, page, "", map[string]ghFile{})
			if err != nil || rel.Version != "0.1.4000" || rel.Notes != "genuine" {
				t.Fatalf("latest.json: %+v %v", rel, err)
			}
			if got, _, _, _, err := githubTag(c.ctx); err != nil || got != tag {
				t.Fatalf("tag = %q, %v", got, err)
			}
			if rel, err := latestFrom(c.ctx, base+feedAsset, ""); err != nil || rel.Version != "0.1.4000" {
				t.Fatalf("feed: %+v %v", rel, err)
			}
			if n := mirrorHits.Load(); n != 0 {
				t.Fatalf("the mirror was asked %d times with the official answer in hand", n)
			}
		})
	}
}

// openCommand goes into /bin/sh: the bundle is single-quoted, so a path
// with $(...), backticks or $VAR is not expanded (#C4).
func TestOpenCommandQuotesTheBundleForTheShell(t *testing.T) {
	bundle := `/Applications/owl $(touch pwned) ` + "`id`" + ` $HOME.app`
	for _, window := range []bool{true, false} {
		got := openCommand(bundle, window, false)
		if !strings.Contains(got, shellQuote(bundle)) {
			t.Errorf("window=%v: %q is not shell-quoted in %q", window, bundle, got)
		}
		if strings.Contains(got, `"`) {
			t.Errorf("window=%v: %q uses double quotes", window, got)
		}
	}
}

// The elevated install copies the download beside exe first, so the
// replacement is one rename on exe's own filesystem; a failure leaves the
// old binary and no temporary file (#H49). Its temporary name is unique
// (mktemp), and the file it renames in keeps exe's own mode and owner, not
// the download's.
func TestInstallBinaryAsAdminScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script runs under /bin/sh")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "owl")
	staged := filepath.Join(dir, "downloads", "owl.new")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := installBinaryAsAdminScript(staged, exe)
	if strings.Contains(script, shellQuote(exe+".owl-new")) {
		t.Errorf("the script uses a fixed temporary name: %s", script)
	}
	if !strings.Contains(script, ".owl-new.XXXXXX") || !strings.Contains(script, "mktemp") {
		t.Errorf("the script does not make a unique temporary file: %s", script)
	}
	if b, err := exec.Command("/bin/sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("exe = %q", b)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o750 {
		t.Fatalf("exe's mode is %v, want the installed file's 0750", fi.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "owl.owl-new.*")); len(left) > 0 {
		t.Fatalf("temporary files are left: %v", left)
	}

	// a copy that can't be made leaves the old binary and nothing beside
	saved, _ := os.ReadFile(exe)
	os.Remove(staged)
	if exec.Command("/bin/sh", "-c", script).Run() == nil {
		t.Error("the script succeeded without a download")
	}
	if b, _ := os.ReadFile(exe); string(b) != string(saved) {
		t.Error("a failed install changed exe")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "owl.owl-new.*")); len(left) > 0 {
		t.Fatalf("a failed install left %v", left)
	}

	// two installs at once get names of their own and neither corrupts exe
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, "downloads", "owl."+string(rune('a'+i)))
		if err := os.WriteFile(p, []byte("new-"+string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
		s := installBinaryAsAdminScript(p, exe)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- exec.Command("/bin/sh", "-c", s).Run()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent install failed: %v", err)
		}
	}
	if b, _ := os.ReadFile(exe); !strings.HasPrefix(string(b), "new-") {
		t.Fatalf("exe after concurrent installs = %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "owl.owl-new.*")); len(left) > 0 {
		t.Fatalf("concurrent installs left %v", left)
	}

	// the download is removed only once the swap went through
	oldRun := run
	defer func() { run = oldRun }()
	run = func(string, ...string) ([]byte, error) { return nil, nil }
	kept := filepath.Join(dir, "downloads", "ok")
	if err := os.WriteFile(kept, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallBinaryAsAdmin(kept, exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("a successful install left its download: %v", err)
	}
	failed := filepath.Join(dir, "downloads", "failed")
	if err := os.WriteFile(failed, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	run = func(string, ...string) ([]byte, error) { return nil, errors.New("canceled") }
	if err := InstallBinaryAsAdmin(failed, exe); err == nil {
		t.Fatal("a failed install reported success")
	}
	if _, err := os.Stat(failed); err != nil {
		t.Errorf("a failed install removed its download: %v", err)
	}
}

// The certificate a signed app carries is read: a system binary on a Mac
// is signed with one, and its SHA-256 is what a release with no team
// identifier (owl's own self-signed build) is matched by.
func TestCertificateHashOfASignedApp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign")
	}
	if got := certificateHash(context.Background(), "/bin/ls"); got == "" {
		t.Fatal("no certificate was read from a signed system binary")
	}
}

// sameSigner accepts the same team or the same certificate, and never two
// empty team identifiers: an ad-hoc app is taken only over another ad-hoc
// one, and a self-signed one is matched by its certificate, not by "" (#M41).
func TestSameSignerCheck(t *testing.T) {
	teamA := signerID{team: "A"}
	teamB := signerID{team: "B"}
	certA := signerID{cert: "aa"}
	certB := signerID{cert: "bb"}
	adhoc := signerID{adhoc: true}
	unreadable := signerID{}
	for _, c := range []struct {
		name     string
		want     signerID
		got      signerID
		accepted bool
	}{
		{"same team", teamA, teamA, true},
		{"other team", teamA, teamB, false},
		{"no team on the download", teamA, unreadable, false},
		{"same certificate", certA, certA, true},
		{"other certificate", certA, certB, false},
		{"certificate over no team", certA, unreadable, false},
		{"ad-hoc over ad-hoc", adhoc, adhoc, true},
		{"ad-hoc over a team", teamA, adhoc, false},
		{"ad-hoc over an unreadable signer", unreadable, adhoc, false},
		{"unreadable over ad-hoc", adhoc, unreadable, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := sameSignerCheck(c.want, c.got)
			if (err == nil) != c.accepted {
				t.Fatalf("sameSignerCheck(%v, %v) = %v, accepted %v", c.want, c.got, err, c.accepted)
			}
		})
	}
}
