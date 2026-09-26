// Package logging provides structured logging via slog with consistent configuration
package logging

import (
	"context"
	"log/slog"
	"os"
)

// contextKey is a custom type for context keys to avoid key collisions
type contextKey string

const loggerKey contextKey = "logger"

var defaultLogger *slog.Logger

// Init initializes the default logger at startup
func Init(level string) {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// parseLevel converts string level to slog.Level
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Info logs an info-level message
func Info(msg string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Info(msg, args...)
	}
}

// Warn logs a warning
func Warn(msg string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Warn(msg, args...)
	}
}

// Error logs an error
func Error(msg string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Error(msg, args...)
	}
}

// Debug logs a debug message
func Debug(msg string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Debug(msg, args...)
	}
}

// FromContext retrieves logger from context (for request-scoped logging with correlation ID)
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return log
	}
	return defaultLogger
}

// WithContext returns a new context with the logger attached
func WithContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}
