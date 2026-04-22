package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stefanfaur/sam/internal/agent"
)

type Approval struct {
	form   *huh.Form
	req    agent.ApprovalRequest
	choice string
	done   bool
}

func newApproval(req agent.ApprovalRequest) *Approval {
	a := &Approval{req: req}
	title := fmt.Sprintf("Approve %s?", req.Tool)
	desc := toolInputPreview(req.Tool, req.Input)
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title(title).Description(desc),
			huh.NewSelect[string]().
				Key("choice").
				Options(
					huh.NewOption("Allow once", "once"),
					huh.NewOption("Allow for session", "session"),
					huh.NewOption("Deny", "deny"),
				).Value(&a.choice),
		),
	).WithShowHelp(true).WithShowErrors(false)
	return a
}

func (a *Approval) Init() tea.Cmd { return a.form.Init() }

func (a *Approval) Update(msg tea.Msg) tea.Cmd {
	f, cmd := a.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		a.form = ff
	}
	if a.form.State == huh.StateCompleted {
		a.done = true
	}
	return cmd
}

func (a *Approval) View() string { return a.form.View() }

func (a *Approval) decide() agent.ApprovalDecision {
	switch a.choice {
	case "once":
		return agent.ApprovalDecision{Kind: agent.DecisionAllow}
	case "session":
		return agent.ApprovalDecision{Kind: agent.DecisionAllowSession}
	default:
		return agent.ApprovalDecision{Kind: agent.DecisionDeny}
	}
}
