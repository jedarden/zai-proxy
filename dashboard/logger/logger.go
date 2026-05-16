// Package logger provides structured JSON logging for the zai-proxy-dashboard.
package logger

import (
	"context"
	"log/slog"
	"os"
	"sync"
)

var (
	defaultLogger *slog.Logger
	once          sync.Once
)

// Config holds logger configuration.
type Config struct {
	// Format: "json" or "text" (default: json)
	Format string
	// Level: "debug", "info", "warn", "error" (default: info)
	Level string
	// AddSource adds source file/line to log entries (default: false)
	AddSource bool
}

// DefaultConfig returns the default logger configuration.
func DefaultConfig() Config {
	return Config{
		Format:    getEnvOrDefault("LOG_FORMAT", "json"),
		Level:     getEnvOrDefault("LOG_LEVEL", "info"),
		AddSource: getEnvOrDefault("LOG_ADD_SOURCE", "false") == "true",
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Init initializes the default logger with the given configuration.
func Init(cfg Config) {
	once.Do(func() {
		var level slog.Level
		switch cfg.Level {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.AddSource,
		}

		var handler slog.Handler
		if cfg.Format == "text" {
			handler = slog.NewTextHandler(os.Stdout, opts)
		} else {
			handler = slog.NewJSONHandler(os.Stdout, opts)
		}

		defaultLogger = slog.New(handler)
		slog.SetDefault(defaultLogger)
	})
}

// Get returns the default logger.
func Get() *slog.Logger {
	if defaultLogger == nil {
		Init(DefaultConfig())
	}
	return defaultLogger
}

// Debug logs at debug level.
func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

// DebugCtx logs at debug level with context.
func DebugCtx(ctx context.Context, msg string, args ...any) {
	Get().DebugContext(ctx, msg, args...)
}

// Info logs at info level.
func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

// InfoCtx logs at info level with context.
func InfoCtx(ctx context.Context, msg string, args ...any) {
	Get().InfoContext(ctx, msg, args...)
}

// Warn logs at warn level.
func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

// WarnCtx logs at warn level with context.
func WarnCtx(ctx context.Context, msg string, args ...any) {
	Get().WarnContext(ctx, msg, args...)
}

// Error logs at error level.
func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

// ErrorCtx logs at error level with context.
func ErrorCtx(ctx context.Context, msg string, args ...any) {
	Get().ErrorContext(ctx, msg, args...)
}

// With returns a logger with additional context.
func With(args ...any) *slog.Logger {
	return Get().With(args...)
}

// WithGroup returns a logger with a group.
func WithGroup(name string) *slog.Logger {
	return Get().WithGroup(name)
}

// Component returns a logger with a component label.
func Component(name string) *slog.Logger {
	return Get().With("component", name)
}
