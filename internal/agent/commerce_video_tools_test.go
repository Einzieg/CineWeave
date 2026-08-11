package agent

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/Einzieg/cineweave/internal/authz"
)

func TestCommerceVideoToolsExposeScriptDerivationLifecycle(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	expected := map[string]struct {
		maySpendProvider bool
		startsWorkflow   bool
	}{
		"commerce.script.revise":              {maySpendProvider: true},
		"commerce.script.derive.preview":      {maySpendProvider: true},
		"commerce.script.derive.batch":        {maySpendProvider: true, startsWorkflow: true},
		"commerce.script.derivation.get":      {},
		"commerce.script.derive.retry_failed": {maySpendProvider: true, startsWorkflow: true},
		"commerce.script.derive.cancel":       {},
		"commerce.attachment.assign":          {},
	}
	for name, want := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Errorf("missing derivation tool %s", name)
			continue
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("%s input schema is empty", name)
		}
		if tool.Effects.MaySpendProvider != want.maySpendProvider {
			t.Errorf("%s MaySpendProvider = %v, want %v", name, tool.Effects.MaySpendProvider, want.maySpendProvider)
		}
		if tool.StartsWorkflow != want.startsWorkflow {
			t.Errorf("%s StartsWorkflow = %v, want %v", name, tool.StartsWorkflow, want.startsWorkflow)
		}
	}
}

func TestCommerceVideoToolsExposeCompletePermissionContracts(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	expected := map[string][]string{
		"commerce.script.derive.batch":        {authz.PermissionScriptWrite, authz.PermissionWorkflowRun},
		"commerce.script.derive.retry_failed": {authz.PermissionScriptWrite, authz.PermissionWorkflowRun},
		"commerce.script.derive.cancel":       {authz.PermissionWorkflowCancel},
		"commerce.video.cancel":               {authz.PermissionWorkflowCancel},
	}
	for name, permissions := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("missing commerce tool %s", name)
		}
		if got := tool.RequiredPermissions(); !reflect.DeepEqual(got, permissions) {
			t.Errorf("%s permissions = %v, want %v", name, got, permissions)
		}
		if got := tool.Descriptor().Permissions; !reflect.DeepEqual(got, permissions) {
			t.Errorf("%s descriptor permissions = %v, want %v", name, got, permissions)
		}
	}
}

func TestCommerceAttachmentAssignmentIsWriteOnlyAndRequiresApproval(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	tool, ok := registry.Get("commerce.attachment.assign")
	if !ok {
		t.Fatal("commerce.attachment.assign missing")
	}
	effects := tool.EffectiveEffects()
	if !effects.WritesProject || effects.MaySpendProvider || effects.StartsWorkflow || effects.Destructive {
		t.Fatalf("attachment assignment effects = %+v", effects)
	}
	if !tool.RequiresApproval {
		t.Fatal("attachment assignment must require approval")
	}
}

func TestCommerceVideoToolsDoNotExposeLegacyStoryboardProduction(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	for _, name := range []string{
		"commerce.script_unit.storyboard.generate",
		"commerce.script_unit.reference_images.generate",
		"commerce.script_unit.video_prompts.generate",
		"commerce.script_unit.shot_videos.generate",
		"commerce.script_unit.final.compose",
	} {
		if _, ok := registry.Get(name); ok {
			t.Errorf("legacy tool %s must not be active for commerce video projects", name)
		}
	}
}

func TestCommerceScriptToolsAcceptStableOrdinalWithoutCopiedUUID(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	tests := []PlanStep{
		{
			Tool: "commerce.script.get",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7}`),
		},
		{
			Tool: "commerce.script.derive.preview",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7,"count":5,"dimension":"scene","instruction":"替换场景"}`),
		},
		{
			Tool: "commerce.script.revise",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7,"expectedRevision":3,"instruction":"压缩到当前视频模型限制内"}`),
		},
		{
			Tool: "commerce.video.generate",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7}`),
		},
	}
	for _, step := range tests {
		if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err != nil {
			t.Errorf("%s stable ordinal args rejected: %v", step.Tool, err)
		}
	}
}

func TestCommerceScriptCreateAcceptsSourceLanguageHint(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	step := PlanStep{
		Tool: "commerce.script.create",
		Args: json.RawMessage(`{
			"expectedScriptUnitsRevision":7,
			"title":"马来语头盔广告",
			"content":"Helmet ini ringan dan selesa.",
			"sourceLanguageHint":"ms-MY",
			"languageMode":"auto",
			"targetDurationSeconds":15,
			"targetPlatform":"tiktok"
		}`),
	}
	if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err != nil {
		t.Fatalf("commerce.script.create sourceLanguageHint rejected: %v", err)
	}
}

func TestCommerceVideoGenerateAcceptsAspectRatio(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	step := PlanStep{
		Tool: "commerce.video.generate",
		Args: json.RawMessage(`{
			"stableOrdinal":2,
			"expectedScriptUnitsRevision":7,
			"durationSeconds":16,
			"resolution":"720p",
			"aspectRatio":"9:16",
			"generateAudio":true
		}`),
	}
	if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err != nil {
		t.Fatalf("commerce.video.generate aspectRatio rejected: %v", err)
	}
}

func TestCommerceVideoListAcceptsStableOrdinalAndEffectiveFilters(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	step := PlanStep{
		Tool: "commerce.video.list",
		Args: json.RawMessage(`{
			"stableOrdinal":2,
			"expectedScriptUnitsRevision":7,
			"status":"running",
			"limit":20
		}`),
	}
	if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err != nil {
		t.Fatalf("commerce.video.list filters rejected: %v", err)
	}
	step.Args = json.RawMessage(`{"status":"unknown"}`)
	if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err == nil {
		t.Fatal("commerce.video.list accepted an unsupported status")
	}
}

