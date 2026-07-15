package dbmigrate

import "testing"

func TestValidateEmbedded(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
}

func TestProductionEnvironment(t *testing.T) {
	for _, value := range []string{"prod", "production", " Production "} {
		if !IsProduction(value) {
			t.Fatalf("IsProduction(%q) = false", value)
		}
	}
	if IsProduction("development") {
		t.Fatal("development must not be treated as production")
	}
}

func TestDestructiveCommands(t *testing.T) {
	for _, command := range []string{"down", "down-to", "reset"} {
		if !isDestructive(command) {
			t.Fatalf("isDestructive(%q) = false", command)
		}
	}
	if isDestructive("up") {
		t.Fatal("up must not be treated as destructive")
	}
}
