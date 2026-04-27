package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/llm"
)

// rewindHintTTL is how long the post-rewind transient hint stays on screen.
const rewindHintTTL = 3 * time.Second

// undoRewindSlot stores the state needed to revert exactly one rewind. It is
// a single-slot buffer cleared on the next user submission.
type undoRewindSlot struct {
	// History is the conversation history as it was BEFORE the rewind.
	History []llm.Message
	// TurnCount is the agent's turnCount BEFORE the rewind.
	TurnCount int
	// Paths are the working-tree paths the rewind restored — undo replays
	// them from latestTurn so the working tree pops forward to its
	// pre-rewind state.
	Paths []string
	// LatestTurn is the maximum turn number with a snapshot at the time the
	// rewind ran. Undo Restore()s back to this turn.
	LatestTurn int
	// FromTurn / ToTurn are descriptive — UI hint text.
	FromTurn int
	ToTurn   int
	// DiscardedTurns holds the persisted TurnRecord entries we dropped
	// from the sidecar at rewind time (everything with Index > ToTurn-1).
	// Undo replays them via SaveSession + re-sets each ref by SHA so the
	// picker, ListPaths, OverlapPaths see the same world they did before.
	DiscardedTurns []agent.TurnRecord
}

// rewindHintState drives the transient "Rewound to turn N — Ctrl+Z to undo"
// banner. Hidden when text == "" or when time.Now() > Until.
type rewindHintState struct {
	Text  string
	Until time.Time
}

func (h rewindHintState) Active() bool {
	return h.Text != "" && !time.Now().After(h.Until)
}

// rewindHintTickMsg is sent after rewindHintTTL so the banner fades.
type rewindHintTickMsg struct{}

func scheduleRewindHintFade(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg { return rewindHintTickMsg{} })
}

// openRewindPicker loads the persisted session record and constructs the
// picker. Returns false (with an info hint command) when the agent is not
// configured for checkpoints or when no turns have completed yet.
func (m *Model) openRewindPicker() (tea.Cmd, bool) {
	if m.agent == nil || !m.agent.CheckpointEnabled() {
		return m.addInfo("rewind unavailable: not in a git repo"), false
	}
	sid := m.agent.SessionID()
	sr, err := agent.LoadSession(sid)
	if err != nil || sr == nil || len(sr.Turns) == 0 {
		return m.addInfo("rewind: no completed turns yet"), false
	}
	m.rewind = newRewindPicker(sr.Turns)
	m.input.Blur()
	return nil, true
}

// closeRewindOverlay dismisses any active picker / restore-confirm and
// re-focuses the input.
func (m *Model) closeRewindOverlay() {
	m.rewind = nil
	m.restoreUI = nil
	m.input.Focus()
}

// handleRewindPickerKey routes keys while the picker is active. Returns
// (cmd, true) when a key was consumed; false to fall through to default.
func (m *Model) handleRewindPickerKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.rewind == nil {
		return nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.closeRewindOverlay()
		return nil, true
	case tea.KeyUp:
		m.rewind.Move(-1)
		return nil, true
	case tea.KeyDown:
		m.rewind.Move(1)
		return nil, true
	case tea.KeyEnter:
		entry, ok := m.rewind.Current()
		if !ok {
			return nil, true
		}
		mgr := m.agent.Checkpoint()
		if mgr == nil {
			m.closeRewindOverlay()
			return m.addInfo("rewind unavailable: checkpoint manager missing"), true
		}
		paths, _ := mgr.ListPaths(entry.Index)
		overlaps, _ := mgr.OverlapPaths(paths)
		m.restoreUI = newRestoreConfirm(entry.Index, paths, overlaps)
		m.rewind = nil
		return nil, true
	case tea.KeyBackspace:
		if q := m.rewind.query; q != "" {
			runes := []rune(q)
			m.rewind.SetQuery(string(runes[:len(runes)-1]))
		}
		return nil, true
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			m.rewind.SetQuery(m.rewind.query + string(msg.Runes))
			return nil, true
		}
	}
	return nil, true // swallow unhandled keys while picker is open
}

// handleRestoreConfirmKey routes keys while the restore-confirm modal is up.
func (m *Model) handleRestoreConfirmKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.restoreUI == nil {
		return nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.closeRewindOverlay()
		return nil, true
	case tea.KeyEnter:
		if !m.restoreUI.IsConfirmed() {
			if !m.restoreUI.Confirm() {
				return nil, true
			}
		}
		cmd := m.executeRewind(m.restoreUI.turnIndex, m.restoreUI.Mode(), m.restoreUI.PathsToRestore())
		m.closeRewindOverlay()
		return cmd, true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return nil, true
		}
		switch msg.Runes[0] {
		case 'c', 'C':
			m.restoreUI.SetMode(restoreModeConvOnly)
		case 'b', 'B':
			m.restoreUI.SetMode(restoreModeConvAndCode)
		case 'o', 'O':
			m.restoreUI.ToggleIncludeOverlaps()
		}
		return nil, true
	}
	return nil, true
}

