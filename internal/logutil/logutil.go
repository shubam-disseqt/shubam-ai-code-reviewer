// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package logutil is a thin wrapper around log/slog. It hands the CLI two
// output shapes: a legacy "[sacr] <stage>: ..." text form for humans and
// existing grep-based tooling, and a JSON-per-line form for CI/observability.
//
// The wrapper stays intentionally small — one custom Handler for the legacy
// text shape, and env-driven construction. Everything else is stock slog.
package logutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Format identifiers for the two supported output shapes.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Env vars read by FromEnv. Documented in docs/troubleshooting.html.
const (
	EnvFormat = "SACR_LOG_FORMAT"
	EnvLevel  = "SACR_LOG_LEVEL"
)

// stageKey is the attribute name every record gets. Kept as a constant so
// WithStage and the text handler agree on it.
const stageKey = "stage"

// New returns a slog.Logger writing to w in the given format at the given
// level. Unknown formats fall back to text; nil writer falls back to stderr.
func New(w io.Writer, format string, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	switch format {
	case FormatJSON:
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	case FormatText, "":
		return slog.New(newLegacyTextHandler(w, level))
	default:
		return slog.New(newLegacyTextHandler(w, level))
	}
}

// FromEnv builds a logger using SACR_LOG_FORMAT (text|json, default text)
// and SACR_LOG_LEVEL (DEBUG|INFO|WARN|ERROR, default INFO). Invalid values
// silently fall back to the defaults — logging must never crash the review.
func FromEnv(w io.Writer) *slog.Logger {
	format := strings.ToLower(strings.TrimSpace(os.Getenv(EnvFormat)))
	if format == "" {
		format = FormatText
	}
	level := parseLevel(os.Getenv(EnvLevel))
	return New(w, format, level)
}

// WithStage returns logger.With("stage", name). Kept as a helper so callers
// don't sprinkle the literal "stage" attribute name across the codebase.
func WithStage(logger *slog.Logger, name string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(stageKey, name)
}

// parseLevel maps case-insensitive names onto slog.Level. Anything unknown
// (including empty) defaults to INFO.
func parseLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// legacyTextHandler emits records in the historical "[sacr] <stage>:
// <msg> key=val key=val" shape so existing grep tooling and docs keep
// working. It's a stripped slog.Handler — no groups, no ReplaceAttr, no
// timestamp (the CLI's audience is per-run logs, not a log aggregator).
type legacyTextHandler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Level
	attrs []slog.Attr
}

func newLegacyTextHandler(w io.Writer, level slog.Level) *legacyTextHandler {
	return &legacyTextHandler{w: w, mu: &sync.Mutex{}, level: level}
}

func (h *legacyTextHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *legacyTextHandler) Handle(_ context.Context, r slog.Record) error {
	// Collect attrs: handler-scoped first, then record-scoped. Pull "stage"
	// out — it becomes the prefix, not a key=val pair.
	stage := ""
	kv := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		if a.Key == stageKey {
			stage = a.Value.String()
			continue
		}
		kv = append(kv, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == stageKey {
			stage = a.Value.String()
			return true
		}
		kv = append(kv, a)
		return true
	})

	// Level prefix: only surface non-INFO levels so INFO lines stay clean.
	// WARN/ERROR gain a "WARN " / "ERROR " prefix; DEBUG becomes "DEBUG ".
	var b strings.Builder
	b.WriteString("[sacr] ")
	if r.Level != slog.LevelInfo {
		b.WriteString(strings.ToUpper(r.Level.String()))
		b.WriteByte(' ')
	}
	if stage != "" {
		b.WriteString(stage)
		b.WriteString(": ")
	}
	b.WriteString(r.Message)

	// Stable key ordering keeps output greppable across runs.
	sort.SliceStable(kv, func(i, j int) bool { return kv[i].Key < kv[j].Key })
	for _, a := range kv {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(formatValue(a.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *legacyTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

// Groups collapse to a no-op — no code in this project uses them and the
// legacy text format wouldn't have a good way to render them.
func (h *legacyTextHandler) WithGroup(_ string) slog.Handler { return h }

// formatValue renders a slog.Value as text. Strings that contain whitespace
// get quoted; other kinds delegate to strconv / fmt so the output stays
// stable for tests. Kept small on purpose — this is not a full slog.Attr
// renderer.
func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if strings.ContainsAny(s, " \t\"") {
			return strconv.Quote(s)
		}
		return s
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	default:
		return fmt.Sprint(v.Any())
	}
}
