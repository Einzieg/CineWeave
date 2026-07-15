package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Einzieg/cineweave/internal/dbmigrate"
	"github.com/Einzieg/cineweave/internal/observability"
)

func main() {
	logger := observability.Logger("cineweave-migrate", firstNonEmpty(os.Getenv("CINEWEAVE_ENV"), "development"))
	if err := run(); err != nil {
		logger.Error("database migration command failed", "error", err)
		os.Exit(1)
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func run() error {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "validate" {
		if err := dbmigrate.ValidateEmbedded(); err != nil {
			return err
		}
		fmt.Println("embedded migrations are valid")
		return nil
	}

	target := int64(0)
	if command == "down-to" {
		if len(os.Args) != 3 {
			return fmt.Errorf("usage: cineweave-migrate down-to <version>")
		}
		parsed, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid target version %q: %w", os.Args[2], err)
		}
		target = parsed
	}

	cfg, err := dbmigrate.ConfigFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	runner, err := dbmigrate.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer runner.Close()
	return runner.Run(ctx, command, target)
}
