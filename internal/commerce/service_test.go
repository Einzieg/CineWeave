package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type recordingStore struct {
	template  WorkflowTemplateVersion
	err       error
	calls     []string
	project   DraftProjectParams
	projectID string
	setupID   string
}

func (store *recordingStore) ResolvePublishedWorkflowTemplate(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error) {
	store.calls = append(store.calls, "resolve-template")
	return store.template, store.err
}

func (store *recordingStore) ResolvePublishedWorkflowTemplateVersion(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error) {
	return WorkflowTemplateVersion{}, errors.New("unexpected call")
}

func (store *recordingStore) ResolveWorkflowTemplateVersionForRebuild(context.Context, pgx.Tx, string, string) (WorkflowTemplateVersion, error) {
	return WorkflowTemplateVersion{}, errors.New("unexpected call")
}

func (store *recordingStore) InsertDraftProject(_ context.Context, _ pgx.Tx, id string, params DraftProjectParams) error {
	store.calls = append(store.calls, "insert-project")
	store.projectID = id
	store.project = params
	return store.err
}

func (store *recordingStore) InsertProjectOwner(_ context.Context, _ pgx.Tx, _, projectID, _ string) error {
	store.calls = append(store.calls, "insert-owner")
	if projectID != store.projectID {
		return errors.New("project identity changed")
	}
	return store.err
}

func (store *recordingStore) InsertSetupSession(_ context.Context, _ pgx.Tx, id, projectID string, template WorkflowTemplateVersion, _ DraftProjectParams) error {
	store.calls = append(store.calls, "insert-setup")
	store.setupID = id
	if projectID != store.projectID || template.ID != store.template.ID {
		return errors.New("setup identity changed")
	}
	return store.err
}

func (store *recordingStore) CompleteDirectProjectSetup(
	context.Context,
	pgx.Tx,
	string,
	string,
	InitialBindingResult,
) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) InsertPreparingVideoBinding(context.Context, pgx.Tx, string, InitialBindingParams, string, []byte, string, int64) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) InsertPreparingCommerceBinding(context.Context, pgx.Tx, string, string, WorkflowTemplateVersion, InitialBindingParams, string, string, int64) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) InsertPreparingProjectGeneration(context.Context, pgx.Tx, string, string, string, InitialBindingParams, int64) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) NextBindingRevision(context.Context, pgx.Tx, string) (int64, error) {
	return 0, errors.New("unexpected call")
}

func (store *recordingStore) NextProjectGenerationNo(context.Context, pgx.Tx, string) (int64, error) {
	return 0, errors.New("unexpected call")
}

func (store *recordingStore) ActivateInitialBindings(context.Context, pgx.Tx, string, InitialBindingResult) error {
	store.calls = append(store.calls, "activate-bindings")
	return store.err
}

func (store *recordingStore) LockBindingPreparationProject(context.Context, pgx.Tx, string, string) (BindingPreparationState, error) {
	return BindingPreparationState{}, errors.New("unexpected call")
}

func (store *recordingStore) LockActiveProductionContext(context.Context, pgx.Tx, string, string) (ProductionContext, error) {
	return ProductionContext{}, errors.New("unexpected call")
}

func (store *recordingStore) LockUnitGenerationContext(context.Context, pgx.Tx, ProductionContext, UnitGenerationIdentity) (UnitGenerationContext, error) {
	return UnitGenerationContext{}, errors.New("unexpected call")
}

func (store *recordingStore) LockProjectRebuild(context.Context, pgx.Tx, string, string, string) (ProjectRebuildContext, error) {
	return ProjectRebuildContext{}, errors.New("unexpected call")
}

func (store *recordingStore) ListProjectRebuildUnitSeeds(context.Context, pgx.Tx, ProductionContext) ([]ProjectRebuildUnitSeed, error) {
	return nil, errors.New("unexpected call")
}

func (store *recordingStore) InsertPreparingProjectRebuildUnit(context.Context, pgx.Tx, string, InitialBindingResult, ProjectRebuildUnitTarget, string) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) AttachPreparedProjectRebuild(context.Context, pgx.Tx, ProjectRebuildContext, InitialBindingResult, int) error {
	return errors.New("unexpected call")
}

func (store *recordingStore) ActivatePreparedProjectRebuild(context.Context, pgx.Tx, ProjectRebuildContext, InitialBindingResult, videoproduction.ProductionConfigurationSnapshot, int) (ProjectRebuildActivationResult, error) {
	return ProjectRebuildActivationResult{}, errors.New("unexpected call")
}

