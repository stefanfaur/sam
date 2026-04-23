package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, desc string) string {
	t.Helper()
	sk := filepath.Join(dir, name)
	if err := os.MkdirAll(sk, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return sk
}

func TestRegistry_PrecedenceProjectBeatsPersonal(t *testing.T) {
	projRoot := t.TempDir()
	persRoot := t.TempDir()
	writeSkill(t, projRoot, "review-pr", "project version")
	writeSkill(t, persRoot, "review-pr", "personal version")

	reg := NewRegistry(
		[]RootSpec{
			{Path: projRoot, Label: "project", Source: SourceProject},
			{Path: persRoot, Label: "personal", Source: SourcePersonal},
		},
		Overrides{},
		NewTrustList(map[string]bool{projRoot: true}, nil),
		nil, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	sk, ok := reg.Resolve("review-pr")
	if !ok {
		t.Fatal("/review-pr did not resolve")
	}
	if sk.Source != SourceProject {
		t.Errorf("winner source = %s, want project", sk.Source)
	}
	// personal reachable via namespace
	ns, ok := reg.Resolve("personal:review-pr")
	if !ok || ns.Source != SourcePersonal {
		t.Errorf("personal:review-pr did not resolve: ok=%v sk=%+v", ok, ns)
	}
	if !ns.Shadowed {
		t.Errorf("personal should be shadowed")
	}
}

func TestRegistry_BuiltinCollision(t *testing.T) {
	persRoot := t.TempDir()
	writeSkill(t, persRoot, "clear", "clears something")

	reg := NewRegistry(
		[]RootSpec{{Path: persRoot, Label: "personal", Source: SourcePersonal}},
		Overrides{}, TrustList{},
		[]string{"clear", "help"}, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Resolve("clear"); ok {
		t.Errorf("/clear must not resolve when built-in reserved")
	}
	ns, ok := reg.Resolve("personal:clear")
	if !ok {
		t.Fatal("personal:clear must resolve")
	}
	if !ns.Shadowed {
		t.Errorf("built-in-collided skill must be marked Shadowed")
	}
}

func TestRegistry_FingerprintStableAcrossReload(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "foo", "d")
	reg := NewRegistry(
		[]RootSpec{{Path: root, Label: "personal", Source: SourcePersonal}},
		Overrides{}, TrustList{}, nil, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	fp1 := reg.List()[0].Fingerprint
	if err := reg.Reload(); err != nil {
		t.Fatal(err)
	}
	fp2 := reg.List()[0].Fingerprint
	if fp1 != fp2 {
		t.Errorf("fingerprint changed across reload: %s -> %s", fp1, fp2)
	}
	if reg.Get(fp1) == nil {
		t.Errorf("Get(fp) returned nil")
	}
}

func TestRegistry_SameNameWithinRootLexFirstWins(t *testing.T) {
	// Impossible to have two same-named dirs in same root, but two roots of
	// the same source mimic "within a source" tie-break.
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeSkill(t, rootA, "x", "a")
	writeSkill(t, rootB, "x", "b")
	reg := NewRegistry(
		[]RootSpec{
			{Path: rootA, Label: "personal", Source: SourcePersonal},
			{Path: rootB, Label: "agents", Source: SourceOther},
		},
		Overrides{}, TrustList{}, nil, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	sk, ok := reg.Resolve("x")
	if !ok {
		t.Fatal("x did not resolve")
	}
	if sk.Source != SourcePersonal {
		t.Errorf("winner source = %s, want personal", sk.Source)
	}
}

func TestRegistry_EffectiveFlags(t *testing.T) {
	persRoot := t.TempDir()
	projRoot := t.TempDir()
	writeSkill(t, persRoot, "a", "x")
	writeSkill(t, projRoot, "b", "y")
	reg := NewRegistry(
		[]RootSpec{
			{Path: projRoot, Label: "project", Source: SourceProject},
			{Path: persRoot, Label: "personal", Source: SourcePersonal},
		},
		Overrides{}, TrustList{}, nil, nil,
	)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	a, _ := reg.Resolve("a")
	b, _ := reg.Resolve("b")

	if !a.Enabled || !a.ModelInvocable || !a.UserInvocable {
		t.Errorf("personal skill defaults: %+v", a)
	}
	// project skill untrusted -> pending, disabled
	if !b.Pending || b.Enabled {
		t.Errorf("untrusted project should be pending+disabled, got %+v", b)
	}

	// Trust the project. Project skill should enable but ModelInvocable stays false.
	reg.SetTrust(NewTrustList(map[string]bool{projRoot: true}, nil))
	b, _ = reg.Resolve("b")
	if !b.Enabled {
		t.Errorf("trusted project skill should be enabled: %+v", b)
	}
	if b.ModelInvocable {
		t.Errorf("trusted project skill should default ModelInvocable=false: %+v", b)
	}

	// Override explicitly turning auto on.
	tru := true
	reg.SetOverrides(Overrides{
		Skills: map[string]SkillOverride{
			string(b.Fingerprint): {Auto: &tru},
		},
	})
	b, _ = reg.Resolve("b")
	if !b.ModelInvocable {
		t.Errorf("override should enable auto, got %+v", b)
	}
}

func TestRegistry_FrontmatterHardOverrides(t *testing.T) {
	root := t.TempDir()
	hardPath := filepath.Join(root, "locked")
	os.MkdirAll(hardPath, 0o755)
	content := "---\nname: locked\ndescription: hard-locked\ndisable-model-invocation: true\nuser-invocable: false\n---\nbody\n"
	os.WriteFile(filepath.Join(hardPath, "SKILL.md"), []byte(content), 0o644)
	reg := NewRegistry(
		[]RootSpec{{Path: root, Label: "personal", Source: SourcePersonal}},
		Overrides{}, TrustList{}, nil, nil,
	)
	reg.Load()
	sk, _ := reg.Resolve("locked")
	if sk == nil {
		t.Fatal("not resolved")
	}
	tru := true
	reg.SetOverrides(Overrides{
		Skills: map[string]SkillOverride{
			string(sk.Fingerprint): {Auto: &tru, Manual: &tru},
		},
	})
	sk, _ = reg.Resolve("locked")
	if sk.ModelInvocable {
		t.Errorf("disable-model-invocation must be unoverridable: %+v", sk)
	}
	if sk.UserInvocable {
		t.Errorf("user-invocable=false must be unoverridable: %+v", sk)
	}
}