func TestCommerceVideoToolInputSchemasMatchAuditedExecutorContracts(t *testing.T) {
	expected := map[string][]string{
		"commerce.project.read_summary":           {},
		"commerce.product.get":                    {},
		"commerce.product.versions.list":          {},
		"commerce.product.version.create":         {"brand", "expectedRevision", "immutableFeatures", "metadata", "name", "prohibitedClaims", "sellingPoints"},
		"commerce.product.rebuild_impact":         {"expectedProductRevision", "targetProductVersionId", "targetReferenceIds"},
		"commerce.product.rebuild":                {"expectedProductRevision", "impactToken"},
		"commerce.product.references.list":        {"status"},
		"commerce.product.reference.archive":      {"expectedRevision", "referenceId"},
		"commerce.product.reference.set_primary":  {"expectedRevision", "referenceId"},
		"commerce.product.reference.update":       {"expectedRevision", "ordinal", "referenceId", "referenceRole", "setPrimary"},
		"commerce.attachment.assign":              {"attachmentId", "expectedScriptUnitsRevision", "referenceRole", "scope", "scriptUnitId", "setPrimary", "stableOrdinal"},
		"commerce.product.update":                 {"brand", "expectedRevision", "immutableFeatures", "metadata", "name", "prohibitedClaims", "sellingPoints"},
		"commerce.script.list":                    {"cursor", "limit", "status"},
		"commerce.script.get":                     {"expectedScriptUnitsRevision", "scriptUnitId", "stableOrdinal"},
		"commerce.script.revise":                  {"expectedRevision", "expectedScriptUnitsRevision", "instruction", "preserve", "scriptUnitId", "stableOrdinal", "targetLengthUnit", "targetMaxLength"},
		"commerce.script.create":                  {"content", "expectedScriptUnitsRevision", "explicitTargetLanguage", "languageMode", "sourceLanguageHint", "targetDurationSeconds", "targetPlatform", "title"},
		"commerce.script.defaults.update":         {"expectedRevision", "languageMode", "targetDurationSeconds", "targetLanguage", "targetPlatform"},
		"commerce.script.duplicate":               {"expectedScriptUnitsRevision", "scriptUnitId", "stableOrdinal"},
		"commerce.script.create_language_variant": {"expectedScriptUnitsRevision", "scriptUnitId", "stableOrdinal", "targetLanguage"},
		"commerce.script.reorder":                 {"expectedScriptUnitsRevision", "items"},
		"commerce.script.rebuild_impact":          {"expectedRevision", "expectedScriptUnitsRevision", "scriptUnitId", "stableOrdinal", "targetDurationSeconds", "targetLanguage", "targetLanguageMode", "targetPlatform", "targetSourceScriptVersionId", "targetStoryboardStrategy"},
		"commerce.script.rebuild":                 {"expectedRevision", "expectedScriptUnitsRevision", "impactToken", "scriptUnitId", "stableOrdinal"},
		"commerce.script.reference.archive":       {"expectedRevision", "expectedScriptUnitsRevision", "referenceId", "scriptUnitId", "stableOrdinal"},
		"commerce.script.update":                  {"draftContent", "expectedRevision", "expectedScriptUnitsRevision", "explicitTargetLanguage", "languageMode", "scriptUnitId", "stableOrdinal", "targetDurationSeconds", "targetPlatform", "title"},
		"commerce.script.archive":                 {"expectedRevision", "expectedScriptUnitsRevision", "reason", "scriptUnitId", "stableOrdinal"},
		"commerce.script.derive.preview":          {"candidateValues", "count", "dimension", "expectedScriptUnitsRevision", "instruction", "preserve", "sourceScriptUnitId", "stableOrdinal"},
		"commerce.script.derive.batch":            {"dimension", "expectedScriptUnitsRevision", "instruction", "preserve", "sourceScriptUnitId", "stableOrdinal", "variations"},
		"commerce.script.derivation.get":          {"batchId", "include"},
		"commerce.script.derive.retry_failed":     {"batchId"},
		"commerce.script.derive.cancel":           {"batchId", "reason"},
		"commerce.video.options":                  {"expectedScriptUnitsRevision", "scriptUnitId", "stableOrdinal"},
		"commerce.video.list":                     {"expectedScriptUnitsRevision", "limit", "scriptUnitId", "stableOrdinal", "status"},
		"commerce.video.get":                      {"jobId"},
		"commerce.video.generate":                 {"aspectRatio", "durationSeconds", "expectedScriptUnitsRevision", "generateAudio", "references", "resolution", "scriptUnitId", "stableOrdinal"},
		"commerce.video.cancel":                   {"jobId", "reason"},
	}

	tools := CommerceVideoTools()
	if len(tools) != len(expected) {
		t.Fatalf("commerce tool count = %d, audited contracts = %d", len(tools), len(expected))
	}
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		want, exists := expected[tool.Name]
		if !exists {
			t.Errorf("commerce tool %s has no audited executor contract", tool.Name)
			continue
		}
		seen[tool.Name] = struct{}{}
		var schema struct {
			Properties           map[string]json.RawMessage `json:"properties"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("decode %s schema: %v", tool.Name, err)
			continue
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("%s must reject unaudited top-level arguments", tool.Name)
		}
		got := make([]string, 0, len(schema.Properties))
		for property := range schema.Properties {
			got = append(got, property)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s properties = %v, audited executor contract = %v", tool.Name, got, want)
		}
	}
	for name := range expected {
		if _, exists := seen[name]; !exists {
			t.Errorf("audited executor contract %s has no active tool", name)
		}
	}
}
