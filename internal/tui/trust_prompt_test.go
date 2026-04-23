package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/skills"
)

func TestTrustPrompt_FiresWhenProjectPending(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "alpha")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "project", Source: skills.SourceProject}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	tp := newTrustPromptModal(reg)
	if tp == nil {
		t.Fatal("trust prompt should fire for untrusted project skills")
	}
	v := tp.View()
	if !strings.Contains(v, "Trust this project") {
		t.Errorf("view missing prompt: %s", v)
	}
}

func TestTrustPrompt_NoFireWhenTrusted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "beta")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "project", Source: skills.SourceProject}},
		skills.Overrides{}, skills.NewTrustList(map[string]bool{root: true}, nil), BuiltinNames, nil,
	)
	reg.Load()
	if tp := newTrustPromptModal(reg); tp != nil {
		t.Errorf("trust prompt should not fire when already trusted")
	}
}

func TestTrustPrompt_YesPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "gamma")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "project", Source: skills.SourceProject}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	tp := newTrustPromptModal(reg)
	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !tp.Done() {
		t.Fatal("expected done after 'y'")
	}
	// Apply with a minimal root model.
	rootModel := &Model{
		settings: DefaultSettings(),
		theme:    NewTheme(DefaultSettings().Theme),
	}
	if cmd := tp.Apply(rootModel); cmd == nil {
		t.Error("expected an info cmd")
	}
	// Skill should now be enabled.
	sk, _ := reg.Resolve("gamma")
	if sk == nil || !sk.Enabled {
		t.Errorf("skill should be enabled after trust, got %+v", sk)
	}
	// Overrides on disk should include the trust entry.
	ov, err := skills.LoadOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Trust[root] {
		t.Errorf("trust entry missing on disk: %+v", ov.Trust)
	}
}

func TestTrustPrompt_ViewExpandsAndCollapses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "delta")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "project", Source: skills.SourceProject}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	tp := newTrustPromptModal(reg)
	tp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if !strings.Contains(tp.View(), "delta") {
		t.Errorf("view should list skill names: %s", tp.View())
	}
	tp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(tp.View(), "Trust this project") {
		t.Errorf("esc should return to prompt: %s", tp.View())
	}
}
