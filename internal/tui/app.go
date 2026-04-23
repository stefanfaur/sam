package tui

import (
	"encoding/json"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/logging"
	"github.com/stefanfaur/sam/internal/skills"
)

type Event = agent.Event

type pendingTurn struct {
	events    <-chan Event
	raw       []rune
	committed int
	done      bool
	skill     *skillCardState
	thinking  *thinkingCardState
	tools     []*toolCardState
}

type toolCardState struct {
	ID        string
	Name      string
	Input     json.RawMessage
	Output    string
	Lines     int
	EditLines int
	Bytes     int
	IsError   bool
	Cancelled bool
	StartedAt time.Time
	EndedAt   time.Time
	Index     int
}

type thinkingCardState struct {
	Text      string
	StartedAt time.Time
	EndedAt   time.Time
	Index     int
}

type toolInvocation struct {
	Index   int
	Header  string
	Input   string
	Output  string
	IsError bool
}

type thinkingInvocation struct {
	Index  int
	Header string
	Body   string
}

// skillCardState drives the live animated skill-invocation card and carries
// the info needed to flush a final static card to scrollback on turn end.
type skillCardState struct {
	Header    string
	Body      string
	Source    string
	Index     int
	StartedAt time.Time
	Flushed   bool
}

type Model struct {
	agent         *agent.Agent
	input         textarea.Model
	status        statusbarModel
	approval      *Approval
	modal         modal
	debug         debugModel
	pending       *pendingTurn
	width         int
	height        int
	settings      Settings
	theme         *Theme
	git           gitInfo
	spinner       spinnerState
	turnStart     time.Time
	ring          *logging.Ring
	factory       ProviderFactory
	ctxWinFn      func(model string) int
	providers     map[string]config.ProviderEntry
	suggest       suggestState
	lastCtrlC     time.Time
	scanner       *blockScanner
	skills            *skills.Registry
	recentInvokes     []skillInvocation
	recentTools       []toolInvocation
	recentThinking    []thinkingInvocation
	nextToolIndex     int
	nextThinkingIndex int
}

// skillInvocation is a TUI-side record of a slash-invoked skill so its body
// can be re-surfaced via /show-skill after the collapsed header prints.
type skillInvocation struct {
	Header string
	Body   string
	Source string
}

const (
	maxRecentInvokes  = 32
	maxRecentTools    = 32
	maxRecentThinking = 32
)

// recordInvoke pushes an invocation onto the bounded ring buffer.
func (m *Model) recordInvoke(inv skillInvocation) {
	m.recentInvokes = append(m.recentInvokes, inv)
	if len(m.recentInvokes) > maxRecentInvokes {
		m.recentInvokes = m.recentInvokes[len(m.recentInvokes)-maxRecentInvokes:]
	}
}

func (m *Model) recordTool(inv toolInvocation) {
	m.recentTools = append(m.recentTools, inv)
	if len(m.recentTools) > maxRecentTools {
		m.recentTools = m.recentTools[len(m.recentTools)-maxRecentTools:]
	}
}

func (m *Model) recordThinking(inv thinkingInvocation) {
	m.recentThinking = append(m.recentThinking, inv)
	if len(m.recentThinking) > maxRecentThinking {
		m.recentThinking = m.recentThinking[len(m.recentThinking)-maxRecentThinking:]
	}
}

func (m *Model) nextToolIdx() int {
	m.nextToolIndex++
	return m.nextToolIndex
}

func (m *Model) nextThinkingIdx() int {
	m.nextThinkingIndex++
	return m.nextThinkingIndex
}

// SetSkills attaches the skills registry to the model. Called during boot.
func (m *Model) SetSkills(r *skills.Registry) {
	m.skills = r
	if tp := newTrustPromptModal(r); tp != nil {
		m.modal = tp
	}
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
	Providers       map[string]config.ProviderEntry
}

func New(a *agent.Agent, ring *logging.Ring, opts Options) *Model {
	settings := LoadSettings()
	theme := NewTheme(settings.Theme)

	ta := textarea.New()
	ta.Placeholder = ""
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
		debug:     debugModel{viewport: viewport.New(80, 20), ring: ring, theme: theme},
		factory:   opts.ProviderFactory,
		ctxWinFn:  opts.ContextWindowFn,
		providers: opts.Providers,
	}
}
