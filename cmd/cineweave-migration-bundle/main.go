package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Einzieg/cineweave/internal/migrationbundle"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	command := "verify"
	if len(arguments) > 0 {
		command = arguments[0]
	}
	directory := filepath.FromSlash("db/baselines/current")
	if len(arguments) > 1 {
		directory = arguments[1]
	}
	switch command {
	case "generate":
		if err := migrationbundle.Generate(directory); err != nil {
			return err
		}
		fmt.Printf("generated migration baseline in %s\n", directory)
		return nil
	case "verify":
		if err := migrationbundle.Verify(directory); err != nil {
			return err
		}
		fmt.Printf("migration baseline is current: %s\n", directory)
		return nil
	default:
		return fmt.Errorf("usage: cineweave-migration-bundle [generate|verify] [directory]")
	}
}
