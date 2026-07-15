package workflows

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
)

func TestParseReviewedImagePrompt(t *testing.T) {
	reviewed, err := parseReviewedImagePrompt("```json\n"+`{"approved":true,"finalPrompt":"A tense medium close-up.","negativePrompt":"no extra people"}`+"\n```", generatedImagePrompt{})
	if err != nil {
		t.Fatalf("parseReviewedImagePrompt: %v", err)
	}
	if !reviewed.Approved || reviewed.FinalPrompt != "A tense medium close-up." {
		t.Fatalf("reviewed = %+v", reviewed)
	}
}

func TestBuildReviewedShotImagePromptExcludesDialogueText(t *testing.T) {
	contextValue := shotImagePromptAgentContext{Shot: shotImagePromptShot{
		Visual:      "方源在山巅下令。",
		Camera:      "中近景",
		AspectRatio: "16:9",
	}}
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "杀上青茅山。", Kind: "dialogue"}}
	prompt := buildReviewedShotImagePrompt(
		stripScriptDialogueFromImagePrompt("Cinematic medium close-up of Fang Yuan commanding the group. 方源说：“杀上青茅山。” 台词仅作为表演上下文。", dialogue),
		"no extra people",
		contextValue,
	)
	for _, required := range []string{"方源", "No on-screen text", "16:9"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
	if strings.Contains(prompt, "杀上青茅山。") || strings.Contains(prompt, "台词") || strings.Contains(prompt, "Performance context only") {
		t.Fatalf("prompt retained dialogue text: %s", prompt)
	}
	if err := validateShotImagePromptContainsNoDialogue(prompt, dialogue); err != nil {
		t.Fatalf("dialogue-free prompt rejected: %v", err)
	}
}

func TestValidateShotImagePromptContainsNoDialogueRejectsLiteralLine(t *testing.T) {
	dialogue := []StoryboardDialogueLine{{Speaker: "方源", Text: "杀上青茅山。", Kind: "dialogue"}}
	if err := validateShotImagePromptContainsNoDialogue("方源正在说：杀上青茅山。", dialogue); err == nil {
		t.Fatal("expected literal dialogue to be rejected")
	}
}

func TestValidateShotImagePromptForCandidatesUsesUTF8BytesAndRejectsRawProfiles(t *testing.T) {
	candidates := []provider.GatewayModelConstraintCandidate{{
		ModelKey: "image-model",
		Prompt: provider.PromptLengthConstraint{
			MaxLength: 8,
			Unit:      provider.PromptLengthUnitUTF8Bytes,
		},
	}}
	if _, err := validateShotImagePromptForCandidates("蛊真人", candidates); err == nil {
		t.Fatal("expected nine-byte prompt to exceed eight-byte limit")
	}
	if _, err := validateShotImagePromptForCandidates(`{"baseClothing":"black robe"}`, nil); err == nil {
		t.Fatal("expected copied raw asset profile to be rejected")
	}
}

func TestCompactShotImagePromptAssetsRemovesRawProfileAndBoundsFields(t *testing.T) {
	values := compactShotImagePromptAssets([]ShotVideoPromptAsset{{
		AssetID:           "asset-1",
		AssetType:         "character",
		Name:              "方源",
		Description:       strings.Repeat("人", 700),
		Profile:           json.RawMessage(`{"forbiddenChanges":["everything"]}`),
		ConsistencyPrompt: strings.Repeat("衣", 1200),
		NegativePrompt:    strings.Repeat("禁", 500),
		Requirement: map[string]any{
			"action": strings.Repeat("动", 700),
			"prompt": strings.Repeat("提", 700),
		},
	}})
	if len(values) != 1 {
		t.Fatalf("values = %+v", values)
	}
	asset := values[0]
	if len(asset.Profile) != 0 {
		t.Fatalf("raw profile was retained: %s", asset.Profile)
	}
	if len([]rune(asset.Description)) != 500 || len([]rune(asset.ConsistencyPrompt)) != 1000 || len([]rune(asset.NegativePrompt)) != 300 {
		t.Fatalf("unexpected compact lengths: description=%d consistency=%d negative=%d", len([]rune(asset.Description)), len([]rune(asset.ConsistencyPrompt)), len([]rune(asset.NegativePrompt)))
	}
	if len([]rune(asset.Requirement["action"].(string))) != 500 || len([]rune(asset.Requirement["prompt"].(string))) != 500 {
		t.Fatalf("requirement was not bounded: %+v", asset.Requirement)
	}
}
