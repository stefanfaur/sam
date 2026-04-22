package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Setup configures logging with a daily JSON file and an in-memory Ring.
// stateDir is typically $XDG_STATE_HOME/sam/logs. On error, falls back to
// stderr-only logging and returns the error (caller may log it and continue).
func Setup(stateDir string) (*slog.Logger, *Ring, error) {
	ring := NewRing(1024)
	ringH := NewRingHandler(ring, slog.LevelDebug)

	if stateDir == "" {
		h := multi{handlers: []slog.Handler{ringH}}
		logger := slog.New(h)
		slog.SetDefault(logger)
		return logger, ring, nil
	}

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return slog.New(ringH), ring, err
	}
	rotate(stateDir, 7)

	path := filepath.Join(stateDir, fmt.Sprintf("sam-%s.log", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(ringH), ring, err
	}

	fileH := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := multi{handlers: []slog.Handler{fileH, ringH}}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger, ring, nil
}

// DefaultStateDir returns $XDG_STATE_HOME/sam/logs or ~/.local/state/sam/logs.
func DefaultStateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "sam", "logs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "sam", "logs")
}

func rotate(dir string, keepDays int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(keepDays) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sam-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// multi fans out one record to multiple handlers.
type multi struct {
	handlers []slog.Handler
}

func (m multi) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m multi) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m multi) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := multi{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		out.handlers[i] = h.WithAttrs(attrs)
	}
	return out
}

func (m multi) WithGroup(name string) slog.Handler {
	out := multi{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		out.handlers[i] = h.WithGroup(name)
	}
	return out
}

// Discard is a no-op writer for testing / silenced modes.
var Discard io.Writer = io.Discard
