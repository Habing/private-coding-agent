// Package logx wires a process-wide *slog.Logger with JSON or text output
// and provides ctx-aware helpers that enrich every record with the standard
// correlation fields (request_id, trace_id, span_id, tenant_id, user_id).
//
// Format and level are configurable; everything else is fixed. Output always
// goes to os.Stdout — log shipping is a deployment concern, not an app one.
package logx

import (
	"log/slog"
	"os"
	"strings"
)

// Config controls handler format and minimum level.
//
// Format: "json" (default) or "text".
// Level:  "debug" | "info" (default) | "warn" | "error".
type Config struct {
	Format string
	Level  string
}

// New constructs a *slog.Logger from cfg. Unknown values fall back to the
// defaults (json + info) — invalid config never panics; misconfiguration
// at startup must not block the server from coming up.
func New(cfg Config) *slog.Logger {
	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		h = slog.NewTextHandler(os.Stdout, opts)
	default:
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// Install sets l as slog.Default() and as the package-level fallback used by
// FromCtx when ctx carries no logger. Safe to call multiple times.
func Install(l *slog.Logger) {
	slog.SetDefault(l)
	defaultLogger = l
}

var defaultLogger = slog.Default()

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
