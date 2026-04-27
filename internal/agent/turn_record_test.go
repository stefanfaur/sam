package agent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"
)

func TestSessionRecord_RoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	sid := "sess-rt-1"
	if _, err := LoadSession(sid); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected not-exist, got %v", err)
	}

	rec := TurnRecord{
		Index:        1,
		UserMsgIndex: 0,
		UserMsg:      "do the thing",
		Tools: []ToolCallRecord{
			{Name: "Edit", Input: json.RawMessage(`{"path":"x"}`), Output: "ok"},
			{Name: "Bash", Input: json.RawMessage(`{"cmd":"ls"}`), Output: "a\nb", IsError: false},
		},
		SnapshotSHA: "deadbeefcafef00d",
		StoppedBy:   "end_turn",
		Timestamp:   time.Now().UTC().Truncate(time.Second),
	}
	if err := AppendTurn(sid, rec); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := LoadSession(sid)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.SessionID != sid || got.Schema != TurnRecordSchemaVersion {
		t.Fatalf("metadata wrong: %+v", got)
	}
	if len(got.Turns) != 1 {
		t.Fatalf("turns = %d", len(got.Turns))
	}
	if got.Turns[0].SnapshotSHA != "deadbeefcafef00d" || got.Turns[0].UserMsg != "do the thing" {
		t.Fatalf("turn data lost: %+v", got.Turns[0])
	}
}

func TestSessionRecord_AppendConcurrent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sid := "sess-concurrent"

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := AppendTurn(sid, TurnRecord{Index: i, Timestamp: time.Now()}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	got, err := LoadSession(sid)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Turns) != 8 {
		t.Fatalf("turns = %d, want 8", len(got.Turns))
	}
}

func TestSessionRecord_DeleteAndList(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, id := range []string{"a", "b", "c"} {
		if err := AppendTurn(id, TurnRecord{Index: 1, Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListSessionIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("list = %v", got)
	}
	if err := DeleteSession("b"); err != nil {
		t.Fatal(err)
	}
	got, _ = ListSessionIDs()
	if len(got) != 2 {
		t.Fatalf("after delete: %v", got)
	}

	// Deleting missing is fine.
	if err := DeleteSession("nope"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestTruncateSession_KeepsThroughIndex(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sid := "trunc-1"
	for i := 1; i <= 4; i++ {
		if err := AppendTurn(sid, TurnRecord{Index: i, Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := TruncateSession(sid, 2); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSession(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Turns) != 2 || got.Turns[0].Index != 1 || got.Turns[1].Index != 2 {
		t.Fatalf("turns = %+v", got.Turns)
	}
	// Truncating a missing session is a no-op.
	if err := TruncateSession("does-not-exist", 5); err != nil {
		t.Fatal(err)
	}
}

func TestSessionMTime(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if !SessionMTime("missing").IsZero() {
		t.Fatal("missing should be zero")
	}
	if err := AppendTurn("present", TurnRecord{Index: 1, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if SessionMTime("present").IsZero() {
		t.Fatal("present should be non-zero")
	}
}
