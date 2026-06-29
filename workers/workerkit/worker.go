package workerkit

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func Run(name string) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", name)
	logger.Info("worker started")

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	<-stopCh

	logger.Info("worker stopped")
}
