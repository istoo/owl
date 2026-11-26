package update

import (
	"testing"

	"github.com/istoo/owl/internal/testenv"
)

// The tests run in a home of their own, never the user's real files, and
// the attestation API and the site's bundles are an address nothing
// listens on: a test that signs a release must not reach GitHub or
// usemagpie.ai (attest_test.go and attest_site_test.go serve their own).
func TestMain(m *testing.M) {
	attestAPI = "http://127.0.0.1:1"
	attestSite = "http://127.0.0.1:1"
	// the same for GitHub's own addresses unless a test serves its own: a
	// failed feed falls back to githubLatest, which now reads a release's
	// latest.json and would find the real one (#71)
	githubWeb, githubAPI = "http://127.0.0.1:1", "http://127.0.0.1:1"
	testenv.Main(m)
}
