package policy

import (
	"encoding/json"
	"sync"
)

type Decision int

const (
	Allow Decision = iota
	Deny
	Ask
)

type ScopeKind int

const (
	ScopeOnce ScopeKind = iota
	ScopeSession
)

type Policy struct {
	mu       sync.Mutex
	defaults map[string]Decision
	session  map[string]bool // tools allowlisted for session
}

func Default() *Policy {
	return &Policy{
		defaults: map[string]Decision{
			"Read":  Allow,
			"Write": Ask,
			"Edit":  Ask,
			"Bash":  Ask,
		},
		session: map[string]bool{},
	}
}

func AllowAll() *Policy {
	return &Policy{
		defaults: map[string]Decision{
			"Read":  Allow,
			"Write": Allow,
			"Edit":  Allow,
			"Bash":  Allow,
		},
		session: map[string]bool{},
	}
}

func (p *Policy) Check(tool string, _ json.RawMessage) Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session[tool] {
		return Allow
	}
	d, ok := p.defaults[tool]
	if !ok {
		return Ask
	}
	return d
}

func (p *Policy) AllowSession(tool string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session[tool] = true
}

func (p *Policy) ResetSession() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session = map[string]bool{}
}
