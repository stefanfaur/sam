package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/skills"
	"github.com/stefanfaur/sam/internal/tools"
)

func writeSkill(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	sk := filepath.Join(dir, name)
	if err := os.MkdirAll(sk, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitSkill_RendersAndSubmits(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "review-pr", "Review a PR.", "Please review PR #$ARGUMENTS carefully.\n")

	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, nil, nil,
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
		Skills: reg,
	})
	a.Start()
	defer a.Close()

	events, err := a.SubmitSkill(context.Background(), "review-pr", "123")
	if err != nil {
		t.Fatalf("SubmitSkill: %v", err)
	}
	collected := []Event{}
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) == 0 {
		t.Fatal("no events")
	}
	inv, ok := collected[0].(SkillInvoked)
	if !ok {
		t.Fatalf("first event = %T, want SkillInvoked", collected[0])
	}
	if inv.Header != "/review-pr 123" {
		t.Errorf("header = %q", inv.Header)
	}
	if inv.Source != "personal" {
		t.Errorf("source = %q", inv.Source)
	}

	if len(a.history) == 0 {
		t.Fatal("history empty")
	}
	first := a.history[0]
	if first.Role != llm.RoleUser {
		t.Errorf("role = %v", first.Role)
	}
	if len(first.Content) != 1 || first.Content[0].Type != llm.ContentText {
		t.Fatalf("content = %+v", first.Content)
	}
	want := "Please review PR #123 carefully."
	if !strings.Contains(first.Content[0].Text, want) {
		t.Errorf("rendered body missing %q: %q", want, first.Content[0].Text)
	}
}

func TestSubmitSkill_UnknownReturnsError(t *testing.T) {
	reg := skills.NewRegistry(nil, skills.Overrides{}, skills.TrustList{}, nil, nil)
	reg.Load()
	a := New(Options{
		Provider: fake.New(), Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		Skills: reg,
	})
	a.Start()
	defer a.Close()

	if _, err := a.SubmitSkill(context.Background(), "does-not-exist", ""); err == nil {
		t.Error("expected error for unknown skill")
	}
}

func TestSubmitSkill_DisabledReturnsError(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "foo", "x", "body\n")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, nil, nil,
	)
	reg.Load()
	sk, _ := reg.Resolve("foo")
	fls := false
	reg.SetOverrides(skills.Overrides{
		Skills: map[string]skills.SkillOverride{
			string(sk.Fingerprint): {Enabled: &fls},
		},
	})

	a := New(Options{
		Provider: fake.New(), Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		Skills: reg,
	})
	a.Start()
	defer a.Close()

	if _, err := a.SubmitSkill(context.Background(), "foo", ""); err == nil {
		t.Error("expected disabled error")
	}
}
