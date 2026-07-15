package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Einzieg/cineweave/internal/dbseed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	command := "apply"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "validate" {
		if err := dbseed.ValidateEmbedded(); err != nil {
			return err
		}
		fmt.Println("embedded seed resources are valid")
		return nil
	}

	cfg, err := dbseed.ConfigFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	runner, err := dbseed.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer runner.Close()
	switch command {
	case "apply", "up":
		return runner.Apply(ctx)
	case "verify":
		return runner.Verify(ctx)
	default:
		return fmt.Errorf("unsupported seed command %q", command)
	}
}
