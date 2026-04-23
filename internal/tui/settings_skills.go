package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/skills"
)

type skillsWidget struct {
	reg      *skills.Registry
	rows     []*skills.Skill // filtered + sorted
	cursor   int
	expanded map[skills.Fingerprint]bool

	// pending user changes, flushed on Ctrl+S.
	pendingOverrides map[skills.Fingerprint]skills.SkillOverride
	pendingAuto      *bool // global auto-invoke toggle
	pendingTrust     map[string]bool
	pendingDeny      map[string]bool

	trustPanel bool
	trustIdx   int

	rootsEditing bool
	rootsBuf     []rune
	pendingRoots []string // committed edits applied on Apply()
}

func newSkillsWidget(reg *skills.Registry) *skillsWidget {
	w := &skillsWidget{
		reg:              reg,
		expanded:         map[skills.Fingerprint]bool{},
		pendingOverrides: map[skills.Fingerprint]skills.SkillOverride{},
		pendingTrust:     map[string]bool{},
		pendingDeny:      map[string]bool{},
	}
	if reg != nil {
		ov := reg.Overrides()
		if ov.AutoInvokeEnable != nil {
			b := *ov.AutoInvokeEnable
			w.pendingAuto = &b
		}
		for k, v := range ov.Skills {
			w.pendingOverrides[skills.Fingerprint(k)] = v
		}
		t := reg.Trust()
		for k, v := range t.Trusted {
			w.pendingTrust[k] = v
		}
		for k, v := range t.Denied {
			w.pendingDeny[k] = v
		}
	}
	w.refreshRows()
	return w
}

func (w *skillsWidget) refreshRows() {
	if w.reg == nil {
		w.rows = nil
		return
	}
	all := w.reg.List()
	sort.SliceStable(all, func(i, j int) bool {
		si, sj := all[i], all[j]
		if si.Source != sj.Source {
			return rankSource(si.Source) < rankSource(sj.Source)
		}
		return si.Name < sj.Name
	})
	w.rows = all
	if w.cursor >= len(w.rows) {
		w.cursor = len(w.rows) - 1
	}
	if w.cursor < 0 {
		w.cursor = 0
	}
}

func rankSource(s skills.Source) int {
	switch s {
	case skills.SourceProject:
		return 0
	case skills.SourcePersonal:
		return 1
	default:
		return 2
	}
}

func (w *skillsWidget) effectiveOverride(sk *skills.Skill) skills.SkillOverride {
	if ov, ok := w.pendingOverrides[sk.Fingerprint]; ok {
		return ov
	}
	return skills.SkillOverride{Path: sk.Path}
}

func (w *skillsWidget) setOverride(sk *skills.Skill, mut func(*skills.SkillOverride)) {
	ov := w.effectiveOverride(sk)
	ov.Path = sk.Path
	mut(&ov)
	w.pendingOverrides[sk.Fingerprint] = ov
}

// Update handles a key message. Returns true if the widget consumed it.
func (w *skillsWidget) Update(msg tea.KeyMsg) bool {
	if w.rootsEditing {
		return w.updateRootsEdit(msg)
	}
	if w.trustPanel {
		return w.updateTrustPanel(msg)
	}
	if len(w.rows) == 0 {
		// still accept global-toggle key
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'g' {
			w.toggleGlobalAuto()
			return true
		}
		return false
	}
	switch msg.Type {
	case tea.KeyUp:
		if w.cursor > 0 {
			w.cursor--
		}
		return true
	case tea.KeyDown:
		if w.cursor < len(w.rows)-1 {
			w.cursor++
		}
		return true
	case tea.KeyEnter:
		sk := w.rows[w.cursor]
		w.expanded[sk.Fingerprint] = !w.expanded[sk.Fingerprint]
		return true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return false
		}
		switch msg.Runes[0] {
		case 'x':
			w.toggleCurrent(func(o *skills.SkillOverride) **bool { return &o.Enabled })
			return true
		case 'a':
			w.toggleCurrent(func(o *skills.SkillOverride) **bool { return &o.Auto })
			return true
		case 'm':
			w.toggleCurrent(func(o *skills.SkillOverride) **bool { return &o.Manual })
			return true
		case 't':
			w.trustPanel = true
			w.trustIdx = 0
			return true
		case 'g':
			w.toggleGlobalAuto()
			return true
		case 'r':
			w.startRootsEdit()
			return true
		}
	}
	return false
}

// startRootsEdit populates the buffer from current roots (pending > overrides
// > registry).
func (w *skillsWidget) startRootsEdit() {
	var src []string
	switch {
	case w.pendingRoots != nil:
		src = w.pendingRoots
	case w.reg != nil:
		if ov := w.reg.Overrides(); len(ov.SkillRoots) > 0 {
			src = ov.SkillRoots
		} else {
			for _, r := range w.reg.Roots() {
				src = append(src, r.Path)
			}
		}
	}
	w.rootsBuf = []rune(strings.Join(src, ", "))
	w.rootsEditing = true
}

