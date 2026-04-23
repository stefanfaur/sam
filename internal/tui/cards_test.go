package tui

import (
	"testing"
)

func TestModel_RecordToolBoundedRing(t *testing.T) {
	m := &Model{}
	for i := 0; i < maxRecentTools+5; i++ {
		m.recordTool(toolInvocation{Index: i + 1, Header: "Bash · done", IsError: false})
	}
	if len(m.recentTools) != maxRecentTools {
		t.Errorf("len = %d, want %d", len(m.recentTools), maxRecentTools)
	}
	if m.recentTools[0].Index != 6 {
		t.Errorf("front Index = %d, want 6", m.recentTools[0].Index)
	}
}

func TestModel_RecordThinkingBoundedRing(t *testing.T) {
	m := &Model{}
	for i := 0; i < maxRecentThinking+3; i++ {
		m.recordThinking(thinkingInvocation{Index: i + 1, Header: "thought for 1s"})
	}
	if len(m.recentThinking) != maxRecentThinking {
		t.Errorf("len = %d, want %d", len(m.recentThinking), maxRecentThinking)
	}
}

func TestModel_MonotonicToolIndex(t *testing.T) {
	m := &Model{}
	a := m.nextToolIdx()
	b := m.nextToolIdx()
	c := m.nextToolIdx()
	if a != 1 || b != 2 || c != 3 {
		t.Errorf("indices = %d,%d,%d; want 1,2,3", a, b, c)
	}
}

func TestModel_MonotonicThinkingIndex(t *testing.T) {
	m := &Model{}
	a := m.nextThinkingIdx()
	b := m.nextThinkingIdx()
	if a != 1 || b != 2 {
		t.Errorf("indices = %d,%d; want 1,2", a, b)
	}
}
