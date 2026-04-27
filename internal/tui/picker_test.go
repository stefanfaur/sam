package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/agent"
)

func sampleTurns() []agent.TurnRecord {
	return []agent.TurnRecord{
		{Index: 1, UserMsg: "add login button", Tools: []agent.ToolCallRecord{{Name: "Edit", Input: json.RawMessage(`{"path":"login.go"}`)}}, Timestamp: time.Now()},
		{Index: 2, UserMsg: "run tests", Tools: []agent.ToolCallRecord{{Name: "Bash", Input: json.RawMessage(`{"cmd":"go test"}`)}}, Timestamp: time.Now()},
		{Index: 3, UserMsg: "fix the failing test", Tools: []agent.ToolCallRecord{{Name: "Edit"}, {Name: "Edit"}}, Timestamp: time.Now()},
	}
}

func TestRewindPicker_DefaultSelection(t *testing.T) {
	p := newRewindPicker(sampleTurns())
	cur, ok := p.Current()
	if !ok {
		t.Fatal("expected current entry")
	}
	if cur.Index != 3 {
		t.Fatalf("expected newest turn (3) selected, got %d", cur.Index)
	}
}

func TestRewindPicker_FuzzyFilter(t *testing.T) {
	p := newRewindPicker(sampleTurns())
	p.SetQuery("login")
	if got := len(p.Filtered()); got != 1 {
		t.Fatalf("login filter: %d entries", got)
	}
	cur, _ := p.Current()
	if cur.Index != 1 {
		t.Fatalf("login filter selected wrong: %d", cur.Index)
	}
}

func TestRewindPicker_NavigationClamped(t *testing.T) {
	p := newRewindPicker(sampleTurns())
	p.Move(-100)
	if cur, _ := p.Current(); cur.Index != 1 {
		t.Fatalf("clamp top: %d", cur.Index)
	}
	p.Move(100)
	if cur, _ := p.Current(); cur.Index != 3 {
		t.Fatalf("clamp bottom: %d", cur.Index)
	}
}

func TestRewindPicker_BashWarningSurfaced(t *testing.T) {
	p := newRewindPicker(sampleTurns())
	p.SetQuery("run tests")
	cur, _ := p.Current()
	if !cur.HasBash {
		t.Fatal("Bash warning not detected")
	}
	out := p.Render(NewTheme(ThemeSettings{}), 80, 20)
	if !strings.Contains(out, "Bash") {
		t.Fatalf("preview missing bash warning:\n%s", out)
	}
}

func TestRewindPicker_RenderShowsAllRows(t *testing.T) {
	p := newRewindPicker(sampleTurns())
	out := p.Render(NewTheme(ThemeSettings{}), 100, 24)
	for _, want := range []string{"login", "run tests", "fix the failing test"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestRewindPicker_EmptyEntries(t *testing.T) {
	p := newRewindPicker(nil)
	if _, ok := p.Current(); ok {
		t.Fatal("empty picker reports current")
	}
	out := p.Render(NewTheme(ThemeSettings{}), 80, 20)
	if !strings.Contains(out, "Rewind") {
		t.Fatalf("empty render: %q", out)
	}
}

func TestOneLine_Truncates(t *testing.T) {
	got := oneLine("a\nb  c   d", 10)
	if got != "a b c d" {
		t.Fatalf("got %q", got)
	}
	got = oneLine("hello world this is long", 8)
	if got != "hello w…" {
		t.Fatalf("got %q", got)
	}
}
