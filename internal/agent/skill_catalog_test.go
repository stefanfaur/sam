package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/skills"
	"github.com/stefanfaur/sam/internal/tools"
)

func TestAgent_CatalogInSystemPromptWhenGateOn(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", "does alpha", "body\n")

	tru := true
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{AutoInvokeEnable: &tru},
		skills.TrustList{}, nil, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}

	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "ok"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		Skills: reg, System: "base prompt",
	})
	a.Start()
	defer a.Close()

	for ev := range a.Submit(context.Background(), "hi") {
		_ = ev
	}
	if len(prov.Calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	sys := prov.Calls[0].System
	if !strings.Contains(sys, "<available_skills>") {
		t.Errorf("system missing catalog: %q", sys)
	}
	if !strings.Contains(sys, "alpha: does alpha") {
		t.Errorf("system missing skill entry: %q", sys)
	}
	if !strings.HasPrefix(sys, "base prompt") {
		t.Errorf("system should begin with base prompt: %q", sys)
	}
}

func TestAgent_CatalogAbsentWhenGateOff(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "beta", "does beta", "body\n")

	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, nil, nil,
	)
	reg.Load()

	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		Skills: reg, System: "base prompt",
	})
	a.Start()
	defer a.Close()
	for ev := range a.Submit(context.Background(), "hi") {
		_ = ev
	}
	if strings.Contains(prov.Calls[0].System, "<available_skills>") {
		t.Errorf("catalog should be absent when gate off: %q", prov.Calls[0].System)
	}
}

func TestAgent_CatalogRebuildsAfterToggle(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "gamma", "does gamma", "body\n")

	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, nil, nil,
	)
	reg.Load()

	prov := fake.New(
		[]llm.StreamEvent{{Type: llm.EventMessageStart}, {Type: llm.EventMessageStop, StopReason: "end_turn"}},
		[]llm.StreamEvent{{Type: llm.EventMessageStart}, {Type: llm.EventMessageStop, StopReason: "end_turn"}},
	)
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		Skills: reg, System: "base",
	})
	a.Start()
	defer a.Close()

	for ev := range a.Submit(context.Background(), "q1") {
		_ = ev
	}
	if strings.Contains(prov.Calls[0].System, "<available_skills>") {
		t.Errorf("catalog unexpected before toggle: %q", prov.Calls[0].System)
	}
	// Flip gate on.
	tru := true
	reg.SetOverrides(skills.Overrides{AutoInvokeEnable: &tru})
	a.RebuildSkillCatalog()
	for ev := range a.Submit(context.Background(), "q2") {
		_ = ev
	}
	if !strings.Contains(prov.Calls[1].System, "<available_skills>") {
		t.Errorf("catalog should appear after toggle: %q", prov.Calls[1].System)
	}
}
