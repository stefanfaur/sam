package tui

import (
	"sort"
	"strings"

	"github.com/stefanfaur/sam/internal/skills"
)

type Command string

const (
	CmdUnknown      Command = ""
	CmdNone         Command = "none"
	CmdQuit         Command = "quit"
	CmdClear        Command = "clear"
	CmdReset        Command = "reset"
	CmdModel        Command = "model"
	CmdProvider     Command = "provider"
	CmdAuth         Command = "auth"
	CmdCwd          Command = "cwd"
	CmdHelp         Command = "help"
	CmdSettings     Command = "settings"
	CmdSkill        Command = "skill"
	CmdReloadSkills Command = "reload-skills"
	CmdShowSkill    Command = "show-skill"
)

// commandSuggestions lists built-in slash commands.
var commandSuggestions = []struct {
	Name string
	Help string
}{
	{"/clear", "clear scrollback and history"},
	{"/cwd", "print launch directory"},
	{"/exit", "quit"},
	{"/help", "show keybindings and commands"},
	{"/model", "switch model; accepts provider/model or bare name"},
	{"/provider", "switch provider; defaults model to preset"},
	{"/auth", "view/set provider API keys"},
	{"/quit", "quit"},
	{"/reload-skills", "re-scan skill roots"},
	{"/reset", "reset history and session allowlist"},
	{"/show-skill", "expand a previously-collapsed skill invocation (arg: index; default last)"},
	{"/settings", "open settings modal (statusline, providers, theme)"},
}

// BuiltinNames lists slash names reserved by the TUI itself.
// Skills with a colliding name are demoted to namespaced-only.
var BuiltinNames = []string{
	"exit", "quit", "clear", "reset", "model", "provider", "auth", "cwd", "help",
	"settings", "reload-skills", "show-skill",
}

// parseCommand extracts a slash command from user input. The registry, when
// non-nil, is consulted for skill-name matches after built-ins are checked.
// The returned *skills.Skill is nil for everything except CmdSkill.
func parseCommand(text string, reg *skills.Registry) (Command, string, *skills.Skill) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return CmdNone, "", nil
	}
	parts := strings.SplitN(t[1:], " ", 2)
	name := strings.ToLower(parts[0])
	var arg string
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	switch name {
	case "exit", "quit":
		return CmdQuit, "", nil
	case "clear":
		return CmdClear, "", nil
	case "reset":
		return CmdReset, "", nil
	case "model":
		return CmdModel, arg, nil
	case "provider":
		return CmdProvider, arg, nil
	case "auth":
		return CmdAuth, arg, nil
	case "cwd":
		return CmdCwd, "", nil
	case "help":
		return CmdHelp, "", nil
	case "settings":
		return CmdSettings, "", nil
	case "reload-skills":
		return CmdReloadSkills, "", nil
	case "show-skill":
		return CmdShowSkill, arg, nil
	}
	// Slash name isn't a built-in. If a registry is attached, try skill lookup.
	// Skill names are lowercased at validation time (regex), so matching is
	// case-insensitive at the call site.
	if reg != nil {
		if sk, ok := reg.Resolve(name); ok {
			return CmdSkill, arg, sk
		}
	}
	return CmdUnknown, name, nil
}

// suggestionsFor returns the autocomplete list of slash commands (built-ins
// plus enabled user-invocable skills).
func suggestionsFor(reg *skills.Registry) []struct {
	Name string
	Help string
} {
	out := make([]struct {
		Name string
		Help string
	}, 0, len(commandSuggestions))
	out = append(out, commandSuggestions...)
	if reg == nil {
		return out
	}
	for _, sk := range reg.List() {
		if sk.LoadError != nil || !sk.Enabled || !sk.UserInvocable {
			continue
		}
		slash := "/" + sk.Name
		if sk.Shadowed {
			slash = "/" + sk.RootLabel + ":" + sk.Name
		}
		desc := sk.FM.Description
		if hint := sk.FM.ArgumentHint; hint != "" {
			slash += " " + hint
		}
		out = append(out, struct {
			Name string
			Help string
		}{slash, desc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

const helpTextBuiltin = `Available commands:
  /exit, /quit        Exit
  /clear              Clear scrollback and agent history
  /reset              Reset history and session allowlist
  /model [name]       Interactive picker; with arg sets directly
  /cwd                Show current working directory
  /settings           Open settings modal (statusline, providers, theme)
  /reload-skills      Re-scan skill roots
  /show-skill [n]     Expand a previously-collapsed skill invocation (default: latest)
  /help               Show this help

Key bindings:
  Enter               Submit
  Shift+Enter         Newline
  Ctrl+L              Toggle debug overlay
  Ctrl+C              Cancel turn; double-tap to quit
  Ctrl+D              Quit on empty input
  In /settings:       Tab/Shift+Tab switch tab, ↑↓←→ navigate, Ctrl+S save, Esc cancel`

// helpText returns the full help text, appending a Skills section if the
// registry has any user-invocable skills.
func helpTextFor(reg *skills.Registry) string {
	base := helpTextBuiltin
	if reg == nil {
		return base
	}
	type row struct{ slash, desc string }
	var rows []row
	for _, sk := range reg.List() {
		if sk.LoadError != nil || !sk.Enabled || !sk.UserInvocable {
			continue
		}
		slash := "/" + sk.Name
		if sk.Shadowed {
			slash = "/" + sk.RootLabel + ":" + sk.Name
		}
		rows = append(rows, row{slash, sk.FM.Description})
	}
	if len(rows) == 0 {
		return base
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].slash < rows[j].slash })
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nSkills:\n")
	maxShow := 10
	if len(rows) <= maxShow {
		for _, r := range rows {
			b.WriteString("  ")
			b.WriteString(r.slash)
			b.WriteString("  ")
			b.WriteString(r.desc)
			b.WriteByte('\n')
		}
	} else {
		for _, r := range rows[:maxShow] {
			b.WriteString("  ")
			b.WriteString(r.slash)
			b.WriteString("  ")
			b.WriteString(r.desc)
			b.WriteByte('\n')
		}
		b.WriteString("  … and ")
		b.WriteString(intStr(len(rows) - maxShow))
		b.WriteString(" more (see /settings → Skills)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// helpText is retained for backwards compatibility in places that do not have
// a registry pointer available (tests, etc.).
var helpText = helpTextBuiltin

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
