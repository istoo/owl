package dirent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A probe folder an owl made carries its process's pid, so a start sweeps
// the ones whose owl is gone and keeps a running owl's; a name an older
// owl made, with no pid in it, is swept only when it is old (#63).
func TestSweepWatchDirsKeepsARunningOwlsProbe(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	deadPID := 1 << 30 // past every system's pid_max
	dead := filepath.Join(tmp, "owl-watch-p"+strconv.Itoa(deadPID)+"-123")
	live := filepath.Join(tmp, "owl-watch-p"+strconv.Itoa(os.Getpid())+"-456")
	oldLegacy := filepath.Join(tmp, "owl-watch-789")
	freshLegacy := filepath.Join(tmp, "owl-watch-790")
	for _, d := range []string{dead, live, oldLegacy, freshLegacy} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * watchStale)
	if err := os.Chtimes(oldLegacy, old, old); err != nil {
		t.Fatal(err)
	}
	sweepWatchDirs()
	for _, c := range []struct {
		path string
		want bool
	}{
		{dead, false},
		{live, true},
		{oldLegacy, false},
		{freshLegacy, true},
	} {
		_, err := os.Stat(c.path)
		if got := err == nil; got != c.want {
			t.Errorf("%s kept=%v, want %v", filepath.Base(c.path), got, c.want)
		}
	}
	t.Cleanup(func() {
		os.RemoveAll(dead)
		os.RemoveAll(live)
		os.RemoveAll(oldLegacy)
		os.RemoveAll(freshLegacy)
	})
}

// Only a name made with a pid is read as one: an older owl's random suffix
// is no process to keep a folder for.
func TestWatchPIDNamesOnlyPidNames(t *testing.T) {
	if pid, ok := watchPID("owl-watch-p1234-567"); !ok || pid != 1234 {
		t.Errorf("owl-watch-p1234-567: %d, %v, want 1234, true", pid, ok)
	}
	for _, name := range []string{"owl-watch-1234", "owl-watch-p-1", "owl-watch-px-1", "owl-watch-p0-1", "other"} {
		if pid, ok := watchPID(name); ok {
			t.Errorf("%q was read as pid %d", name, pid)
		}
	}
}
