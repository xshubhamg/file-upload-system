// Package observability provides structured logging and Prometheus metrics.
package observability

import (
	"io"
	"log/slog"
	"strings"
)

// NewLogger returns a slog logger. JSON is the production default; text is
// easier to read during local development.
func NewLogger(level slog.Level, format string, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}