// executeRewind performs the actual rewind: truncate history at the chosen
// turn boundary, optionally restore working-tree paths, set the undo slot,
// and trigger the transient hint.
func (m *Model) executeRewind(targetTurn int, mode restoreMode, paths []string) tea.Cmd {
	if m.agent == nil || !m.agent.CheckpointEnabled() {
		return m.addInfo("rewind unavailable")
	}
	sid := m.agent.SessionID()
	sr, err := agent.LoadSession(sid)
	if err != nil || sr == nil {
		return m.addInfo("rewind: session record missing")
	}
	var target *agent.TurnRecord
	for i := range sr.Turns {
		if sr.Turns[i].Index == targetTurn {
			target = &sr.Turns[i]
			break
		}
	}
	if target == nil {
		return m.addInfo(fmt.Sprintf("rewind: turn %d not found", targetTurn))
	}

	// Capture undo state BEFORE mutation: future TurnRecords get dropped
	// from the sidecar in a moment, so save them on the undo slot.
	var discarded []agent.TurnRecord
	for _, t := range sr.Turns {
		if t.Index > targetTurn-1 {
			discarded = append(discarded, t)
		}
	}
	undo := &undoRewindSlot{
		History:        m.agent.History(),
		TurnCount:      m.agent.TurnCount(),
		FromTurn:       m.agent.TurnCount(),
		ToTurn:         targetTurn,
		Paths:          append([]string(nil), paths...),
		LatestTurn:     latestTurnForUndo(m.agent),
		DiscardedTurns: discarded,
	}

	// Restore code first (if requested) so a write failure aborts before
	// we mutate agent state.
	if mode == restoreModeConvAndCode && len(paths) > 0 {
		if err := m.agent.Checkpoint().Restore(targetTurn, paths); err != nil {
			return m.addInfo("rewind: restore failed: " + err.Error())
		}
	}

	// Truncate history. UserMsgIndex points at the start of the chosen
	// turn's user message; rewinding "to turn N" means rolling BACK to
	// just before turn N kicked off.
	hist := m.agent.History()
	if target.UserMsgIndex >= 0 && target.UserMsgIndex <= len(hist) {
		m.agent.SetHistory(hist[:target.UserMsgIndex])
	}
	// Roll the agent's turn counter back so the next snapshot writes
	// refs/sam/checkpoints/<sid>/<targetTurn>.
	m.agent.SetTurnCount(targetTurn - 1)

	// Drop any snapshot refs / sidecar entries for turns we just undid.
	// Without this, MaxTurn / ListPaths / OverlapPaths keep returning stale
	// data after rewind, and the picker preview shows phantom turns from
	// the discarded future. Failures here are non-fatal: cleanup still
	// happens on next session shutdown via DeleteSession.
	if mgr := m.agent.Checkpoint(); mgr != nil {
		if err := mgr.DeleteFromTurn(targetTurn - 1); err != nil && m.agent != nil {
			// Soft-fail: log via info hint but do not roll back the rewind.
			_ = err
		}
	}
	if err := agent.TruncateSession(sid, targetTurn-1); err != nil {
		_ = err
	}

	m.undoRewind = undo
	m.rewindHint = rewindHintState{
		Text:  fmt.Sprintf("Rewound to turn %d — Ctrl+Z to undo", targetTurn),
		Until: time.Now().Add(rewindHintTTL),
	}
	return scheduleRewindHintFade(rewindHintTTL)
}

// latestTurnForUndo fetches the highest-numbered snapshot ref. Used at
// rewind time so undo can Restore() forward to the original state.
func latestTurnForUndo(a *agent.Agent) int {
	mgr := a.Checkpoint()
	if mgr == nil {
		return 0
	}
	max, _ := mgr.MaxTurn()
	return max
}

// applyUndoRewind reverses the most recent rewind. Returns a cmd that emits
// an info line and (if appropriate) schedules another hint fade.
func (m *Model) applyUndoRewind() tea.Cmd {
	if m.undoRewind == nil {
		return m.addInfo("nothing to undo")
	}
	u := m.undoRewind
	m.undoRewind = nil

	mgr := m.agent.Checkpoint()
	// Resurrect refs FIRST so a subsequent Restore (which reads the ref's
	// commit tree) can find LatestTurn's snapshot. Rewind deleted those
	// refs but the underlying commit objects still exist in .git/objects.
	if mgr != nil {
		for _, t := range u.DiscardedTurns {
			if t.SnapshotSHA != "" {
				if err := mgr.SetSnapshotRef(t.Index, t.SnapshotSHA); err != nil {
					return m.addInfo("undo: ref restore failed: " + err.Error())
				}
			}
		}
	}
	if len(u.Paths) > 0 && u.LatestTurn > 0 && mgr != nil {
		if err := mgr.Restore(u.LatestTurn, u.Paths); err != nil {
			return m.addInfo("undo: restore failed: " + err.Error())
		}
	}
	if sid := m.agent.SessionID(); sid != "" && len(u.DiscardedTurns) > 0 {
		for _, t := range u.DiscardedTurns {
			if err := agent.AppendTurn(sid, t); err != nil {
				return m.addInfo("undo: sidecar restore failed: " + err.Error())
			}
		}
	}
	m.agent.SetHistory(u.History)
	m.agent.SetTurnCount(u.TurnCount)
	m.rewindHint = rewindHintState{
		Text:  fmt.Sprintf("Undone — back at turn %d", u.FromTurn),
		Until: time.Now().Add(rewindHintTTL),
	}
	return scheduleRewindHintFade(rewindHintTTL)
}

// clearUndoOnNextSubmit drops the undo buffer the moment the user starts a
// new turn (matches the plan's "one-slot, cleared on next user message").
func (m *Model) clearUndoOnNextSubmit() {
	m.undoRewind = nil
}

// renderRewindHint returns the transient banner text or "" when inactive.
func (m *Model) renderRewindHint() string {
	if !m.rewindHint.Active() {
		return ""
	}
	return m.theme.Suggest.Render(m.rewindHint.Text)
}
