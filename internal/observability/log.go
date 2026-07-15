package observability

import (
	"context"
	"log/slog"
	"os"
)

type contextLoggerKey struct{}

func Logger(service, env string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: env != "production",
	})
	logger := slog.New(handler).With("service", service, "env", env)
	slog.SetDefault(logger)
	return logger
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, contextLoggerKey{}, logger)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return nil
	}
	logger, _ := ctx.Value(contextLoggerKey{}).(*slog.Logger)
	return logger
}

func Log(ctx context.Context, level slog.Level, message string, args ...any) {
	if logger := LoggerFromContext(ctx); logger != nil {
		logger.Log(ctx, level, message, args...)
	}
}
