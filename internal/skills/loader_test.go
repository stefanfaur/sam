package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanRoot_ValidSkill(t *testing.T) {
	skills, err := ScanRoot("testdata/skills/ok/..", "personal", SourcePersonal)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var ok *Skill
	for _, s := range skills {
		if filepath.Base(filepath.Dir(s.Path)) == "ok" {
			ok = s
		}
	}
	if ok == nil {
		t.Fatalf("missing ok skill in %+v", skills)
	}
	if ok.LoadError != nil {
		t.Fatalf("unexpected load error: %+v", ok.LoadError)
	}
	if ok.Name != "ok" {
		t.Fatalf("name = %q", ok.Name)
	}
	if ok.FM.ArgumentHint != "[thing]" {
		t.Fatalf("argument-hint = %q", ok.FM.ArgumentHint)
	}
	if !strings.Contains(ok.Body, "$ARGUMENTS") {
		t.Fatalf("body missing $ARGUMENTS: %q", ok.Body)
	}
	if ok.Fingerprint == "" || !strings.HasPrefix(string(ok.Fingerprint), "sha256:") {
		t.Fatalf("fingerprint = %q", ok.Fingerprint)
	}
}

func TestScanRoot_ErrorsDoNotPropagate(t *testing.T) {
	skills, err := ScanRoot("testdata/skills", "personal", SourcePersonal)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	byName := map[string]*Skill{}
	for _, s := range skills {
		byName[filepath.Base(filepath.Dir(s.Path))] = s
	}
	cases := []struct {
		dir         string
		wantErrSub  string
		wantLoadErr bool
	}{
		{"bad-name", "name invalid", true},
		{"missing-desc", "description required", true},
		{"no-fence", "missing opening", true},
	}
	for _, tc := range cases {
		s := byName[tc.dir]
		if s == nil {
			t.Errorf("%s not loaded", tc.dir)
			continue
		}
		if tc.wantLoadErr && s.LoadError == nil {
			t.Errorf("%s: expected LoadError", tc.dir)
		}
		if tc.wantLoadErr && !strings.Contains(s.LoadError.Reason, tc.wantErrSub) {
			t.Errorf("%s: reason %q missing substring %q", tc.dir, s.LoadError.Reason, tc.wantErrSub)
		}
	}
}

func TestScanRoot_UnknownFieldsPreserved(t *testing.T) {
	skills, err := ScanRoot("testdata/skills", "personal", SourcePersonal)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var uf *Skill
	for _, s := range skills {
		if s.Name == "unknown-fields" {
			uf = s
		}
	}
	if uf == nil {
		t.Fatalf("unknown-fields not loaded: %+v", skills)
	}
	if uf.LoadError != nil {
		t.Fatalf("load error: %+v", uf.LoadError)
	}
	if uf.FM.Unknown["future-field"] != "xyz" {
		t.Errorf("future-field not preserved: %+v", uf.FM.Unknown)
	}
	if uf.FM.Unknown["when_to_use"] != "when testing" {
		t.Errorf("when_to_use not preserved: %+v", uf.FM.Unknown)
	}
}

func TestScanRoot_BodyTooLarge(t *testing.T) {
	dir := t.TempDir()
	skDir := filepath.Join(dir, "huge")
	if err := os.MkdirAll(skDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", MaxBodyBytes+1)
	content := "---\nname: huge\ndescription: too big\n---\n" + body
	if err := os.WriteFile(filepath.Join(skDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skills, err := ScanRoot(dir, "personal", SourcePersonal)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(skills))
	}
	if skills[0].LoadError == nil || !strings.Contains(skills[0].LoadError.Reason, "50 KB") {
		t.Errorf("want size load error, got %+v", skills[0].LoadError)
	}
}

func TestScanRoot_MissingRootReturnsNil(t *testing.T) {
	skills, err := ScanRoot("/does/not/exist", "project", SourceProject)
	if err != nil {
		t.Errorf("missing root should be nil err, got %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills, got %+v", skills)
	}
}

func TestScanRoot_SymlinkEscapesRootSkipped(t *testing.T) {
	// Create a root with a symlink pointing outside root.
	root := t.TempDir()
	outside := t.TempDir()
	outsideSkill := filepath.Join(outside, "escaped")
	if err := os.MkdirAll(outsideSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: escaped\ndescription: outside root\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(outsideSkill, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// also create a normal skill inside root
	inside := filepath.Join(root, "inside")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	inContent := "---\nname: inside\ndescription: valid\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(inside, "SKILL.md"), []byte(inContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// symlink in root pointing at outside skill dir
	if err := os.Symlink(outsideSkill, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	skills, err := ScanRoot(root, "personal", SourcePersonal)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range skills {
		if s.LoadError == nil {
			names[s.Name] = true
		}
	}
	if !names["inside"] {
		t.Errorf("expected inside skill loaded, got %+v", names)
	}
	if names["escaped"] {
		t.Errorf("expected escaped (symlink outside root) skipped, got %+v", names)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		wantF string
		wantB string
		err   string
	}{
		{"basic", "---\nname: a\n---\nbody\n", "name: a\n", "body\n", ""},
		{"no-open", "name: a\n---\nbody", "", "", "missing opening"},
		{"no-close", "---\nname: a\nbody", "", "", "missing closing"},
		{"crlf", "---\r\nname: a\r\n---\r\nbody\r\n", "name: a\r\n", "body\r\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, body, err := splitFrontmatter([]byte(tc.in))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err=%v want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if string(fm) != tc.wantF {
				t.Errorf("fm = %q want %q", fm, tc.wantF)
			}
			if string(body) != tc.wantB {
				t.Errorf("body = %q want %q", body, tc.wantB)
			}
		})
	}
}

func TestFingerprintStable(t *testing.T) {
	a := fingerprint("/abs/path/SKILL.md")
	b := fingerprint("/abs/path/SKILL.md")
	if a != b {
		t.Fatalf("fingerprint not stable: %s vs %s", a, b)
	}
	if len(string(a)) != len("sha256:")+16 {
		t.Errorf("fingerprint length = %d", len(string(a)))
	}
}
