package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/agent"
)

type agentEventMsg struct{ Ev Event }
type submitMsg struct{ text string }
type turnClosedMsg struct{}
type quitMsg struct{}
type approvalDoneMsg struct {
	req      agent.ApprovalRequest
	decision agent.ApprovalDecision
}

// waitAgent bridges agent events to tea messages (one per round-trip).
func waitAgent(ch <-chan Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return turnClosedMsg{}
		}
		return agentEventMsg{Ev: ev}
	}
}
