package tui

import "strings"

type Command string

const (
	CmdUnknown  Command = ""
	CmdNone     Command = "none"
	CmdQuit     Command = "quit"
	CmdClear    Command = "clear"
	CmdReset    Command = "reset"
	CmdModel    Command = "model"
	CmdProvider Command = "provider"
	CmdCwd      Command = "cwd"
	CmdHelp     Command = "help"
	CmdAuth     Command = "auth"
)

// commandSuggestions lists user-visible slash commands for autocomplete.
var commandSuggestions = []struct {
	Name string
	Help string
}{
	{"/auth", "configure API key for a provider"},
	{"/clear", "clear scrollback and history"},
	{"/cwd", "print launch directory"},
	{"/exit", "quit"},
	{"/help", "show keybindings and commands"},
	{"/model", "pick model (no arg = interactive)"},
	{"/provider", "pick provider (no arg = interactive)"},
	{"/quit", "quit"},
	{"/reset", "reset history and session allowlist"},
}

func parseCommand(text string) (Command, string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return CmdNone, ""
	}
	parts := strings.SplitN(t[1:], " ", 2)
	name := strings.ToLower(parts[0])
	var arg string
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	switch name {
	case "exit", "quit":
		return CmdQuit, ""
	case "clear":
		return CmdClear, ""
	case "reset":
		return CmdReset, ""
	case "model":
		return CmdModel, arg
	case "provider":
		return CmdProvider, arg
	case "cwd":
		return CmdCwd, ""
	case "help":
		return CmdHelp, ""
	case "auth":
		return CmdAuth, arg
	default:
		return CmdUnknown, name
	}
}

const helpText = `Available commands:
  /auth               Set or update API keys (saved to secrets.toml 0600)
  /exit, /quit       Exit
  /clear              Clear scrollback and agent history
  /reset              Reset history and session allowlist
  /model [name]       Interactive picker; with arg sets directly
  /provider [name]    Interactive picker; with arg sets directly
  /cwd                Show current working directory
  /help               Show this help

Key bindings:
  Enter               Submit
  Shift+Enter         Newline
  Ctrl+L              Toggle debug overlay
  Ctrl+C              Cancel turn; double-tap to quit
  Ctrl+D              Quit on empty input`
