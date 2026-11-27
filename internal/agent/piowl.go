package agent

import (
	"encoding/json"
	"os"

	"github.com/istoo/owl/internal/edit"
	"github.com/istoo/owl/internal/lazyre"
)

// piOwlExt matches a Pi package source for the pi-owl extension:
// npm:pi-owl, npm:pi-owl@0.1.6, or a path or git source ending in it.
var piOwlExt = lazyre.New(`(^|[:/\\])pi-owl(@[^/\\]*)?(#.*)?$`)

// piOwlInstalled says Pi's settings.json lists the pi-owl extension
// in "packages". It serves provider "owl" from owl's live catalog,
// and warns at every start while models.json also names providers.owl:
// that block gives every model one baseUrl and a frozen list. A settings
// file that can't be read is unknown, so not installed.
func piOwlInstalled(settings string) bool {
	raw, err := os.ReadFile(settings)
	if err != nil {
		return false
	}
	var s struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if json.Unmarshal(edit.TrimBOM(raw), &s) != nil {
		return false
	}
	for _, p := range s.Packages {
		var src string
		if json.Unmarshal(p, &src) != nil {
			var o struct {
				Source string `json:"source"`
			}
			if json.Unmarshal(p, &o) != nil {
				continue
			}
			src = o.Source
		}
		if piOwlExt.MatchString(src) {
			return true
		}
	}
	return false
}

// dropPiBlock takes owl's own providers.owl out of models.json, the
// rest of the file as it was, after keeping a copy of the file beside it
// (models.json.before-pi-owl, the first one kept). off is wiringOff for
// the block: "" says it points at this gateway with owl's key, so it is
// owl's; any other block is the user's and stays.
func dropPiBlock(modelsPath string, off func() string) error {
	if _, has := edit.GetJSON(modelsPath, "providers."+owlID); !has || off() != "" {
		return nil
	}
	if raw, err := os.ReadFile(modelsPath); err == nil {
		if _, e := os.Stat(modelsPath + ".before-pi-owl"); e != nil {
			// the backup holds the key the block had: the file's own mode
			// when that is no wider than 0600, 0600 otherwise
			perm := os.FileMode(0o600)
			if st, serr := os.Stat(modelsPath); serr == nil {
				if p := st.Mode().Perm() & 0o600; p != 0 {
					perm = p
				}
			}
			if err := edit.WriteAtomicMode(modelsPath+".before-pi-owl", raw, perm); err != nil {
				return err
			}
		}
	}
	return edit.DelJSON(modelsPath, "providers."+owlID)
}
