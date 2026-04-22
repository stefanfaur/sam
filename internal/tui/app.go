package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/logging"
)

type Event = agent.Event

type pendingTurn struct {
	events    <-chan Event
	raw       []rune // full assistant text received so far
	committed int    // rune index in raw that has been flushed to scrollback
	thinkRaw  []rune // live thinking text (rendered in View(), never committed)
	done      bool   // TurnDone received
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
	glam      *glamour.TermRenderer
	ring      *logging.Ring
	factory   ProviderFactory
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
}

type debugModel struct {
	viewport viewport.Model
	visible  bool
	ring     *logging.Ring
}

type Options struct {
	Provider        string
	Model           string
	MaxIter         int
	ProviderFactory ProviderFactory
}

func New(a *agent.Agent, ring *logging.Ring, opts Options) *Model {
	glam, _ := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(80))

	ta := textarea.New()
	ta.Placeholder = "Ask SAM anything, or type /help"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.FocusedStyle.Prompt = inputPromptStyle
	ta.BlurredStyle.Prompt = inputPromptStyle.Foreground(lipgloss.Color("240"))
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.Focus()

	if opts.MaxIter == 0 {
		opts.MaxIter = 25
	}

	return &Model{
		agent:   a,
		input:   ta,
		glam:    glam,
		ring:    ring,
		scanner: &blockScanner{},
		status: statusbarModel{
			provider: opts.Provider,
			model:    opts.Model,
			state:    "idle",
			maxIter:  opts.MaxIter,
		},
		debug:   debugModel{viewport: viewport.New(80, 20), ring: ring},
		factory: opts.ProviderFactory,
	}
}
