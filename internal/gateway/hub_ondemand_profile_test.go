package gateway

import "testing"

// An agent's profile id (claude#work) is the agent itself when the hub
// decides whether it searches its MCP tools on its own (#47).
func TestHubOnDemandSeesThroughProfile(t *testing.T) {
	if hubOnDemand("claude#work", hubOnDemandOver+1) {
		t.Fatal("claude#work was given tools on demand though Claude searches them itself")
	}
	// the profile is still told apart from the plain id for anything else
	if got := HubAgentOf("claude#work"); got != "claude#work" {
		t.Fatalf("HubAgentOf = %q", got)
	}
}
