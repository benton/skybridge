package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogLine is one record as the panel shows it.
type LogLine struct {
	Time  string `json:"time"` // HH:MM:SS
	Level string `json:"level"`
	Text  string `json:"text"` // message followed by key=value pairs
}

// LogBuffer is a slog.Handler that keeps the last few hundred records and
// tells a listener about each new one. Wrap it with Tee to keep stderr output.
type LogBuffer struct {
	mu     sync.Mutex
	lines  []LogLine
	max    int
	level  slog.Level
	notify func(LogLine)
}

const logKeep = 300

// NewLogBuffer keeps records at or above level. notify may be nil.
func NewLogBuffer(level slog.Level, notify func(LogLine)) *LogBuffer {
	return &LogBuffer{max: logKeep, level: level, notify: notify}
}

// Lines returns the buffered records, oldest first.
func (b *LogBuffer) Lines() []LogLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]LogLine(nil), b.lines...)
}

// Enabled implements slog.Handler.
func (b *LogBuffer) Enabled(_ context.Context, l slog.Level) bool { return l >= b.level }

// Handle implements slog.Handler.
func (b *LogBuffer) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&sb, a)
		return true
	})
	line := LogLine{Time: r.Time.Format("15:04:05"), Level: strings.ToLower(r.Level.String()), Text: sb.String()}
	if r.Time.IsZero() {
		line.Time = time.Now().Format("15:04:05")
	}
	b.mu.Lock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
	notify := b.notify
	b.mu.Unlock()
	if notify != nil {
		notify(line)
	}
	return nil
}

func writeAttr(sb *strings.Builder, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	fmt.Fprintf(sb, " %s=%v", a.Key, a.Value.Resolve())
}

// WithAttrs implements slog.Handler; records keep the shared buffer.
func (b *LogBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &shared{LogBuffer: b, attrs: append([]slog.Attr(nil), attrs...)}
}

// WithGroup implements slog.Handler; groups are flattened, which is fine for a panel.
func (b *LogBuffer) WithGroup(string) slog.Handler { return b }

// shared is a LogBuffer view with extra attrs that writes into the parent.
type shared struct {
	*LogBuffer
	attrs []slog.Attr
}

func (s *shared) Handle(ctx context.Context, r slog.Record) error {
	r = r.Clone()
	r.AddAttrs(s.attrs...)
	return s.LogBuffer.Handle(ctx, r)
}

func (s *shared) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &shared{LogBuffer: s.LogBuffer, attrs: append(append([]slog.Attr(nil), s.attrs...), attrs...)}
}

// Tee sends every record to all of hs.
func Tee(hs ...slog.Handler) slog.Handler { return tee(hs) }

type tee []slog.Handler

func (t tee) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range t {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (t tee) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range t {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (t tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (t tee) WithGroup(name string) slog.Handler {
	out := make(tee, len(t))
	for i, h := range t {
		out[i] = h.WithGroup(name)
	}
	return out
}