func (w *skillsWidget) updateRootsEdit(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyEsc:
		w.rootsEditing = false
		w.rootsBuf = nil
		return true
	case tea.KeyEnter:
		w.commitRootsEdit()
		return true
	case tea.KeyBackspace, tea.KeyDelete:
		if len(w.rootsBuf) > 0 {
			w.rootsBuf = w.rootsBuf[:len(w.rootsBuf)-1]
		}
		return true
	case tea.KeyCtrlU:
		w.rootsBuf = nil
		return true
	case tea.KeySpace:
		w.rootsBuf = append(w.rootsBuf, ' ')
		return true
	case tea.KeyRunes:
		w.rootsBuf = append(w.rootsBuf, msg.Runes...)
		return true
	}
	return false
}

func (w *skillsWidget) commitRootsEdit() {
	parts := strings.Split(string(w.rootsBuf), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	w.pendingRoots = out
	w.rootsEditing = false
	w.rootsBuf = nil
}

// toggleCurrent flips a tri-state pointer flag on the cursor row's override.
// Sequence: unset → true → false → unset.
func (w *skillsWidget) toggleCurrent(pick func(*skills.SkillOverride) **bool) {
	sk := w.rows[w.cursor]
	w.setOverride(sk, func(o *skills.SkillOverride) {
		p := pick(o)
		switch {
		case *p == nil:
			v := true
			*p = &v
		case **p:
			v := false
			*p = &v
		default:
			*p = nil
		}
	})
}

func (w *skillsWidget) toggleGlobalAuto() {
	switch {
	case w.pendingAuto == nil:
		b := true
		w.pendingAuto = &b
	case *w.pendingAuto:
		b := false
		w.pendingAuto = &b
	default:
		w.pendingAuto = nil
	}
}

func (w *skillsWidget) updateTrustPanel(msg tea.KeyMsg) bool {
	paths := w.trustPanelEntries()
	switch msg.Type {
	case tea.KeyEsc:
		w.trustPanel = false
		return true
	case tea.KeyUp:
		if w.trustIdx > 0 {
			w.trustIdx--
		}
		return true
	case tea.KeyDown:
		if w.trustIdx < len(paths)-1 {
			w.trustIdx++
		}
		return true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 || len(paths) == 0 {
			return false
		}
		r := msg.Runes[0]
		row := paths[w.trustIdx]
		switch r {
		case 'd':
			if row.denied {
				return true
			}
			delete(w.pendingTrust, row.path)
			return true
		case 'u':
			if !row.denied {
				return true
			}
			delete(w.pendingDeny, row.path)
			return true
		}
	}
	return false
}

type trustEntry struct {
	path   string
	denied bool
}

func (w *skillsWidget) trustPanelEntries() []trustEntry {
	var out []trustEntry
	for p := range w.pendingTrust {
		out = append(out, trustEntry{p, false})
	}
	for p := range w.pendingDeny {
		out = append(out, trustEntry{p, true})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].denied != out[j].denied {
			return !out[i].denied
		}
		return out[i].path < out[j].path
	})
	return out
}

func (w *skillsWidget) View() string {
	if w.rootsEditing {
		return w.rootsEditView()
	}
	if w.trustPanel {
		return w.trustView()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Catalog budget: ~%d / %d tok\n",
		w.catalogTokens(), skills.CatalogSoftCapTokens)
	fmt.Fprintf(&b, "Global auto-invocation: %s  [g] toggle\n\n", tristateStr(w.pendingAuto))
	rs := w.currentRoots()
	if len(rs) > 0 {
		fmt.Fprintf(&b, "Roots: %s  [r]edit\n\n", strings.Join(rs, "  "))
	} else {
		b.WriteString("Roots: (none)  [r]edit\n\n")
	}
	if w.pendingRoots != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).
			Render("  * roots pending — save to apply") + "\n\n")
	}
	if len(w.rows) == 0 {
		b.WriteString("(no skills loaded)\n")
	} else {
		for i, sk := range w.rows {
			cursor := "  "
			if i == w.cursor {
				cursor = "▸ "
			}
			status := "✓"
			if sk.LoadError != nil {
				status = "✗"
			}
			name := sk.Name
			if sk.LoadError != nil {
				name = strings.TrimSuffix(strings.TrimPrefix(sk.Path, ""), "/SKILL.md")
			}
			ov := w.effectiveOverride(sk)
			fmt.Fprintf(&b, "%s%s %-24s %-9s %s %s\n",
				cursor, status,
				truncateStr(name, 24),
				string(sk.Source),
				flagCell("auto", ov.Auto, defaultAuto(sk)),
				flagCell("man", ov.Manual, defaultManual(sk)),
			)
			if sk.Pending {
				b.WriteString("    trust this project first\n")
			}
			if sk.LoadError != nil {
				b.WriteString("    LOAD ERROR: ")
				b.WriteString(sk.LoadError.Reason)
				b.WriteByte('\n')
			}
			if w.expanded[sk.Fingerprint] && sk.LoadError == nil {
				fmt.Fprintf(&b, "    %s\n    %s\n", truncateStr(sk.FM.Description, 76), sk.Path)
			}
		}
	}
	b.WriteString("\n")
	tl := w.reg.Trust()
	fmt.Fprintf(&b, "Trusted projects (%d)  [t]rust panel\n", len(tl.Trusted))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
		Render("[x]enabled [a]auto [m]manual [Enter]expand [r]oots [g]global-auto [t]rust"))
	return b.String()
}

