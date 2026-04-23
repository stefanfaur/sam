package skills

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Overrides is the persisted user configuration in skills.toml.
type Overrides struct {
	SkillRoots       []string                 `toml:"skill_roots"`
	AutoInvokeEnable *bool                    `toml:"auto_invoke_enabled"`
	Trust            map[string]bool          `toml:"trust"`
	Deny             map[string]bool          `toml:"deny"`
	Skills           map[string]SkillOverride `toml:"skills"`
}

// SkillOverride is the per-fingerprint user override block.
type SkillOverride struct {
	Path    string `toml:"path"`
	Enabled *bool  `toml:"enabled"`
	Auto    *bool  `toml:"auto"`
	Manual  *bool  `toml:"manual"`
}

// OverridesPath returns ~/.config/sam/skills.toml (respects XDG_CONFIG_HOME).
func OverridesPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "skills.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sam", "skills.toml")
}

// LoadOverrides reads skills.toml from disk. A missing file is not an error.
func LoadOverrides() (Overrides, error) {
	return LoadOverridesFrom(OverridesPath())
}

// LoadOverridesFrom reads skills.toml from an explicit path.
func LoadOverridesFrom(path string) (Overrides, error) {
	var o Overrides
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	if err := toml.Unmarshal(data, &o); err != nil {
		return o, err
	}
	return o, nil
}

// SaveOverrides writes skills.toml atomically to the standard path.
func SaveOverrides(o Overrides) error {
	return SaveOverridesTo(OverridesPath(), o)
}

// SaveOverridesTo writes skills.toml to an explicit path.
func SaveOverridesTo(path string, o Overrides) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills.toml.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(o); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// GC drops skill override entries whose fingerprint is not present in validFP.
// Returns the number of entries removed.
func (o *Overrides) GC(validFP map[Fingerprint]bool) int {
	removed := 0
	for k := range o.Skills {
		if !validFP[Fingerprint(k)] {
			delete(o.Skills, k)
			removed++
		}
	}
	return removed
}
