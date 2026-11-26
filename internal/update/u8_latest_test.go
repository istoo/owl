package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// githubWorld serves the newest release's API answer, its latest.json and
// nothing else (no SHA256SUMS, no SHA256SUMS.sig).
func githubWorld(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + releaseRepo + "/releases/latest":
			fmt.Fprintf(rw, `{"tag_name":"v0.2.30","body":"notes","html_url":"https://github.com/%s/releases/tag/v0.2.30","assets":[`+
				`{"name":"latest.json","size":80,"url":%q},{"name":"owl-darwin-arm64.zip","size":3,"url":%q}]}`,
				releaseRepo, srv.URL+"/assets/latest.json", srv.URL+"/assets/zip")
		case "/assets/latest.json", "/" + releaseRepo + "/releases/download/v0.2.30/latest.json":
			fmt.Fprint(rw, `{"version":"0.2.30","notes":"new","url":"https://github.com/istoo/owl/releases/tag/v0.2.30","assets":{"owl-darwin-arm64.zip":{"url":"https://site/x","size":3,"sha256":"abc"}}}`)
		default:
			http.NotFound(rw, r)
		}
	}))
	return srv
}

// owl's own releases carry latest.json, not a SHA256SUMS.sig: with signing
// off, the fallback reads that and finds the update instead of a 404 (#71).
func TestGitHubLatestReadsLatestJSONWhenNotSigning(t *testing.T) {
	t.Setenv("OWL_UPDATE_FEED", "")
	wasOn, wasWeb, wasAPI := signingOn, githubWeb, githubAPI
	signingOn = false
	srv := githubWorld(t)
	defer srv.Close()
	githubWeb, githubAPI = srv.URL, srv.URL
	t.Cleanup(func() { signingOn, githubWeb, githubAPI = wasOn, wasWeb, wasAPI })

	rel, err := githubLatest(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.2.30" || rel.Notes != "new" || rel.Signed != nil {
		t.Fatalf("release %+v", rel)
	}
	if _, ok := rel.Assets["owl-darwin-arm64.zip"]; !ok {
		t.Fatalf("assets %v", rel.Assets)
	}
}

// With signing on the SHA256SUMS.sig is still required: a release that is
// nothing but latest.json is refused.
func TestGitHubLatestStillNeedsTheSignatureWhenSigning(t *testing.T) {
	t.Setenv("OWL_UPDATE_FEED", "")
	wasOn, wasWeb, wasAPI := signingOn, githubWeb, githubAPI
	signingOn = true
	srv := githubWorld(t)
	defer srv.Close()
	githubWeb, githubAPI = srv.URL, srv.URL
	t.Cleanup(func() { signingOn, githubWeb, githubAPI = wasOn, wasWeb, wasAPI })

	if _, err := githubLatest(context.Background(), ""); err == nil {
		t.Fatal("a release with no SHA256SUMS.sig was taken")
	}
}
