package tui

import (
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/config"
)

func testProviders() map[string]config.ProviderEntry {
	return map[string]config.ProviderEntry{
		"minimax": {
			Name: "minimax", Wire: "anthropic", DefaultModel: "MiniMax-M2.7",
			Models: []string{"MiniMax-M2.6"},
		},
		"anthropic": {
			Name: "anthropic", Wire: "anthropic", DefaultModel: "claude-sonnet-4-5",
			ModelPrefixes: []string{"claude-"},
		},
		"openai": {
			Name: "openai", Wire: "openai", DefaultModel: "gpt-4o-mini",
		},
		"arcee": {
			Name: "arcee", Wire: "openai", DefaultModel: "trinity-large-thinking",
		},
	}
}

func TestResolveModelExplicit(t *testing.T) {
	p, m, err := resolveModelSpec("openai/gpt-4o-mini", testProviders())
	if err != nil || p != "openai" || m != "gpt-4o-mini" {
		t.Fatalf("got (%q, %q, %v)", p, m, err)
	}
}

func TestResolveModelExplicitUnknownProvider(t *testing.T) {
	if _, _, err := resolveModelSpec("ghost/x", testProviders()); err == nil {
		t.Fatal("want error")
	}
}

func TestResolveModelBareDefaultModel(t *testing.T) {
	p, m, err := resolveModelSpec("MiniMax-M2.7", testProviders())
	if err != nil || p != "minimax" || m != "MiniMax-M2.7" {
		t.Fatalf("got (%q, %q, %v)", p, m, err)
	}
}

func TestResolveModelBareModelsMember(t *testing.T) {
	p, _, err := resolveModelSpec("MiniMax-M2.6", testProviders())
	if err != nil || p != "minimax" {
		t.Fatalf("got (%q, %v)", p, err)
	}
}

func TestResolveModelBarePrefixTableSoleProvider(t *testing.T) {
	// With only one openai-wire provider, the hardcoded prefix table
	// resolves bare names like gpt-5 unambiguously.
	providers := map[string]config.ProviderEntry{
		"openai": {
			Name: "openai", Wire: "openai", DefaultModel: "gpt-4o-mini",
		},
		"anthropic": {
			Name: "anthropic", Wire: "anthropic", DefaultModel: "claude-sonnet-4-5",
		},
	}
	p, _, err := resolveModelSpec("gpt-5", providers)
	if err != nil || p != "openai" {
		t.Fatalf("got (%q, %v)", p, err)
	}
}

func TestResolveModelAmbiguousBetweenOpenAIWireProviders(t *testing.T) {
	// gpt-4o matches openai's prefix table; arcee is also openai-wire.
	// MatchesPrefix is true for both entries so we expect ambiguity.
	_, _, err := resolveModelSpec("gpt-4o-mini", testProviders())
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	// The error should mention both candidates.
	if !strings.Contains(err.Error(), "openai") || !strings.Contains(err.Error(), "arcee") {
		t.Errorf("err should list matches: %v", err)
	}
}

func TestResolveModelUnknown(t *testing.T) {
	_, _, err := resolveModelSpec("fictional-x", testProviders())
	if err == nil {
		t.Fatal("want unknown-model error")
	}
	if !strings.Contains(err.Error(), "try provider/model") {
		t.Errorf("err hint missing: %v", err)
	}
}
