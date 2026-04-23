package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkCatalogSkill(t *testing.T, root, name, desc string) {
	t.Helper()
	d := filepath.Join(root, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalog_EmptyWhenNoEligible(t *testing.T) {
	reg := NewRegistry(nil, Overrides{}, TrustList{}, nil, nil)
	reg.Load()
	cr := reg.Catalog()
	if cr.Block != "" {
		t.Errorf("empty registry should produce empty block, got %q", cr.Block)
	}
	if cr.Included != 0 {
		t.Errorf("included = %d", cr.Included)
	}
}

func TestCatalog_RendersEnabledSkills(t *testing.T) {
	root := t.TempDir()
	mkCatalogSkill(t, root, "alpha", "alpha desc")
	mkCatalogSkill(t, root, "beta", "beta desc")
	reg := NewRegistry(
		[]RootSpec{{Path: root, Label: "personal", Source: SourcePersonal}},
		Overrides{}, TrustList{}, nil, nil,
	)
	reg.Load()
	cr := reg.Catalog()
	if cr.Included != 2 {
		t.Errorf("included = %d, want 2", cr.Included)
	}
	if !strings.Contains(cr.Block, "<available_skills>") {
		t.Errorf("missing fence: %q", cr.Block)
	}
	if !strings.Contains(cr.Block, "alpha: alpha desc") {
		t.Errorf("missing alpha: %q", cr.Block)
	}
	// alpha comes before beta (lex order).
	if strings.Index(cr.Block, "alpha") >= strings.Index(cr.Block, "beta") {
		t.Errorf("alpha should precede beta: %q", cr.Block)
	}
}

func TestCatalog_ExcludesShadowedPendingAndError(t *testing.T) {
	// Project shadows personal. Personal is Enabled but Shadowed → excluded.
	proj := t.TempDir()
	pers := t.TempDir()
	mkCatalogSkill(t, proj, "foo", "project foo")
	mkCatalogSkill(t, pers, "foo", "personal foo")

	reg := NewRegistry(
		[]RootSpec{
			{Path: proj, Label: "project", Source: SourceProject},
			{Path: pers, Label: "personal", Source: SourcePersonal},
		},
		Overrides{}, NewTrustList(map[string]bool{proj: true}, nil), nil, nil,
	)
	reg.Load()
	// Project is trusted but defaults ModelInvocable=false — turn it on via override.
	proj_sk, _ := reg.Resolve("foo")
	tru := true
	reg.SetOverrides(Overrides{
		Skills: map[string]SkillOverride{
			string(proj_sk.Fingerprint): {Auto: &tru},
		},
	})

	cr := reg.Catalog()
	// Only the winning project skill should appear.
	if cr.Included != 1 {
		t.Errorf("included = %d, want 1", cr.Included)
	}
	if !strings.Contains(cr.Block, "project foo") {
		t.Errorf("missing project foo: %q", cr.Block)
	}
	if strings.Contains(cr.Block, "personal foo") {
		t.Errorf("shadowed personal should not appear: %q", cr.Block)
	}
}

func TestCatalog_PersonalSortsBeforeProject(t *testing.T) {
	proj := t.TempDir()
	pers := t.TempDir()
	mkCatalogSkill(t, proj, "aaa-proj", "project skill")
	mkCatalogSkill(t, pers, "zzz-pers", "personal skill")

	tru := true
	reg := NewRegistry(
		[]RootSpec{
			{Path: proj, Label: "project", Source: SourceProject},
			{Path: pers, Label: "personal", Source: SourcePersonal},
		},
		Overrides{}, NewTrustList(map[string]bool{proj: true}, nil), nil, nil,
	)
	reg.Load()
	// Enable auto on the project skill (default is off for project).
	projSk, _ := reg.Resolve("aaa-proj")
	reg.SetOverrides(Overrides{
		Skills: map[string]SkillOverride{
			string(projSk.Fingerprint): {Auto: &tru},
		},
	})

	cr := reg.Catalog()
	if cr.Included != 2 {
		t.Fatalf("want 2 included, got %d (block=%q)", cr.Included, cr.Block)
	}
	// Personal must appear before project even though "aaa" < "zzz".
	iPers := strings.Index(cr.Block, "zzz-pers")
	iProj := strings.Index(cr.Block, "aaa-proj")
	if iPers < 0 || iProj < 0 {
		t.Fatalf("both skills should be rendered: %q", cr.Block)
	}
	if iPers >= iProj {
		t.Errorf("personal should precede project in catalog: %q", cr.Block)
	}
}

func TestCatalog_UntrustedProjectSkipped(t *testing.T) {
	root := t.TempDir()
	mkCatalogSkill(t, root, "q", "a")
	reg := NewRegistry(
		[]RootSpec{{Path: root, Label: "project", Source: SourceProject}},
		Overrides{}, TrustList{}, nil, nil,
	)
	reg.Load()
	cr := reg.Catalog()
	if cr.Included != 0 {
		t.Errorf("untrusted project skill should not appear: %+v", cr)
	}
}
