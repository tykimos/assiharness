package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Setup initializes the default slog logger with JSON output to both os.Stderr
// and a log file at <logsDir>/assiharness.log. level must be one of "debug",
// "info", "warn", or "error" (case-insensitive). The logsDir is created if it
// does not already exist.
func Setup(level string, logsDir string) error {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return err
	}

	logPath := filepath.Join(logsDir, "assiharness.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	w := io.MultiWriter(os.Stderr, f)

	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(h))

	return nil
}

// Info logs a message at INFO level using the default logger.
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Error logs a message at ERROR level using the default logger.
func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

// Debug logs a message at DEBUG level using the default logger.
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// Warn logs a message at WARN level using the default logger.
func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

// WithComponent returns a child logger that includes a "component" field
// pre-set to name. Use it to scope log output to a specific subsystem.
func WithComponent(name string) *slog.Logger {
	return slog.Default().With("component", name)
}
