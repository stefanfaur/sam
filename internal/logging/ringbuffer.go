package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry is a minimal log record snapshot.
type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// Ring is a bounded ring buffer for log entries and a slog.Handler.
type Ring struct {
	mu    sync.Mutex
	buf   []Entry
	size  int
	head  int
	count int

	subs []chan Entry
}

func NewRing(size int) *Ring {
	if size <= 0 {
		size = 1024
	}
	return &Ring{buf: make([]Entry, size), size: size}
}

func (r *Ring) Add(e Entry) {
	r.mu.Lock()
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
	subs := r.subs
	r.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (r *Ring) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, r.count)
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.size) % r.size
		out[i] = r.buf[idx]
	}
	return out
}

// Subscribe returns a channel that receives new entries. Caller should drain it;
// missed entries are dropped.
func (r *Ring) Subscribe(buf int) <-chan Entry {
	if buf <= 0 {
		buf = 16
	}
	ch := make(chan Entry, buf)
	r.mu.Lock()
	r.subs = append(r.subs, ch)
	r.mu.Unlock()
	return ch
}

// RingHandler adapts a Ring as a slog.Handler.
type RingHandler struct {
	ring  *Ring
	level slog.Level
	attrs []slog.Attr
	group string
}

func NewRingHandler(r *Ring, level slog.Level) *RingHandler {
	return &RingHandler{ring: r, level: level}
}

func (h *RingHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *RingHandler) Handle(_ context.Context, rec slog.Record) error {
	attrs := map[string]any{}
	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Any()
	}
	rec.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.ring.Add(Entry{Time: rec.Time, Level: rec.Level, Message: rec.Message, Attrs: attrs})
	return nil
}

func (h *RingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &c
}

func (h *RingHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.group = name
	return &c
}
