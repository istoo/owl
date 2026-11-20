package provider

import (
	"slices"
	"strings"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/settings"
)

// What a deleted provider or group leaves in the lists that name it: a
// visibility (Visible), or models picked or taken out, that still names it
// keeps it out of an agent's lists after a provider is added again under
// its id, and owl visible then refuses that list's names (ark, neolix on
// the user's machine). So a deletion takes them out as it goes.

// forgetVisible takes every name matches out of every agent's visibility,
// and every ref matches out of the models picked or taken out. A
// visibility left with no name is gone: the agent is narrowed no more.
// An agent shown only its picks keeps the switch, its picks gone.
func forgetVisible(name, ref func(string) bool) error {
	s := settings.Load()
	changed := false
	for agent, names := range s.Visible {
		keep := slices.DeleteFunc(slices.Clone(names), name)
		if len(keep) == len(names) {
			continue
		}
		changed = true
		if len(keep) == 0 {
			delete(s.Visible, agent)
		} else {
			s.Visible[agent] = keep
		}
	}
	for agent, ids := range s.PickedModels {
		keep := slices.DeleteFunc(slices.Clone(ids), ref)
		if len(keep) == len(ids) {
			continue
		}
		changed = true
		s.PickedModels[agent] = keep
	}
	for agent, ids := range s.HiddenModels {
		keep := slices.DeleteFunc(slices.Clone(ids), ref)
		if len(keep) == len(ids) {
			continue
		}
		changed = true
		if len(keep) == 0 {
			delete(s.HiddenModels, agent)
		} else {
			s.HiddenModels[agent] = keep
		}
	}
	if !changed {
		return nil
	}
	if err := settings.Save(s); err != nil {
		return err
	}
	catalog.Touched()
	return nil
}

// forgetProvider takes a deleted provider's id out of every agent's
// visibility, and its models out of the models picked or taken out.
func forgetProvider(id string) error {
	return forgetVisible(
		func(n string) bool { return strings.EqualFold(n, id) },
		func(r string) bool { return strings.HasPrefix(r, id+"/") },
	)
}

// forgetGroup takes a deleted group's id out of every agent's visibility,
// and its refs, given as refs (group/<id>, or a found group's auto/<model>),
// out of the models picked or taken out.
func forgetGroup(id string, refs []string) error {
	name := func(n string) bool { return strings.EqualFold(n, id) || strings.EqualFold(n, GroupPrefix+id) }
	ref := func(r string) bool {
		if slices.ContainsFunc(refs, func(x string) bool { return strings.EqualFold(r, x) }) {
			return true
		}
		return strings.EqualFold(r, GroupPrefix+id)
	}
	return forgetVisible(name, ref)
}
