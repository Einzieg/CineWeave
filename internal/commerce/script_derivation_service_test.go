package commerce

import (
	"strings"
	"testing"
)

func TestNormalizeScriptDerivationPreviewInput(t *testing.T) {
	input := ScriptDerivationPreviewInput{
		SourceScriptUnitID: " source ",
		Count:              5,
		Dimension:          " scene ",
		Instruction:        " 替换场景 ",
		CandidateValues:    []string{"夜市", " 商场 ", ""},
		Preserve:           []string{"cta", "product_facts", "cta"},
	}
	if err := NormalizeScriptDerivationPreviewInput(&input); err != nil {
		t.Fatalf("NormalizeScriptDerivationPreviewInput: %v", err)
	}
	if input.SourceScriptUnitID != "source" || input.Dimension != "scene" ||
		input.Instruction != "替换场景" {
		t.Fatalf("normalized input = %+v", input)
	}
	if len(input.CandidateValues) != 2 ||
		len(input.Preserve) != 2 ||
		input.Preserve[0] != "cta" ||
		input.Preserve[1] != "product_facts" {
		t.Fatalf("normalized collections = %+v / %+v", input.CandidateValues, input.Preserve)
	}
}

func TestDecodeScriptDerivationPreviewProducesBatchReadyVariations(t *testing.T) {
	input := ScriptDerivationPreviewInput{
		SourceScriptUnitID: "source",
		Count:              2,
		Dimension:          "scene",
		Instruction:        "只替换场景",
		Preserve:           []string{"product_facts", "cta"},
	}
	source := ScriptUnit{
		ID: "source", Title: "脚本 2", CurrentContentHash: "sha256:source",
	}
	raw := `{
		"contractVersion":"commerce-script-derivation-preview/v1",
		"dimension":"scene",
		"instruction":"只替换场景",
		"preserve":["product_facts","cta"],
		"variations":[
			{"key":"night_market","label":"夜市场景","brief":"真实夜市体验"},
			{"key":"office","label":"办公室场景","brief":"午间办公体验"}
		]
	}`
	preview, err := DecodeScriptDerivationPreview(raw, input, source)
	if err != nil {
		t.Fatalf("DecodeScriptDerivationPreview: %v", err)
	}
	if len(preview.Variations) != 2 ||
		preview.Variations[0].Ordinal != 1 ||
		preview.Variations[1].Ordinal != 2 {
		t.Fatalf("preview variations = %+v", preview.Variations)
	}
	if preview.SourceScriptUnitID != source.ID ||
		preview.SourceContentHash != source.CurrentContentHash {
		t.Fatalf("preview source = %+v", preview)
	}
	batchInput := CreateScriptDerivationInput{
		Dimension: preview.Dimension, Instruction: preview.Instruction,
		Preserve: preview.Preserve, Variations: preview.Variations,
	}
	if err := NormalizeScriptDerivationInput(&batchInput); err != nil {
		t.Fatalf("preview is not batch-ready: %v", err)
	}
}

func TestDecodeScriptDerivationPreviewRejectsWrongCount(t *testing.T) {
	_, err := DecodeScriptDerivationPreview(
		`{"contractVersion":"commerce-script-derivation-preview/v1","dimension":"scene","instruction":"替换","preserve":[],"variations":[{"key":"one","label":"一","brief":"一"}]}`,
		ScriptDerivationPreviewInput{
			SourceScriptUnitID: "source", Count: 2, Dimension: "scene", Instruction: "替换",
		},
		ScriptUnit{ID: "source"},
	)
	if err == nil || !strings.Contains(err.Error(), "数量") {
		t.Fatalf("error = %v", err)
	}
}
