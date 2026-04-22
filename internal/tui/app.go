package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/logging"
)

type Event = agent.Event

type pendingTurn struct {
	events    <-chan Event
	raw       []rune
	committed int
	thinkRaw  []rune
	done      bool
}

type Model struct {
	agent     *agent.Agent
	input     textarea.Model
	status    statusbarModel
	approval  *Approval
	modal     modal
	debug     debugModel
	pending   *pendingTurn
	width     int
	height    int
	settings  Settings
	theme     *Theme
	git       gitInfo
	spinner   spinnerState
	turnStart time.Time
	ring      *logging.Ring
	factory   ProviderFactory
	ctxWinFn  func(model string) int
	suggest   suggestState
	lastCtrlC time.Time
	scanner   *blockScanner
}

type suggestState struct {
	active   bool
	matches  []string
	selected int
}

type statusbarModel struct {
	model        string
	provider     string
	state        string
	iter         int
	maxIter      int
	inputTokens  int
	outputTokens int
	width        int

	// token accumulators
	lastIterIn    int
	turnIn        int
	turnOut       int
	turnCacheRead int
	sessionIn     int
	sessionOut    int
}

type debugModel struct {
	viewport viewport.Model
	visible  bool
	ring     *logging.Ring
	theme    *Theme
}

type Options struct {
	Provider        string
	Model           string
	MaxIter         int
	ProviderFactory ProviderFactory
	ContextWindowFn func(model string) int
}

func New(a *agent.Agent, ring *logging.Ring, opts Options) *Model {
	settings := LoadSettings()
	theme := NewTheme(settings.Theme)

	ta := textarea.New()
	ta.Placeholder = "Ask SAM anything, or type /help"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.FocusedStyle.Prompt = theme.InputPrompt
	ta.BlurredStyle.Prompt = theme.InputPrompt.Foreground(lipgloss.Color("240"))
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.Focus()

	if opts.MaxIter == 0 {
		opts.MaxIter = 25
	}

	return &Model{
		agent:    a,
		input:    ta,
		settings: settings,
		theme:    theme,
		git:      probeGit(a.LaunchDir()),
		ring:     ring,
		scanner:  &blockScanner{},
		status: statusbarModel{
			provider: opts.Provider,
			model:    opts.Model,
			state:    "idle",
			maxIter:  opts.MaxIter,
		},
		debug:    debugModel{viewport: viewport.New(80, 20), ring: ring, theme: theme},
		factory:  opts.ProviderFactory,
		ctxWinFn: opts.ContextWindowFn,
	}
}
