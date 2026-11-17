package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// RemoteAgentModelsPath is where a remote owl tells the models its
// agents are on (KoiLx0 on X: several computers on one remote owl, each
// setting every agent's models again by hand, and again after each change
// there). It is answered as the Library is: to the computer itself and to
// a full-access key of a owl shared on the local network.
const RemoteAgentModelsPath = "/v1/owl/agent-models"

// RemoteAgentModelsWait is how long a remote owl has to tell its picks.
const RemoteAgentModelsWait = 8 * time.Second

const errRemoteNoAgentModels = "remote owl doesn't tell its agents' models; update owl on that computer"

// AgentModels is what a owl tells of its agents' models: by agent id,
// each model field's key and the catalog ref it is on there (group/smart,
// deepseek/deepseek-chat).
type AgentModels struct {
	Agents map[string]map[string]string `json:"agents"`
}

// RemotePicks is one Remote owl's AgentModels, under its provider id
// here: a ref there is Provider+"/"+ref in this owl's catalog.
type RemotePicks struct {
	Provider string
	Agents   map[string]map[string]string
}

// pickedOwls are the Remote owls whose agents' picks are followed:
// on, with an address and a key, and not this owl itself.
func pickedOwls() []Provider {
	var out []Provider
	for _, p := range storedFile().Providers {
		if strings.TrimSpace(p.Anthropic) == "" {
			continue
		}
		if p = normalize(p); !hasEndpoint(p) {
			continue
		}
		if p.IsRemoteOwl() && !p.Off && p.Anthropic != "" && p.Key != "" && !p.SelfLeftOut() {
			out = append(out, p)
		}
	}
	return out
}

// HasPickedOwl reports whether a Remote owl's picks could be asked:
// a caller with nothing to follow asks no one.
func HasPickedOwl() bool { return len(pickedOwls()) > 0 }

// RemoteAgentPicks asks every Remote owl, in the order they were added,
// the models its agents are on. One that can't tell (unreachable, older,
// a held key) is left out, its error in errs by provider id.
func RemoteAgentPicks(ctx context.Context) (out []RemotePicks, errs map[string]error) {
	for _, p := range pickedOwls() {
		b, err := remoteBodyAs(ctx, p, http.MethodGet, RemoteAgentModelsPath, RemoteAgentModelsWait, 1<<20, errRemoteNoAgentModels)
		var m AgentModels
		if err == nil {
			err = json.Unmarshal(b, &m)
		}
		if err != nil {
			if errs == nil {
				errs = map[string]error{}
			}
			errs[p.ID] = err
			continue
		}
		out = append(out, RemotePicks{Provider: p.ID, Agents: m.Agents})
	}
	return out, errs
}
