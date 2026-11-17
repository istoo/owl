package provider

import (
	"fmt"
	"strings"

	"github.com/istoo/owl/internal/settings"
)

// CodexCompactModel is the model Codex's compactions made by owl are
// put on, "" for the session's own. A provider renamed since it was set is
// named by its new id (RenamedRef).
func CodexCompactModel() string {
	id := strings.TrimSpace(heldSettings().CodexCompactModel)
	if id == "" {
		return ""
	}
	return RenamedRef(id)
}

// SetCodexCompactModel puts Codex's compactions on id, a model or routing
// group of owl's (provider/model, group/<id>); "" leaves each on the
// session's model. One owl doesn't serve now is kept all the same: the
// gateway leaves compactions where they are until it is served.
func SetCodexCompactModel(id string) error {
	id = strings.TrimSpace(id)
	if pid, model, ok := strings.Cut(id, "/"); id != "" && (!ok || pid == "" || model == "") {
		return fmt.Errorf("Codex's compaction can't be put on %q: pick one of owl's models (provider/model)", id)
	}
	s := settings.Load()
	if s.CodexCompactModel == id {
		return nil
	}
	s.CodexCompactModel = id
	if err := settings.Save(s); err != nil {
		return err
	}
	Changed()
	return nil
}
