package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/skills"
)

func setupSkillsWidget(t *testing.T) (*skillsWidget, *skills.Registry) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "foo")
	mkSkill(t, root, "bar")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	return newSkillsWidget(reg), reg
}

func TestSkillsWidget_RendersRows(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	view := w.View()
	if !strings.Contains(view, "foo") || !strings.Contains(view, "bar") {
		t.Errorf("view missing skill names: %s", view)
	}
	if !strings.Contains(view, "Catalog budget") {
		t.Errorf("view missing catalog budget: %s", view)
	}
	if !strings.Contains(view, "Global auto-invocation") {
		t.Errorf("view missing global auto toggle: %s", view)
	}
}

func TestSkillsWidget_TogglesEnabled(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	// cursor on first row, press x -> enabled override set
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	sk := w.rows[w.cursor]
	ov := w.pendingOverrides[sk.Fingerprint]
	if ov.Enabled == nil || !*ov.Enabled {
		t.Errorf("first press should set Enabled=true, got %+v", ov)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	ov = w.pendingOverrides[sk.Fingerprint]
	if ov.Enabled == nil || *ov.Enabled {
		t.Errorf("second press should set Enabled=false, got %+v", ov)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	ov = w.pendingOverrides[sk.Fingerprint]
	if ov.Enabled != nil {
		t.Errorf("third press should clear override, got %+v", ov)
	}
}

func TestSkillsWidget_GlobalAutoToggle(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	if w.pendingAuto != nil {
		t.Errorf("expected default nil, got %v", w.pendingAuto)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if w.pendingAuto == nil || !*w.pendingAuto {
		t.Errorf("after 1st g: want true, got %v", w.pendingAuto)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if w.pendingAuto == nil || *w.pendingAuto {
		t.Errorf("after 2nd g: want false, got %v", w.pendingAuto)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if w.pendingAuto != nil {
		t.Errorf("after 3rd g: want nil, got %v", w.pendingAuto)
	}
}

func TestSkillsWidget_ApplyPersists(t *testing.T) {
	w, reg := setupSkillsWidget(t)
	// Toggle first row to enabled=false.
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	sk := w.rows[w.cursor]
	if err := w.Apply(); err != nil {
		t.Fatal(err)
	}
	// After Apply + reload, the skill should be disabled.
	got, _ := reg.Resolve(sk.Name)
	if got == nil {
		t.Fatal("skill disappeared")
	}
	if got.Enabled {
		t.Errorf("skill should be disabled after save")
	}
}

func TestSkillsWidget_NavigatesCursor(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	if len(w.rows) < 2 {
		t.Skip("need at least 2 rows")
	}
	start := w.cursor
	w.Update(tea.KeyMsg{Type: tea.KeyDown})
	if w.cursor != start+1 {
		t.Errorf("cursor %d, want %d", w.cursor, start+1)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyUp})
	if w.cursor != start {
		t.Errorf("cursor %d, want %d", w.cursor, start)
	}
}

func TestSkillsWidget_RootsEdit(t *testing.T) {
	w, reg := setupSkillsWidget(t)
	// Press r → enter edit mode pre-populated with current roots.
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !w.rootsEditing {
		t.Fatal("should enter roots edit mode")
	}
	if len(w.rootsBuf) == 0 {
		t.Errorf("buffer should be pre-populated, got %q", string(w.rootsBuf))
	}
	// Clear and type a new path.
	w.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range []rune("/tmp/a, /tmp/b") {
		w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if w.rootsEditing {
		t.Error("Enter should exit edit mode")
	}
	if len(w.pendingRoots) != 2 || w.pendingRoots[0] != "/tmp/a" || w.pendingRoots[1] != "/tmp/b" {
		t.Errorf("pendingRoots = %+v", w.pendingRoots)
	}
	// Save → overrides.SkillRoots written.
	if err := w.Apply(); err != nil {
		t.Fatal(err)
	}
	ov := reg.Overrides()
	if len(ov.SkillRoots) != 2 || ov.SkillRoots[0] != "/tmp/a" {
		t.Errorf("SkillRoots not persisted: %+v", ov.SkillRoots)
	}
}

func TestSkillsWidget_RootsEditEsc(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	w.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range []rune("should discard") {
		w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if w.rootsEditing {
		t.Error("Esc should exit edit mode")
	}
	if w.pendingRoots != nil {
		t.Errorf("Esc should not commit: %+v", w.pendingRoots)
	}
}

func TestSkillsWidget_TrustPanelNavigation(t *testing.T) {
	w, _ := setupSkillsWidget(t)
	w.pendingTrust["/a"] = true
	w.pendingTrust["/b"] = true
	w.pendingDeny["/c"] = true
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !w.trustPanel {
		t.Fatal("trust panel should open")
	}
	if !strings.Contains(w.View(), "/a") {
		t.Errorf("trust view missing /a: %s", w.View())
	}
	// demote first (trusted) row
	w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if len(w.pendingTrust) != 1 {
		t.Errorf("demote should remove one trusted: %v", w.pendingTrust)
	}
	w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if w.trustPanel {
		t.Error("esc should close trust panel")
	}
}
