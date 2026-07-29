package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Einzieg/cineweave/internal/editionmigration"
	"github.com/Einzieg/cineweave/internal/migrationstream"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s <commercial-migration-directory>\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	directory, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve Commercial migration directory: %v\n", err)
		os.Exit(1)
	}
	info, err := os.Stat(directory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect Commercial migration directory: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Commercial migration path is not a directory: %s\n", directory)
		os.Exit(1)
	}
	migrations, err := migrationstream.Validate(
		editionmigration.CommercialDefinition(os.DirFS(directory)),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Commercial DDL owner check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"Commercial migration stream passed DDL owner check: files=%d head=%d ledger=%s.%s\n",
		len(migrations),
		migrations[len(migrations)-1].Version,
		editionmigration.CommercialControlSchema,
		editionmigration.CommercialLedgerTable,
	)
}
