package agent

import "time"

// snapshotTurn is the graceful turn-completion hook: it asks the checkpoint
// manager to write refs/sam/checkpoints/<session>/<turnNum> against the
// current working tree, then persists a TurnRecord sidecar. Any failure is
// logged and swallowed — checkpoints are best-effort and must never break
// the live agent flow.
//
// turnCount is incremented even when the manager is nil so callers (the TUI
// picker, /unrewind) can reason about turn numbers consistently in non-git
// dirs (where rewind is unavailable but turn counting still happens).
func (a *Agent) snapshotTurn(userMsg string, userMsgIdx int, tools []ToolCallRecord, stoppedBy string) {
	a.mu.Lock()
	a.turnCount++
	turn := a.turnCount
	mgr := a.checkpoint
	sid := a.sessionID
	a.mu.Unlock()

	if mgr == nil || sid == "" {
		return
	}

	sha, err := mgr.Snapshot(turn)
	if err != nil {
		if a.log != nil {
			a.log.Warn("checkpoint: snapshot failed", "turn", turn, "err", err)
		}
		return
	}

	rec := TurnRecord{
		Index:        turn,
		UserMsgIndex: userMsgIdx,
		UserMsg:      userMsg,
		Tools:        tools,
		SnapshotSHA:  sha,
		StoppedBy:    stoppedBy,
		Timestamp:    time.Now().UTC(),
	}
	if err := AppendTurn(sid, rec); err != nil && a.log != nil {
		a.log.Warn("checkpoint: persist turn record", "turn", turn, "err", err)
	}
}