func (w *skillsWidget) currentRoots() []string {
	if w.pendingRoots != nil {
		return w.pendingRoots
	}
	if w.reg == nil {
		return nil
	}
	if ov := w.reg.Overrides(); len(ov.SkillRoots) > 0 {
		return ov.SkillRoots
	}
	var out []string
	for _, r := range w.reg.Roots() {
		out = append(out, r.Path)
	}
	return out
}

func (w *skillsWidget) rootsEditView() string {
	var b strings.Builder
	b.WriteString("Edit skill roots (comma-separated)\n\n")
	b.WriteString("  ")
	b.WriteString(string(w.rootsBuf))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("█"))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
		Render("[Enter] commit  [Esc] cancel  [Ctrl+U] clear"))
	return b.String()
}

func (w *skillsWidget) trustView() string {
	var b strings.Builder
	b.WriteString("Trust management\n\n")
	entries := w.trustPanelEntries()
	if len(entries) == 0 {
		b.WriteString("(no trusted or denied paths)\n")
	} else {
		for i, e := range entries {
			cursor := "  "
			if i == w.trustIdx {
				cursor = "▸ "
			}
			tag := "trusted"
			if e.denied {
				tag = "denied "
			}
			fmt.Fprintf(&b, "%s%s %s\n", cursor, tag, e.path)
		}
	}
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
		Render("[d] demote trusted  [u] un-deny  [Esc] back"))
	return b.String()
}

func (w *skillsWidget) catalogTokens() int {
	if w.reg == nil {
		return 0
	}
	return w.reg.Catalog().TokensEstimate
}

func tristateStr(b *bool) string {
	if b == nil {
		return "default"
	}
	if *b {
		return "on"
	}
	return "off"
}

func flagCell(label string, ov *bool, def bool) string {
	v := def
	marker := " "
	if ov != nil {
		v = *ov
		marker = "*"
	}
	state := " "
	if v {
		state = "x"
	}
	return "[" + state + "]" + marker + label
}

func defaultAuto(sk *skills.Skill) bool {
	if sk.FM.DisableModelInvocation {
		return false
	}
	if sk.Source == skills.SourceProject {
		return false
	}
	return true
}

func defaultManual(sk *skills.Skill) bool {
	if sk.FM.UserInvocable != nil {
		return *sk.FM.UserInvocable
	}
	return true
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// Apply persists pending overrides and trust changes, then reloads the registry.
func (w *skillsWidget) Apply() error {
	if w.reg == nil {
		return nil
	}
	ov := w.reg.Overrides()
	if ov.Skills == nil {
		ov.Skills = map[string]skills.SkillOverride{}
	}
	for fp, so := range w.pendingOverrides {
		// Drop entries with no meaningful override content (all nil pointers).
		if so.Enabled == nil && so.Auto == nil && so.Manual == nil {
			delete(ov.Skills, string(fp))
			continue
		}
		ov.Skills[string(fp)] = so
	}
	ov.AutoInvokeEnable = w.pendingAuto
	if w.pendingRoots != nil {
		ov.SkillRoots = append([]string(nil), w.pendingRoots...)
	}
	ov.Trust = map[string]bool{}
	for k, v := range w.pendingTrust {
		if v {
			ov.Trust[k] = true
		}
	}
	ov.Deny = map[string]bool{}
	for k, v := range w.pendingDeny {
		if v {
			ov.Deny[k] = true
		}
	}
	// GC stale entries.
	validFP := map[skills.Fingerprint]bool{}
	for _, sk := range w.reg.List() {
		validFP[sk.Fingerprint] = true
	}
	ov.GC(validFP)
	if err := skills.SaveOverrides(ov); err != nil {
		return err
	}
	w.reg.SetOverrides(ov)
	w.reg.SetTrust(skills.NewTrustList(ov.Trust, ov.Deny))
	if w.pendingRoots != nil {
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		w.reg.SetRoots(skills.ExpandRoots(ov.SkillRoots, cwd, home))
	}
	return w.reg.Reload()
}
