package gateway

import (
	"github.com/istoo/owl/internal/provider"
	"github.com/tidwall/gjson"
)

// copilotHost is GitHub Copilot's API host. A key provider the user points
// there is the same vendor as the built-in account, and takes the same
// shaping: a custom provider is a key and an endpoint, not a new vendor.
const copilotHost = "api.githubcopilot.com"

// copilotServed says p's requests to to go to GitHub Copilot's API: its
// built-in account (whose base is the one its token exchange hands back),
// or a key provider pointed at the vendor's host.
func copilotServed(p provider.Provider, to provider.Protocol) bool {
	return p.Account != nil && p.Account.Agent == "copilot" || provider.HostOf(p.Base(to)) == copilotHost
}

// copilotGemini is whether body goes to a Gemini model served by Copilot.
func copilotGemini(p provider.Provider, to provider.Protocol, body []byte) bool {
	return copilotServed(p, to) && geminiNamed(gjson.GetBytes(body, "model").String())
}

// copilotGrokFlat is a Responses body to Copilot's Grok with Codex's
// namespaced functions flat and their parameters objects at the root: the
// backend behind Copilot turns a request with either away, and Copilot's
// word for it is a bare status (#4374), so nothing teaches owl to do it
// after a refusal. The calls come back under their namespaces
// (namespacedIn). Another vendor's tools go as they came.
func copilotGrokFlat(p provider.Provider, to provider.Protocol, body []byte) []byte {
	if to != provider.Responses || !copilotServed(p, to) ||
		!grokModel(gjson.GetBytes(body, "model").String()) {
		return body
	}
	return provider.GrokFlatBody(body)
}
