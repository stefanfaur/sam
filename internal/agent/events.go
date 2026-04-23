package agent

import (
	"encoding/json"
)

// DecisionKind represents the type of approval decision
type DecisionKind string

const (
	DecisionAllow        DecisionKind = "allow"
	DecisionAllowSession DecisionKind = "allow_session"
	DecisionDeny         DecisionKind = "deny"
)

type Event interface{ isEvent() }

type TextDelta struct {
	Text string
}

func (TextDelta) isEvent() {}

type ThinkingDelta struct {
	Text string
}

func (ThinkingDelta) isEvent() {}

type MessageStart struct{}

func (MessageStart) isEvent() {}

type MessageEnd struct {
	StopReason string
}

func (MessageEnd) isEvent() {}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolCall) isEvent() {}

type ToolResult struct {
	ID        string
	Name      string
	Output    string
	IsError   bool
	Rewritten string
}

func (ToolResult) isEvent() {}

type ApprovalRequest struct {
	ID    string
	Tool  string
	Input json.RawMessage
	reply chan ApprovalDecision
}

func (ApprovalRequest) isEvent() {}

// Respond sends the approval decision back to the agent
func (r ApprovalRequest) Respond(decision ApprovalDecision) {
	r.reply <- decision
}

type ApprovalDecision struct {
	Kind   DecisionKind
	Reason string
}

func (ApprovalDecision) isEvent() {}

type TurnDone struct {
	StopReason string
}

func (TurnDone) isEvent() {}

type ErrorEvent struct {
	Err error
}

func (ErrorEvent) isEvent() {}

type UsageEvent struct {
	InputTokens        int
	OutputTokens       int
	CacheReadInput     int
	CacheCreationInput int
}

func (UsageEvent) isEvent() {}

// SkillInvoked is emitted at the start of SubmitSkill so the TUI can attach
// its collapsed-render annotation before the normal turn events arrive. Body
// is the rendered skill body (post-$ARGUMENTS substitution) that the provider
// will see as the user turn — the TUI keeps it so it can be expanded on demand.
type SkillInvoked struct {
	Fingerprint string
	Header      string
	Source      string
	Body        string
}

func (SkillInvoked) isEvent() {}
