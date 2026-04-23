package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRenderToolCard_Running(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		ID:        "1",
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test ./..."}`),
		StartedAt: time.Now(),
		Index:     3,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing name: %q", out)
	}
	if !strings.Contains(out, "running…") {
		t.Errorf("missing running slug: %q", out)
	}
	if strings.Contains(out, "/show-tool") {
		t.Errorf("hint should NOT appear while running: %q", out)
	}
	if !strings.ContainsAny(out, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("missing spinner glyph: %q", out)
	}
}

func TestRenderToolCard_DoneSuccess(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	start := time.Now().Add(-1200 * time.Millisecond)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test"}`),
		Output:    "PASS\nok  pkg/x  0.1s\n",
		Lines:     2,
		StartedAt: start,
		EndedAt:   time.Now(),
		Index:     5,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	// Settled success is a compact one-liner; Bash command appears in detail.
	for _, want := range []string{"●", "Bash", "2 lines", "[5]", "go test"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestRenderToolCard_DoneError(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test"}`),
		Output:    "FAIL: TestFoo\n--- FAIL",
		Lines:     2,
		IsError:   true,
		StartedAt: time.Now().Add(-400 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     9,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	for _, want := range []string{"✗", "Bash", "FAIL", "[9]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestRenderToolCard_Cancelled(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"sleep 9"}`),
		Cancelled: true,
		StartedAt: time.Now().Add(-500 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     11,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	for _, want := range []string{"◌", "Bash", "cancelled", "[11]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestToolSummary_PerTool(t *testing.T) {
	cases := []struct {
		tc   *toolCardState
		want string
	}{
		{&toolCardState{Name: "Bash", Lines: 47,
			StartedAt: time.Now().Add(-1200 * time.Millisecond),
			EndedAt:   time.Now()}, "47 lines"},
		{&toolCardState{Name: "Edit", EditLines: 10,
			Input:   json.RawMessage(`{"file_path":"foo/bar.go"}`),
			EndedAt: time.Now()}, "10 lines edited"},
		{&toolCardState{Name: "Write", Lines: 42,
			Input:   json.RawMessage(`{"file_path":"foo/bar.go"}`),
			EndedAt: time.Now()}, "42 lines written"},
		{&toolCardState{Name: "Task", Bytes: 1024,
			EndedAt: time.Now()}, "~256 tok"},
	}
	for _, c := range cases {
		got := toolSummary(c.tc, time.Now())
		if !strings.Contains(got, c.want) {
			t.Errorf("toolSummary(%s) = %q, want contains %q", c.tc.Name, got, c.want)
		}
	}

	running := &toolCardState{Name: "Bash"}
	if got := toolSummary(running, time.Now()); got != "running…" {
		t.Errorf("running summary = %q, want running…", got)
	}

	cancelled := &toolCardState{Name: "Bash", Cancelled: true,
		StartedAt: time.Now().Add(-300 * time.Millisecond),
		EndedAt:   time.Now()}
	if got := toolSummary(cancelled, time.Now()); !strings.Contains(got, "cancelled") {
		t.Errorf("cancelled summary = %q, want contains cancelled", got)
	}
}

func TestRenderToolCard_RewrittenSubline(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"git status"}`),
		Output:    "ok\n",
		Rewritten: "rtk git status",
		Lines:     1,
		StartedAt: time.Now().Add(-200 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     1,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	if !strings.Contains(out, "git status") {
		t.Errorf("expected original command on primary line: %q", out)
	}
	if !strings.Contains(out, "↳") {
		t.Errorf("expected ↳ marker for rewritten subline: %q", out)
	}
	if !strings.Contains(out, "rtk git status") {
		t.Errorf("expected rewritten command in subline: %q", out)
	}
}

func TestRenderToolCard_NoRewrittenSuppressesSubline(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"echo hi"}`),
		Output:    "hi\n",
		Lines:     1,
		StartedAt: time.Now().Add(-50 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     2,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	if strings.Contains(out, "↳") {
		t.Errorf("subline must not appear without rewritten: %q", out)
	}
}

func TestRenderToolCard_NarrowTerminalHintWraps(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name: "Bash", Lines: 2,
		Input:     json.RawMessage(`{"command":"go test ./internal/tui/..."}`),
		Output:    "PASS\n",
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Index:     99,
	}
	narrow := renderToolCard(th, tc, time.Now(), 0, 30)
	if !strings.Contains(narrow, "[99]") {
		t.Errorf("hint absent: %q", narrow)
	}
}
