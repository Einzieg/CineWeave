package config

import "testing"

func TestValidateProductionSecret(t *testing.T) {
	if err := ValidateProductionSecret("development", "SECRET", "", "default"); err != nil {
		t.Fatalf("development secret should be optional: %v", err)
	}
	if err := ValidateProductionSecret("production", "SECRET", "", "default"); err == nil {
		t.Fatal("production secret should be required")
	}
	if err := ValidateProductionSecret("production", "SECRET", "default", "default"); err == nil {
		t.Fatal("production secret should reject development defaults")
	}
	if err := ValidateProductionSecret("production", "SECRET", "custom", "default"); err != nil {
		t.Fatalf("custom production secret should pass: %v", err)
	}
}
