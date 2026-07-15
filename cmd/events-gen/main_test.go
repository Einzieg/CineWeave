package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogCoversEveryStaticallyEmittedEvent(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := loadCatalog(filepath.Join(root, "packages", "events", "catalog.yaml"))
	if err != nil {
		t.Fatalf("load event catalog: %v", err)
	}
	known := make(map[string]bool, len(catalog.Events))
	for _, event := range catalog.Events {
		known[event.Name] = true
	}
	discovered, err := discoverEventNames(root)
	if err != nil {
		t.Fatalf("discover emitted events: %v", err)
	}
	for _, name := range discovered {
		if !known[name] {
			t.Errorf("backend event %q is not registered in packages/events/catalog.yaml", name)
		}
	}
}

func TestGeneratedEventContractsAreCurrent(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := loadCatalog(filepath.Join(root, "packages", "events", "catalog.yaml"))
	if err != nil {
		t.Fatalf("load event catalog: %v", err)
	}
	outputs, err := renderOutputs(catalog)
	if err != nil {
		t.Fatalf("render event contracts: %v", err)
	}
	for relativePath, expected := range outputs {
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		if !bytes.Equal(actual, expected) {
			t.Errorf("%s is stale; run go run ./cmd/events-gen", relativePath)
		}
	}
}

func TestCatalogAggregateTypesMatchStaticEmitters(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := loadCatalog(filepath.Join(root, "packages", "events", "catalog.yaml"))
	if err != nil {
		t.Fatalf("load event catalog: %v", err)
	}
	known := make(map[string]eventDefinition, len(catalog.Events))
	for _, event := range catalog.Events {
		known[event.Name] = event
	}
	emissions, err := discoverStaticEventEmissions(root)
	if err != nil {
		t.Fatalf("discover static event emissions: %v", err)
	}
	for _, emission := range emissions {
		definition, ok := known[emission.Name]
		if !ok {
			continue
		}
		if emission.AggregateType != definition.AggregateType {
			t.Errorf("%s emits %s with aggregate %q, catalog requires %q", emission.Location, emission.Name, emission.AggregateType, definition.AggregateType)
		}
	}
}

func TestProductionCodeUsesCatalogEventWriter(t *testing.T) {
	writes, err := discoverDirectEventOutboxWrites(repositoryRoot(t))
	if err != nil {
		t.Fatalf("discover direct event outbox writes: %v", err)
	}
	for _, location := range writes {
		t.Errorf("%s writes event_outbox directly; use events.AppendTx", location)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
