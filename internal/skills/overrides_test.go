package skills

import (
	"path/filepath"
	"testing"
)

func TestOverrides_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.toml")

	tru, fls := true, false
	orig := Overrides{
		SkillRoots:       []string{".sam/skills", "~/.sam/skills"},
		AutoInvokeEnable: &tru,
		Trust:            map[string]bool{"/Users/a/repo": true},
		Deny:             map[string]bool{"/Users/a/evil": true},
		Skills: map[string]SkillOverride{
			"sha256:deadbeef0000abcd": {
				Path:    "/Users/a/.sam/skills/foo/SKILL.md",
				Enabled: &tru,
				Auto:    &fls,
				Manual:  &tru,
			},
		},
	}
	if err := SaveOverridesTo(path, orig); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOverridesFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SkillRoots) != 2 || got.SkillRoots[0] != ".sam/skills" {
		t.Errorf("roots = %+v", got.SkillRoots)
	}
	if got.AutoInvokeEnable == nil || !*got.AutoInvokeEnable {
		t.Errorf("auto_invoke_enabled = %v", got.AutoInvokeEnable)
	}
	if !got.Trust["/Users/a/repo"] {
		t.Errorf("trust missing: %+v", got.Trust)
	}
	ov := got.Skills["sha256:deadbeef0000abcd"]
	if ov.Enabled == nil || !*ov.Enabled {
		t.Errorf("enabled lost: %+v", ov)
	}
	if ov.Auto == nil || *ov.Auto {
		t.Errorf("auto=false lost: %+v", ov)
	}
}

func TestOverrides_MissingFileOK(t *testing.T) {
	got, err := LoadOverridesFrom(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
	if got.Skills != nil {
		t.Errorf("expected zero overrides, got %+v", got)
	}
}

func TestOverrides_GCRemovesStale(t *testing.T) {
	tru := true
	o := Overrides{
		Skills: map[string]SkillOverride{
			"sha256:aaaa":  {Enabled: &tru},
			"sha256:bbbb":  {Enabled: &tru},
			"sha256:stale": {Enabled: &tru},
		},
	}
	valid := map[Fingerprint]bool{
		Fingerprint("sha256:aaaa"): true,
		Fingerprint("sha256:bbbb"): true,
	}
	removed := o.GC(valid)
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, exists := o.Skills["sha256:stale"]; exists {
		t.Errorf("stale entry still present")
	}
	if len(o.Skills) != 2 {
		t.Errorf("len = %d, want 2", len(o.Skills))
	}
}

func TestOverrides_PointerUnsetVsFalse(t *testing.T) {
	// Unset Enabled should be nil, explicit false should be pointer to false.
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.toml")
	fls := false
	orig := Overrides{
		Skills: map[string]SkillOverride{
			"sha256:unset":    {}, // pointers nil
			"sha256:explicit": {Enabled: &fls},
		},
	}
	if err := SaveOverridesTo(path, orig); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOverridesFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Skills["sha256:unset"].Enabled != nil {
		t.Errorf("unset should decode to nil, got %v", got.Skills["sha256:unset"].Enabled)
	}
	ex := got.Skills["sha256:explicit"]
	if ex.Enabled == nil {
		t.Fatalf("explicit should decode to non-nil")
	}
	if *ex.Enabled != false {
		t.Errorf("explicit = %v, want false", *ex.Enabled)
	}
}
