package skills

import (
	"fmt"
	"sort"
	"strings"
)

// CatalogSoftCapTokens warns the caller that the rendered block is large.
const CatalogSoftCapTokens = 2048

// CatalogHardCapTokens forces demotion to manual-only for overflow skills.
const CatalogHardCapTokens = 4096

// CatalogResult is the rendered <available_skills> block plus diagnostics.
type CatalogResult struct {
	Block          string
	TokensEstimate int
	Included       int
	Demoted        []*Skill
}

// Catalog renders the model-invocable block for the current enabled skills.
func (r *Registry) Catalog() CatalogResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var elig []*Skill
	for _, sk := range r.skills {
		if sk.LoadError != nil {
			continue
		}
		if !sk.Enabled || !sk.ModelInvocable || sk.Shadowed || sk.Pending {
			continue
		}
		elig = append(elig, sk)
	}
	// Catalog ordering (per design §7.2): personal > project > other. Differs
	// from resolution precedence, which prefers project over personal. This
	// ordering determines which skills get demoted first when the hard-cap is
	// exceeded — personal skills stay, project skills demote first.
	sort.SliceStable(elig, func(i, j int) bool {
		if catalogRank(elig[i].Source) != catalogRank(elig[j].Source) {
			return catalogRank(elig[i].Source) < catalogRank(elig[j].Source)
		}
		return elig[i].Name < elig[j].Name
	})

	var lines []string
	running := 0
	res := CatalogResult{}
	for _, sk := range elig {
		line := fmt.Sprintf("- %s: %s <SKILL.md: %s>", sk.Name, sk.FM.Description, sk.Path)
		tok := estimateTokens(line)
		if running+tok > CatalogHardCapTokens {
			res.Demoted = append(res.Demoted, sk)
			continue
		}
		lines = append(lines, line)
		running += tok
		res.Included++
	}
	if len(lines) == 0 {
		return res
	}
	res.Block = "<available_skills>\n" + strings.Join(lines, "\n") +
		"\n</available_skills>\n\nTo use a skill, Read its SKILL.md path, then follow the instructions inside.\n"
	res.TokensEstimate = running
	return res
}

func estimateTokens(s string) int { return (len(s) + 3) / 4 }

func catalogRank(s Source) int {
	switch s {
	case SourcePersonal:
		return 0
	case SourceProject:
		return 1
	default:
		return 2
	}
}
