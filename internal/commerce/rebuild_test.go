package commerce

import (
	"encoding/json"
	"testing"
)

func TestPrepareProjectRebuildUnitRewritesCompleteProductionIdentity(t *testing.T) {
	seed := ProjectRebuildUnitSeed{
		SourceUnitGenerationID: "source-unit-generation",
		ConfigurationSnapshot: json.RawMessage(`{
			"schemaVersion": 3,
			"projectGenerationId": "old-generation",
			"videoProductionBindingId": "old-video-binding",
			"videoProductionBindingRevision": 1,
			"commerceWorkflowBindingId": "old-commerce-binding",
			"commerceWorkflowBindingRevision": 1,
			"workflowTemplateVersionId": "old-template",
			"rebuildId": "old-rebuild",
			"targetConfigurationHash": "old-hash"
		}`),
	}
	target := InitialBindingResult{
		ProjectGenerationID:       "new-generation",
		VideoBindingID:            "new-video-binding",
		VideoBindingRevision:      2,
		VideoProfileSnapshotHash:  "new-profile-hash",
		CommerceBindingID:         "new-commerce-binding",
		CommerceBindingRevision:   2,
		CommerceConfigurationHash: "new-commerce-hash",
	}

	prepared, err := prepareProjectRebuildUnit(seed, target, "new-template", "new-rebuild")
	if err != nil {
		t.Fatalf("prepare unit: %v", err)
	}
	var snapshot struct {
		ProjectGenerationID             string `json:"projectGenerationId"`
		VideoProductionBindingID        string `json:"videoProductionBindingId"`
		VideoProductionBindingRevision  int64  `json:"videoProductionBindingRevision"`
		CommerceWorkflowBindingID       string `json:"commerceWorkflowBindingId"`
		CommerceWorkflowBindingRevision int64  `json:"commerceWorkflowBindingRevision"`
		WorkflowTemplateVersionID       string `json:"workflowTemplateVersionId"`
		RebuildID                       string `json:"rebuildId"`
		SourceUnitGenerationID          string `json:"sourceUnitGenerationId"`
		TargetConfigurationHash         string `json:"targetConfigurationHash"`
		ProductionIdentity              struct {
			ProjectGenerationID             string `json:"projectGenerationId"`
			VideoProductionBindingID        string `json:"videoProductionBindingId"`
			VideoProductionBindingRevision  int64  `json:"videoProductionBindingRevision"`
			CommerceWorkflowBindingID       string `json:"commerceWorkflowBindingId"`
			CommerceWorkflowBindingRevision int64  `json:"commerceWorkflowBindingRevision"`
			CommerceConfigurationHash       string `json:"commerceConfigurationHash"`
		} `json:"productionIdentity"`
	}
	if err := json.Unmarshal(prepared.TargetConfiguration, &snapshot); err != nil {
		t.Fatalf("decode prepared snapshot: %v", err)
	}
	if snapshot.ProjectGenerationID != target.ProjectGenerationID ||
		snapshot.VideoProductionBindingID != target.VideoBindingID ||
		snapshot.VideoProductionBindingRevision != target.VideoBindingRevision ||
		snapshot.CommerceWorkflowBindingID != target.CommerceBindingID ||
		snapshot.CommerceWorkflowBindingRevision != target.CommerceBindingRevision ||
		snapshot.WorkflowTemplateVersionID != "new-template" ||
		snapshot.RebuildID != "new-rebuild" ||
		snapshot.SourceUnitGenerationID != seed.SourceUnitGenerationID ||
		snapshot.TargetConfigurationHash != target.CommerceConfigurationHash ||
		snapshot.ProductionIdentity.ProjectGenerationID != target.ProjectGenerationID ||
		snapshot.ProductionIdentity.VideoProductionBindingID != target.VideoBindingID ||
		snapshot.ProductionIdentity.VideoProductionBindingRevision != target.VideoBindingRevision ||
		snapshot.ProductionIdentity.CommerceWorkflowBindingID != target.CommerceBindingID ||
		snapshot.ProductionIdentity.CommerceWorkflowBindingRevision != target.CommerceBindingRevision ||
		snapshot.ProductionIdentity.CommerceConfigurationHash != target.CommerceConfigurationHash {
		t.Fatalf("prepared snapshot did not rewrite complete production identity: %+v", snapshot)
	}
}

func TestNormalizeScriptUnitRebuildTargetAcceptsUserSelectedDuration(t *testing.T) {
	target, err := normalizeScriptUnitRebuildTarget(ScriptUnitRebuildTarget{
		TargetSourceScriptVersionID: "source-version",
		TargetLanguageMode:          "auto",
		TargetDurationSeconds:       20,
		TargetPlatform:              "tiktok",
		TargetStoryboardStrategy:    StoryboardStrategySmart,
	}, ScriptUnit{Status: "active"})
	if err != nil {
		t.Fatalf("normalizeScriptUnitRebuildTarget() error = %v", err)
	}
	if target.TargetDurationSeconds != 20 {
		t.Fatalf("target duration = %d, want 20", target.TargetDurationSeconds)
	}
}
