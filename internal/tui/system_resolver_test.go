package tui

import "testing"

func TestOptions_SystemResolverFn_DefaultNil(t *testing.T) {
	var opts Options
	if opts.SystemResolverFn != nil {
		t.Fatal("expected nil default")
	}
}

func TestOptions_SystemResolverFn_InvokesClosure(t *testing.T) {
	called := ""
	opts := Options{SystemResolverFn: func(m string) string {
		called = m
		return "RESOLVED:" + m
	}}
	got := opts.SystemResolverFn("claude-sonnet-4-5")
	if called != "claude-sonnet-4-5" || got != "RESOLVED:claude-sonnet-4-5" {
		t.Fatalf("closure misbehaved: called=%q got=%q", called, got)
	}
}
