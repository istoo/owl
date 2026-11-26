// Package jsmod compiles a plugin's JavaScript module for moejs, the
// JavaScript engine written in Go that owl runs plugin code in its own
// process with: gateway middleware (internal/middleware) and agents
// (internal/agentplug).
package jsmod

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MoeForge/moejs"
	"github.com/istoo/owl/internal/proc"
)

// Compile compiles the module in file and the modules it imports, which
// are files under root: there is no node_modules here, so a package is
// bundled into the module. what names the kind of module in an error
// ("a middleware").
//
// Every path is resolved with proc.RealPath, so a symlink or a Windows
// junction that leads out of root is refused too, not only a "..": the
// entry file and each relative import must really be inside root.
func Compile(root, file, what string) (*moejs.Module, error) {
	realRoot, err := proc.RealPath(root)
	if err != nil {
		return nil, err
	}
	realFile, err := proc.RealPath(file)
	if err != nil {
		return nil, err
	}
	if !insideReal(realRoot, realFile) {
		return nil, fmt.Errorf("%s is outside the plugin", file)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	entry, err := moejs.Compile(file, string(src))
	if err != nil {
		return nil, err
	}
	mods := map[string]*moejs.Module{file: entry}
	return moejs.Link(entry, func(ref moejs.Referrer, spec string) (*moejs.Module, error) {
		if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
			return nil, fmt.Errorf("%q: %s imports only files beside it; bundle packages into it", spec, what)
		}
		from := file
		if ref != nil {
			from = ref.Name()
		}
		p := filepath.Clean(filepath.Join(filepath.Dir(from), filepath.FromSlash(spec)))
		real, err := proc.RealPath(p)
		if err != nil {
			return nil, err
		}
		if !insideReal(realRoot, real) {
			return nil, fmt.Errorf("%q is outside the plugin", spec)
		}
		if m, ok := mods[p]; ok {
			return m, nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		m, err := moejs.Compile(p, string(src))
		if err != nil {
			return nil, err
		}
		mods[p] = m
		return m, nil
	})
}

// insideReal says p is root or under it, both given as real paths.
func insideReal(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
