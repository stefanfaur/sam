package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/agent"
)

func newQueueTestModel(t *testing.T) (*Model, *agent.Agent) {
	t.Helper()
	m, a, _, _ := newTestModelWithResolver(t, nil)
	return m, a
}

func TestEnterDuringPendingTurnQueues(t *testing.T) {
	m, a := newQueueTestModel(t)

	// Simulate a live turn so handleKey takes the queueing branch.
	m.pending = &pendingTurn{events: make(chan Event)}
	m.input.SetValue("add unit tests")

	if _, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}); false {
	}

	q := a.GetQueue()
	if len(q) != 1 || q[0] != "add unit tests" {
		t.Fatalf("expected queue [\"add unit tests\"], got %v", q)
	}
	if got := strings.TrimSpace(m.input.Value()); got != "" {
		t.Fatalf("expected input cleared after queueing, got %q", got)
	}
}

func TestEnterDuringPendingTurnIgnoresEmptyInput(t *testing.T) {
	m, a := newQueueTestModel(t)
	m.pending = &pendingTurn{events: make(chan Event)}
	m.input.SetValue("   ")

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("expected empty queue for whitespace-only input, got %d", got)
	}
}

func TestEscEscAbortDiscardsQueue(t *testing.T) {
	m, a := newQueueTestModel(t)
	m.pending = &pendingTurn{events: make(chan Event)}

	a.QueueSteer("first")
	a.QueueSteer("second")
	if got := len(a.GetQueue()); got != 2 {
		t.Fatalf("setup: expected queue length 2, got %d", got)
	}

	// First Esc records lastEscTime; second within window aborts + discards.
	m.lastEscTime = time.Now().Add(-50 * time.Millisecond)
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("expected queue discarded after Esc-Esc, got %d items", got)
	}
}

func TestSingleEscPreservesQueue(t *testing.T) {
	m, a := newQueueTestModel(t)
	m.pending = &pendingTurn{events: make(chan Event)}

	a.QueueSteer("preserve me")

	// No prior Esc → falls through to granular cancel branch which leaves
	// the queue intact.
	m.lastEscTime = time.Time{}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	q := a.GetQueue()
	if len(q) != 1 || q[0] != "preserve me" {
		t.Fatalf("expected queue preserved on granular Esc, got %v", q)
	}
}

func TestRenderQueueIndicator(t *testing.T) {
	th := NewTheme(ThemeSettings{})

	if got := renderQueueIndicator(th, nil, 80); got != "" {
		t.Fatalf("expected empty string for empty queue, got %q", got)
	}

	out := renderQueueIndicator(th, []string{"add tests"}, 80)
	if !strings.Contains(out, "queued (1)") {
		t.Fatalf("expected 'queued (1)' in output, got %q", out)
	}
	if !strings.Contains(out, "add tests") {
		t.Fatalf("expected 'add tests' in output, got %q", out)
	}

	out = renderQueueIndicator(th, []string{"a", "b", "c"}, 80)
	if !strings.Contains(out, "queued (3)") {
		t.Fatalf("expected 'queued (3)' in output, got %q", out)
	}
}