func TestCreateDraftProjectCoordinatesRepositoryWrites(t *testing.T) {
	store := &recordingStore{template: WorkflowTemplateVersion{
		ID: "template-version", ContentHash: strings.Repeat("a", 64),
	}}
	service := NewService(store)
	result, err := service.CreateDraftProject(context.Background(), nil, DraftProjectParams{
		OrganizationID:   "organization",
		WorkspaceID:      "workspace",
		Name:             "  商品视频  ",
		VideoRatio:       "9:16",
		AudioStrategy:    "native_av",
		AudioRequirement: "preferred",
		ImageQuality:     "standard",
		TimelineTimebase: 90000,
		FPSNumerator:     24,
		FPSDenominator:   1,
		Settings:         json.RawMessage(`{"languageMode":"auto"}`),
		CreatedBy:        "user",
		IdempotencyScope: "commerce_project_create",
		ClientRequestID:  "request-1",
		RequestHash:      strings.Repeat("b", 64),
		InputSnapshot:    json.RawMessage(`{"name":"商品视频"}`),
		SetupExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateDraftProject() error = %v", err)
	}
	if got, want := strings.Join(store.calls, ","), "resolve-template,insert-project,insert-owner,insert-setup"; got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if store.project.Name != "商品视频" || store.project.WorkflowTemplateKey != DefaultWorkflowTemplateKey {
		t.Fatalf("normalized params = %+v", store.project)
	}
	if result.ProjectID == "" || result.ProjectID != store.projectID || result.SetupSessionID == "" || result.SetupSessionID != store.setupID {
		t.Fatalf("result identities = %+v", result)
	}
	if result.SetupState != "draft" || result.WorkflowTemplateVersionID != store.template.ID || result.SetupConfigurationHash != store.template.ContentHash {
		t.Fatalf("result = %+v", result)
	}
}

func TestCreateDraftProjectRejectsInvalidSnapshotBeforeRepositoryCall(t *testing.T) {
	store := &recordingStore{}
	_, err := NewService(store).CreateDraftProject(context.Background(), nil, DraftProjectParams{
		OrganizationID:   "organization",
		WorkspaceID:      "workspace",
		Name:             "商品视频",
		Settings:         json.RawMessage(`{}`),
		CreatedBy:        "user",
		IdempotencyScope: "commerce_project_create",
		ClientRequestID:  "request-1",
		RequestHash:      strings.Repeat("b", 64),
		InputSnapshot:    json.RawMessage(`[]`),
		SetupExpiresAt:   time.Now().Add(time.Hour),
	})
	if err == nil || !strings.Contains(err.Error(), "input snapshot") {
		t.Fatalf("error = %v, want invalid input snapshot", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("repository calls = %v, want none", store.calls)
	}
}

func TestHashJSONCanonicalizesObjectKeyOrder(t *testing.T) {
	left, err := hashJSON(json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("hash left: %v", err)
	}
	right, err := hashJSON(json.RawMessage(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("hash right: %v", err)
	}
	if left != right || !validSHA256(left) {
		t.Fatalf("hashes = %q and %q", left, right)
	}
}

func TestLocalizationTimingUsesStructuredVoiceoverChannels(t *testing.T) {
	sourceSegments := []ScriptSegment{
		{ID: "segment-1"},
		{ID: "segment-2"},
		{ID: "segment-3"},
	}
	localizedSegments := []string{
		"镜头一：白色随行杯桌面特写。旁白：热饮，随行不将就。",
		"镜头二：旋紧透明杯盖。旁白：双层真空保温，密封杯盖更省心。",
		"镜头三：随行杯放入通勤包。旁白：臻白随行杯，陪你从容出发。",
	}
	contract := json.RawMessage(`{
		"contractVersion":"commerce-script-localization/v1",
		"segments":[
			{"ordinal":1,"sourceSegmentId":"segment-1","localizedText":"镜头一：白色随行杯桌面特写。旁白：热饮，随行不将就。","voiceoverText":"热饮，随行不将就。"},
			{"ordinal":2,"sourceSegmentId":"segment-2","localizedText":"镜头二：旋紧透明杯盖。旁白：双层真空保温，密封杯盖更省心。","voiceoverText":"双层真空保温，密封杯盖更省心。"},
			{"ordinal":3,"sourceSegmentId":"segment-3","localizedText":"镜头三：随行杯放入通勤包。旁白：臻白随行杯，陪你从容出发。","voiceoverText":"臻白随行杯，陪你从容出发。"}
		]
	}`)
	segments, structured, err := localizationTimingSegments(LocalizationInput{
		LocalizedContent:   strings.Join(localizedSegments, "\n\n"),
		StructuredContract: contract,
	}, sourceSegments, localizedSegments)
	if err != nil {
		t.Fatalf("localizationTimingSegments() error = %v", err)
	}
	if !structured {
		t.Fatal("localizationTimingSegments() structured = false, want true")
	}
	timing, err := estimateStructuredScriptTiming(segments, "zh-CN", 15, LocalizationTimingPolicy{
		Version: "zh-cn-voiceover/v2", Unit: "han_character", NormalUnitsPerSecond: 3.5,
		CommaPauseSeconds: 0.15, SentencePauseSeconds: 0.35, SegmentGapSeconds: 0.10,
	})
	if err != nil {
		t.Fatalf("estimateStructuredScriptTiming() error = %v", err)
	}
	if timing.Units != 31 || timing.Exceeded || timing.EstimatedVoiceoverSeconds < 10.55 || timing.EstimatedVoiceoverSeconds > 10.57 {
		t.Fatalf("timing = %+v", timing)
	}
}

func TestStructuredLocalizationTimingIgnoresSilentSegmentsAndCoalescesPunctuation(t *testing.T) {
	timing, err := estimateStructuredScriptTiming([]string{
		"",
		"Helmet ini selesa...",
		"   ",
		"Memang berbaloi?!",
	}, "ms-MY", 2, LocalizationTimingPolicy{
		Version: "ms-my-voiceover/v1", Unit: "word", NormalUnitsPerSecond: 2.5,
		CommaPauseSeconds: 0.15, SentencePauseSeconds: 0.35, SegmentGapSeconds: 0.10,
	})
	if err != nil {
		t.Fatalf("estimateStructuredScriptTiming() error = %v", err)
	}
	// Five words at 2.5 words/second, two sentence pauses, and one spoken-segment gap.
	if timing.Units != 5 || timing.EstimatedVoiceoverSeconds < 2.79 || timing.EstimatedVoiceoverSeconds > 2.81 {
		t.Fatalf("timing = %+v", timing)
	}
	if !timing.Exceeded {
		t.Fatalf("timing.Exceeded = false, want advisory overrun")
	}
}

func TestLocalizationTimingRejectsMismatchedStructuredSegmentIdentity(t *testing.T) {
	_, _, err := localizationTimingSegments(LocalizationInput{
		StructuredContract: json.RawMessage(`{
			"contractVersion":"commerce-script-localization/v1",
			"segments":[{"ordinal":1,"sourceSegmentId":"other","localizedText":"旁白","voiceoverText":"旁白"}]
		}`),
	}, []ScriptSegment{{ID: "segment-1"}}, []string{"旁白"})
	if err == nil || !strings.Contains(err.Error(), "身份不一致") {
		t.Fatalf("error = %v, want segment identity mismatch", err)
	}
}

func TestLocalizationPersistencePreservesStructuredChannels(t *testing.T) {
	sourceSegments := []ScriptSegment{{ID: "segment-1"}}
	localizedSegments := []string{"镜头一：白色随行杯。旁白：热饮不将就。"}
	segments, err := localizationPersistenceSegments(LocalizationInput{
		StructuredContract: json.RawMessage(`{
			"contractVersion":"commerce-script-localization/v1",
			"segments":[{
				"ordinal":1,
				"sourceSegmentId":"segment-1",
				"salesBeat":"hook",
				"localizedText":"镜头一：白色随行杯。旁白：热饮不将就。",
				"voiceoverText":"热饮不将就。",
				"onscreenText":"通勤保温",
				"productClaims":["双层真空保温"],
				"requiredProductFeatures":["透明密封杯盖"]
			}]
		}`),
	}, sourceSegments, localizedSegments)
	if err != nil {
		t.Fatalf("localizationPersistenceSegments() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("segments = %+v", segments)
	}
	segment := segments[0]
	if segment.VoiceoverText != "热饮不将就。" || segment.OnscreenText != "通勤保温" ||
		segment.LocalizedText != localizedSegments[0] || segment.SalesBeat != "hook" ||
		len(segment.ProductClaims) != 1 || len(segment.RequiredProductFeatures) != 1 {
		t.Fatalf("structured channels were not preserved: %+v", segment)
	}
}

func TestLocalizationPersistenceNormalizesMissingStructuredArrays(t *testing.T) {
	sourceSegments := []ScriptSegment{{ID: "segment-1"}}
	localizedSegments := []string{"镜头一：白色随行杯。旁白：热饮不将就。"}
	segments, err := localizationPersistenceSegments(LocalizationInput{
		StructuredContract: json.RawMessage(`{
			"contractVersion":"commerce-script-localization/v1",
			"segments":[{
				"ordinal":1,
				"sourceSegmentId":"segment-1",
				"salesBeat":"hook",
				"localizedText":"镜头一：白色随行杯。旁白：热饮不将就。",
				"voiceoverText":"热饮不将就。"
			}]
		}`),
	}, sourceSegments, localizedSegments)
	if err != nil {
		t.Fatalf("localizationPersistenceSegments() error = %v", err)
	}
	if len(segments) != 1 || segments[0].ProductClaims == nil || segments[0].RequiredProductFeatures == nil {
		t.Fatalf("optional arrays must be persisted as JSON arrays: %+v", segments)
	}
}

func TestScriptUnitDurationAcceptsAnyPositiveWholeSecond(t *testing.T) {
	input := CreateScriptUnitInput{
		Title:                 "20 秒广告",
		LanguageMode:          "auto",
		TargetDurationSeconds: 20,
		TargetPlatform:        "tiktok",
	}
	if err := normalizeScriptUnitInput(&input); err != nil {
		t.Fatalf("normalizeScriptUnitInput() error = %v", err)
	}

	invalid := -1
	if err := validateScriptUnitUpdate(ScriptUnit{LanguageMode: "auto"}, &UpdateScriptUnitInput{
		TargetDurationSeconds: &invalid,
	}); err == nil {
		t.Fatal("validateScriptUnitUpdate() error = nil, want non-positive duration rejection")
	}
}
