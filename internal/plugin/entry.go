package plugin

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/istoo/owl/internal/proc"
)

// entryInside is rel joined to dir, if it stays inside dir: no "..", no
// absolute path, and no link out of it. It is the check a package.json's
// owl.middleware, owl.agent and owl.hooks entries get, so a plugin can't
// point owl at a file of another plugin's or the user's.
//
// Unlike page.go's inside, the entry may not exist yet (owl reports that
// when it loads it, rather than calling the package no plugin), so when
// the file itself can't be resolved the nearest existing parent is: a
// folder that is a link out of dir is refused even when the file under
// it is missing.
func entryInside(dir, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(dir, clean)
	realDir, err := proc.RealPath(dir)
	if err != nil {
		return "", false
	}
	real, err := proc.RealPath(full)
	if err == nil {
		return full, withinReal(realDir, real)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	// the file isn't there: the nearest parent that is must be inside dir
	for p := filepath.Dir(full); ; p = filepath.Dir(p) {
		if real, err := proc.RealPath(p); err == nil {
			return full, withinReal(realDir, real)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
		if parent := filepath.Dir(p); parent == p {
			return "", false
		}
	}
}

// withinReal says p is root or under it, both given as real paths.
func withinReal(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
