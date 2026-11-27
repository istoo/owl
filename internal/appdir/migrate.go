package appdir

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

// This build keeps its files in owl's own folders, but a user coming from
// upstream magpie has theirs there: the first start copies them over, once,
// so the sign-ins, providers and settings are all there. The copy goes to a
// temporary folder that is renamed into place, so a start that is killed
// part way leaves nothing half-written to be trusted next time.

// The folder the upstream build kept its files in, which this one copies
// from. Spelled out here rather than derived from owl's name: it is
// history, fixed.
const legacyUpName = "magpie"

func legacyConfigs(home string) []string {
	return []string{filepath.Join(home, ".config", legacyUpName)}
}

func legacyCaches(home string) []string {
	return []string{filepath.Join(home, ".cache", legacyUpName)}
}

// migrateMu makes one migration at a time within this process: Config and
// Cache are asked for on many paths, and two first looks must not both
// copy.
var migrateMu sync.Mutex

// migrate copies src to dst once, when dst holds nothing yet. It reports
// what it did, "" when there was nothing to do.
func migrate(src, dst string) string {
	if src == "" || dst == "" || filepath.Clean(src) == filepath.Clean(dst) {
		return ""
	}
	migrateMu.Lock()
	defer migrateMu.Unlock()
	// dst is owl's own folder. A link there means owl's files live on
	// another disk (owl data move), and the target may be unmounted: it is
	// never removed or written over from an older build's folder (#H47).
	// linkOf looks at it itself (Lstat), so a broken link is still the
	// user's own.
	if _, linked := linkOf(dst); linked {
		return ""
	}
	if _, err := os.Stat(src); err != nil {
		return ""
	}
	if es, err := os.ReadDir(dst); err == nil && len(es) > 0 {
		return ""
	}
	tmp := dst + ".migrating"
	os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		os.RemoveAll(tmp)
		log.Printf("appdir: couldn't copy %s to %s: %v", src, dst, err)
		return ""
	}
	if err := os.Rename(tmp, dst); err != nil {
		// dst may have been made by this process meanwhile
		if es, rerr := os.ReadDir(dst); rerr == nil && len(es) > 0 {
			os.RemoveAll(tmp)
			return ""
		}
		os.RemoveAll(dst)
		if err := os.Rename(tmp, dst); err != nil {
			os.RemoveAll(tmp)
			log.Printf("appdir: couldn't move %s into place: %v", dst, err)
			return ""
		}
	}
	return src
}
