package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stefanfaur/sam/internal/checkpoint"
)

// TurnRecordSchemaVersion bumps when on-disk format changes.
const TurnRecordSchemaVersion = 1

// ToolCallRecord captures a single tool invocation within a turn for durable
// history-picker preview rendering.
type ToolCallRecord struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input,omitempty"`
	Output  string          `json:"output,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// TurnRecord is the persisted, picker-rendered metadata for one agent turn.
type TurnRecord struct {
	Index        int              `json:"index"`
	UserMsgIndex int              `json:"user_msg_index"`
	UserMsg      string           `json:"user_msg,omitempty"`
	Tools        []ToolCallRecord `json:"tools,omitempty"`
	SnapshotSHA  string           `json:"snapshot_sha,omitempty"`
	StoppedBy    string           `json:"stopped_by,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
}

// SessionRecord is the JSON sidecar for a session.
type SessionRecord struct {
	Schema    int          `json:"schema"`
	SessionID string       `json:"session_id"`
	StartedAt time.Time    `json:"started_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Turns     []TurnRecord `json:"turns"`
}

// turnRecordMu serializes file writes per session id within the process.
var turnRecordMu sync.Map // map[string]*sync.Mutex

func sessionLock(id string) *sync.Mutex {
	v, _ := turnRecordMu.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// SessionRecordPath returns the canonical sidecar path for sessionID, anchored
// at $XDG_STATE_HOME/sam/sessions or ~/.local/state/sam/sessions.
func SessionRecordPath(sessionID string) string {
	dir := checkpoint.SessionsStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, sessionID+".json")
}

// LoadSession reads the sidecar for sessionID. Returns (nil, fs.ErrNotExist)
// when the session has no record yet.
func LoadSession(sessionID string) (*SessionRecord, error) {
	path := SessionRecordPath(sessionID)
	if path == "" {
		return nil, errors.New("turn_record: no state dir resolved")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sr SessionRecord
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, fmt.Errorf("turn_record: parse %s: %w", path, err)
	}
	return &sr, nil
}

// SaveSession writes sr atomically (tmp file + rename). Acquires the
// per-session lock — do not call while already holding it.
func SaveSession(sr *SessionRecord) error {
	if sr == nil || sr.SessionID == "" {
		return errors.New("turn_record: nil or unidentified session")
	}
	mu := sessionLock(sr.SessionID)
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(sr)
}

// saveLocked writes sr without taking the per-session lock.
func saveLocked(sr *SessionRecord) error {
	if sr.Schema == 0 {
		sr.Schema = TurnRecordSchemaVersion
	}
	sr.UpdatedAt = time.Now().UTC()

	path := SessionRecordPath(sr.SessionID)
	if path == "" {
		return errors.New("turn_record: no state dir resolved")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(sr); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// loadLocked reads the on-disk record without taking the lock.
func loadLocked(sessionID string) (*SessionRecord, error) {
	path := SessionRecordPath(sessionID)
	if path == "" {
		return nil, errors.New("turn_record: no state dir resolved")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sr SessionRecord
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, fmt.Errorf("turn_record: parse %s: %w", path, err)
	}
	return &sr, nil
}

// TruncateSession drops every persisted turn whose Index is strictly greater
// than keepThrough. Pairs with checkpoint.Manager.DeleteFromTurn at rewind
// time so the on-disk picker preview matches the live ref namespace.
func TruncateSession(sessionID string, keepThrough int) error {
	mu := sessionLock(sessionID)
	mu.Lock()
	defer mu.Unlock()
	sr, err := loadLocked(sessionID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	out := sr.Turns[:0]
	for _, t := range sr.Turns {
		if t.Index <= keepThrough {
			out = append(out, t)
		}
	}
	sr.Turns = out
	return saveLocked(sr)
}

// AppendTurn loads (or creates) the session record and adds rec, then saves.
// Concurrent calls for the same sessionID serialize through sessionLock.
func AppendTurn(sessionID string, rec TurnRecord) error {
	mu := sessionLock(sessionID)
	mu.Lock()
	defer mu.Unlock()
	sr, err := loadLocked(sessionID)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if sr == nil {
		sr = &SessionRecord{
			Schema:    TurnRecordSchemaVersion,
			SessionID: sessionID,
			StartedAt: time.Now().UTC(),
		}
	}
	sr.Turns = append(sr.Turns, rec)
	return saveLocked(sr)
}

// DeleteSession removes the sidecar for sessionID. Missing file is not an error.
func DeleteSession(sessionID string) error {
	path := SessionRecordPath(sessionID)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ListSessionIDs returns every session id that currently has a sidecar on disk.
// Used by orphan sweep.
func ListSessionIDs() ([]string, error) {
	dir := checkpoint.SessionsStateDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}

// SessionMTime returns modification time of the sidecar, or zero if missing.
func SessionMTime(sessionID string) time.Time {
	path := SessionRecordPath(sessionID)
	if path == "" {
		return time.Time{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}
