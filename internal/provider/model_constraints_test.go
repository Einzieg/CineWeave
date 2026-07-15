package provider

import (
	"encoding/json"
	"testing"
)

func TestModelPromptLengthConstraintUsesConfiguredUnit(t *testing.T) {
	constraint := ModelPromptLengthConstraint([]Capability{{
		InputLimits: json.RawMessage(`{"promptMaxLength":4096,"promptLengthUnit":"utf8_bytes"}`),
	}})
	if constraint.MaxLength != 4096 || constraint.Unit != PromptLengthUnitUTF8Bytes {
		t.Fatalf("constraint = %+v", constraint)
	}
	if got := MeasurePromptLength("蛊真人", constraint.Unit); got != 9 {
		t.Fatalf("UTF-8 byte length = %d, want 9", got)
	}
}

func TestModelPromptLengthConstraintDefaultsToCharacters(t *testing.T) {
	constraint := ModelPromptLengthConstraint([]Capability{{
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"promptMaxLength":4}}`),
	}})
	if constraint.MaxLength != 4 || constraint.Unit != PromptLengthUnitCharacters {
		t.Fatalf("constraint = %+v", constraint)
	}
	if !PromptWithinConstraint("蛊真人啊", constraint) {
		t.Fatal("four Chinese characters should fit a four-character limit")
	}
}
