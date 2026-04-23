package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stefanfaur/sam/internal/skills"
)

func newRegistryForTest(t *testing.T, root string) *skills.Registry {
	t.Helper()
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	return reg
}

func mkSkill(t *testing.T, root, name string) {
	t.Helper()
	d := filepath.Join(root, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test " + name + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseCommand_Builtins(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd Command
		wantArg string
	}{
		{"/help", CmdHelp, ""},
		{"/quit", CmdQuit, ""},
		{"/reload-skills", CmdReloadSkills, ""},
		{"/model gpt-5", CmdModel, "gpt-5"},
		{"not a command", CmdNone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			cmd, arg, sk := parseCommand(tc.in, nil)
			if cmd != tc.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tc.wantCmd)
			}
			if arg != tc.wantArg {
				t.Errorf("arg = %q, want %q", arg, tc.wantArg)
			}
			if sk != nil {
				t.Errorf("sk = %+v, want nil", sk)
			}
		})
	}
}

func TestParseCommand_Skill(t *testing.T) {
	root := t.TempDir()
	mkSkill(t, root, "review-pr")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	cmd, arg, sk := parseCommand("/review-pr 123", reg)
	if cmd != CmdSkill {
		t.Fatalf("cmd = %q", cmd)
	}
	if arg != "123" {
		t.Errorf("arg = %q", arg)
	}
	if sk == nil || sk.Name != "review-pr" {
		t.Fatalf("sk = %+v", sk)
	}
}

func TestParseCommand_UnknownNoSkill(t *testing.T) {
	reg := skills.NewRegistry(nil, skills.Overrides{}, skills.TrustList{}, nil, nil)
	reg.Load()
	cmd, arg, sk := parseCommand("/nope", reg)
	if cmd != CmdUnknown {
		t.Errorf("cmd = %q", cmd)
	}
	if arg != "nope" {
		t.Errorf("arg = %q", arg)
	}
	if sk != nil {
		t.Errorf("sk = %+v", sk)
	}
}

func TestParseCommand_BuiltinBeatsSkill(t *testing.T) {
	// A skill named 'clear' must not shadow the built-in.
	root := t.TempDir()
	mkSkill(t, root, "clear")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	cmd, _, _ := parseCommand("/clear", reg)
	if cmd != CmdClear {
		t.Errorf("built-in should win, got %q", cmd)
	}
	// But namespaced form resolves.
	cmd, _, sk := parseCommand("/personal:clear", reg)
	if cmd != CmdSkill || sk == nil {
		t.Errorf("namespaced skill did not resolve: cmd=%q sk=%+v", cmd, sk)
	}
}

func TestSuggestionsFor_IncludesEnabledSkills(t *testing.T) {
	root := t.TempDir()
	mkSkill(t, root, "run-tests")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	sugg := suggestionsFor(reg)
	found := false
	for _, s := range sugg {
		if s.Name == "/run-tests" {
			found = true
		}
	}
	if !found {
		t.Errorf("/run-tests missing from suggestions: %+v", sugg)
	}
}

func TestHelpTextFor_AppendsSkillsSection(t *testing.T) {
	root := t.TempDir()
	mkSkill(t, root, "foo")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	txt := helpTextFor(reg)
	if !containsAll(txt, []string{"Skills:", "/foo"}) {
		t.Errorf("help text missing skills section: %s", txt)
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strContains(s, sub) {
			return false
		}
	}
	return true
}

func strContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
