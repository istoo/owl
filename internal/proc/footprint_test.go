package proc

import (
	"runtime"
	"testing"
)

// The OS's count of this process is a real number where owl knows how to
// read it, and unknown elsewhere: never 0 with a kind, never a made-up
// value.
func TestCurrentFootprintIsThisProcess(t *testing.T) {
	fp, ok := CurrentFootprint()
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		if !ok {
			t.Fatalf("no footprint on %s", runtime.GOOS)
		}
		if fp.Bytes < 1<<20 {
			t.Errorf("footprint %d bytes, less than a running test binary", fp.Bytes)
		}
		if fp.Kind == "" {
			t.Error("a footprint with no kind")
		}
	default:
		if ok {
			t.Fatalf("a footprint on %s: %+v", runtime.GOOS, fp)
		}
	}
}
