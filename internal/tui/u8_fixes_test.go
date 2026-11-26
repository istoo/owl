package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/istoo/owl/internal/agent"
)

// A machine with no agent of its own, or an agent with no field, is a
// page, not a division by zero: the keys that move the cursor leave it
// where it is instead of panicking (#78).
func TestKeysWithNoAgentOrNoField(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyRight}, {Type: tea.KeyLeft}, {Type: tea.KeyEnter},
	}
	empty := model{page: pageAgents, mode: modeList}
	for _, k := range keys {
		empty.updateList(k) // must not panic
	}

	noFields := model{
		page: pageAgents, mode: modeList,
		agents:   []*agent.Agent{{ID: "x", Name: "X"}},
		values:   []map[string]string{{}},
		failover: []string{""},
	}
	for _, k := range keys {
		noFields.updateList(k) // must not panic
	}
}
