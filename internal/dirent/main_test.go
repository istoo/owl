package dirent

import (
	"os"
	"testing"

	"github.com/istoo/owl/internal/testenv"
)

// The tests run in a home of their own, never the user's real files, and
// leave no probe folder behind.
func TestMain(m *testing.M) {
	code := testenv.Run(m)
	cleanProbe()
	os.Exit(code)
}
