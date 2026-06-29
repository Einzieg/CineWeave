package observability

import (
	"log/slog"
	"os"
)

func Logger(service, env string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: env != "production",
	})
	return slog.New(handler).With("service", service, "env", env)
}
