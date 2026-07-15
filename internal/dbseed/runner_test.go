package dbseed

import "testing"

func TestValidateEmbedded(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
}

func TestEmbeddedResourceCoverage(t *testing.T) {
	resources, err := loadResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 5 {
		t.Fatalf("resource count = %d, want 5", len(resources))
	}
	totals := map[string]int{}
	for _, resource := range resources {
		for table, count := range resource.Counts {
			totals[table] += count
		}
	}
	want := map[string]int{
		"permissions":                       51,
		"roles":                             10,
		"role_permissions":                  146,
		"provider_connectors":               2,
		"provider_catalog_entries":          15,
		"provider_model_capability_presets": 48,
		"prompt_templates":                  234,
		"prompt_versions":                   250,
	}
	for table, expected := range want {
		if totals[table] != expected {
			t.Fatalf("%s count = %d, want %d", table, totals[table], expected)
		}
	}
}
