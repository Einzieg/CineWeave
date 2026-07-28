package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

func (s *CatalogService) ListScriptUnits(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
	cursor string,
	limit int,
) (ScriptUnitList, error) {
	if status == "" || status == "active" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		return ScriptUnitList{}, errors.New("script unit status filter is invalid")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	cursorSort, cursorID, err := decodeScriptUnitCursor(cursor)
	if err != nil {
		return ScriptUnitList{}, err
	}
	items, err := s.repository.ListScriptUnits(ctx, db, organizationID, projectID, status, cursorSort, cursorID, limit+1)
	if err != nil {
		return ScriptUnitList{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	product, err := s.GetProduct(ctx, db, organizationID, projectID)
	if err != nil {
		return ScriptUnitList{}, err
	}
	result := ScriptUnitList{Items: items, HasMore: hasMore, ScriptUnitsRevision: product.ScriptUnitsRevision}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result.NextCursor = encodeScriptUnitCursor(last.SortOrder, last.ID)
	}
	return result, nil
}

func (s *CatalogService) GetScriptUnit(ctx context.Context, db rowQuerier, organizationID, projectID, unitID string) (ScriptUnit, error) {
	item, err := s.repository.LoadScriptUnit(ctx, db, organizationID, projectID, unitID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRequired, Message: "脚本单元不存在", Cause: err}
	}
	if err != nil {
		return ScriptUnit{}, err
	}
	if item.CurrentSourceVersionID != nil {
		version, versionErr := s.repository.LoadScriptVersion(ctx, db, organizationID, projectID, unitID, *item.CurrentSourceVersionID)
		if versionErr != nil {
			return ScriptUnit{}, versionErr
		}
		item.CurrentSourceVersion = &version
	}
	if resolution, resolutionErr := s.repository.LoadLatestLanguageResolution(ctx, db, organizationID, projectID, unitID); resolutionErr == nil {
		item.LanguageResolution = &resolution
	} else if !errors.Is(resolutionErr, pgx.ErrNoRows) {
		return ScriptUnit{}, resolutionErr
	}
	if item.CurrentLocalizationID != nil {
		localization, localizationErr := s.repository.LoadLocalization(ctx, db, organizationID, projectID, unitID, *item.CurrentLocalizationID)
		if localizationErr != nil {
			return ScriptUnit{}, localizationErr
		}
		item.CurrentLocalization = &localization
	}
	return item, nil
}

func (s *CatalogService) ResolveCurrentScriptContent(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	unitID string,
) (CurrentScriptContent, error) {
	item, err := s.repository.LoadScriptUnit(ctx, db, organizationID, projectID, unitID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentScriptContent{}, Error{Code: CodeScriptUnitRequired, Message: "脚本单元不存在", Cause: err}
	}
	if err != nil {
		return CurrentScriptContent{}, err
	}
	content := strings.TrimSpace(item.CurrentContent)
	if content == "" {
		return CurrentScriptContent{}, Error{
			Code: CodeScriptDerivationSourceEmpty, Message: "源广告脚本当前正文为空",
		}
	}
	contentHash := strings.TrimSpace(item.CurrentContentHash)
	if contentHash == "" {
		contentHash = hashText(content)
	}
	return CurrentScriptContent{
		ScriptUnitID: item.ID, Content: content, ContentHash: contentHash,
		SourceVersionID: item.CurrentSourceVersionID, UnitRevision: item.Revision,
	}, nil
}

func (s *CatalogService) CreateScriptUnit(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	createdBy string,
	expectedCollectionRevision int64,
	input CreateScriptUnitInput,
) (ScriptVersionMutation, error) {
	if err := normalizeScriptUnitInput(&input); err != nil {
		return ScriptVersionMutation{}, err
	}
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	if !found {
		return ScriptVersionMutation{}, Error{Code: CodeProductRequired, Message: "请先填写商品信息"}
	}
	if expectedCollectionRevision > 0 && product.ScriptUnitsRevision != expectedCollectionRevision {
		return ScriptVersionMutation{}, Error{Code: CodeScriptUnitRevision, Message: "脚本列表已变化，请刷新后重试"}
	}
	if input.DerivedFromScriptUnitID != nil {
		if _, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, *input.DerivedFromScriptUnitID, true); err != nil {
			return ScriptVersionMutation{}, Error{Code: CodeScriptUnitRequired, Message: "来源脚本不存在", Cause: err}
		}
	}
	unit, err := s.repository.InsertScriptUnit(ctx, tx, product, input, createdBy)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	if strings.TrimSpace(input.Content) == "" {
		return ScriptVersionMutation{ScriptUnit: unit}, nil
	}
	version, err := s.repository.InsertScriptVersion(ctx, tx, unit, input.Content, input.SourceLanguageHint, nil, true, createdBy)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	unit, err = s.repository.ActivateScriptVersion(ctx, tx, unit, version)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	return ScriptVersionMutation{ScriptUnit: unit, Version: version, Activated: true}, nil
}

func (s *CatalogService) UpdateScriptUnit(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	updatedBy string,
	expectedRevision int64,
	input UpdateScriptUnitInput,
) (ScriptUnit, error) {
	current, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRequired, Message: "脚本单元不存在", Cause: err}
	}
	if err != nil {
		return ScriptUnit{}, err
	}
	if current.Status == "archived" {
		return ScriptUnit{}, Error{Code: CodeScriptUnitArchived, Message: "已归档脚本不能修改"}
	}
	if current.Revision != expectedRevision {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已被其他操作修改，请刷新后重试"}
	}
	if err := validateScriptUnitUpdate(current, &input); err != nil {
		return ScriptUnit{}, err
	}
	contentChanged := input.DraftContent != nil && *input.DraftContent != current.CurrentContent
	productionChanged := contentChanged ||
		(input.LanguageMode != nil && *input.LanguageMode != current.LanguageMode) ||
		input.ExplicitTargetLanguage != nil ||
		(input.TargetDurationSeconds != nil && *input.TargetDurationSeconds != current.TargetDurationSeconds) ||
		(input.TargetPlatform != nil && strings.TrimSpace(*input.TargetPlatform) != current.TargetPlatform)
	if productionChanged && current.ActiveUnitGenerationID != nil && !contentChanged {
		return ScriptUnit{}, Error{Code: CodeScriptVersionStale, Message: "该脚本已有生产结果，请先确认单元重建影响"}
	}
	if contentChanged {
		version, versionErr := s.repository.InsertScriptVersion(
			ctx, tx, current, *input.DraftContent, nil, current.CurrentSourceVersionID, true, updatedBy,
		)
		if versionErr != nil {
			return ScriptUnit{}, versionErr
		}
		current, err = s.repository.ActivateScriptVersion(ctx, tx, current, version)
		if err != nil {
			return ScriptUnit{}, err
		}
		input.DraftContent = nil
		if !hasScriptUnitMetadataUpdate(input) {
			return current, nil
		}
	}
	return s.repository.UpdateScriptUnit(ctx, tx, current, input)
}

func hasScriptUnitMetadataUpdate(input UpdateScriptUnitInput) bool {
	return input.Title != nil || input.LanguageMode != nil ||
		input.ExplicitTargetLanguage != nil || input.TargetDurationSeconds != nil ||
		input.TargetPlatform != nil
}

func (s *CatalogService) ArchiveScriptUnit(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID string, expectedRevision int64) (ScriptUnit, error) {
	current, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptUnit{}, err
	}
	if current.Status == "archived" {
		return current, nil
	}
	if current.Revision != expectedRevision {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	return s.repository.ArchiveScriptUnit(ctx, tx, current)
}

func (s *CatalogService) ReorderScriptUnits(ctx context.Context, tx pgx.Tx, organizationID, projectID string, expectedCollectionRevision int64, items []ReorderScriptUnitItem) (int64, error) {
	product, found, err := s.repository.LockProduct(ctx, tx, organizationID, projectID)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	return s.repository.ReorderScriptUnits(ctx, tx, product, expectedCollectionRevision, items)
}

func (s *CatalogService) CreateScriptVersion(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, createdBy string, expectedRevision int64, content string, sourceLanguageHint *string, activate bool) (ScriptVersionMutation, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return ScriptVersionMutation{}, Error{Code: CodeScriptRequired, Message: "广告脚本不能为空"}
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	if unit.Status == "archived" {
		return ScriptVersionMutation{}, Error{Code: CodeScriptUnitArchived, Message: "已归档脚本不能创建版本"}
	}
	if unit.Revision != expectedRevision {
		return ScriptVersionMutation{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	version, err := s.repository.InsertScriptVersion(ctx, tx, unit, content, sourceLanguageHint, unit.CurrentSourceVersionID, true, createdBy)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	if !activate {
		return ScriptVersionMutation{ScriptUnit: unit, Version: version}, nil
	}
	unit, err = s.repository.ActivateScriptVersion(ctx, tx, unit, version)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	return ScriptVersionMutation{ScriptUnit: unit, Version: version, Activated: true}, nil
}

func (s *CatalogService) ListScriptVersions(ctx context.Context, db rowsQuerier, organizationID, projectID, unitID string) ([]ScriptVersion, error) {
	return s.repository.ListScriptVersions(ctx, db, organizationID, projectID, unitID)
}

func (s *CatalogService) GetScriptVersion(ctx context.Context, db rowQuerier, organizationID, projectID, unitID, versionID string) (ScriptVersion, error) {
	return s.repository.LoadScriptVersion(ctx, db, organizationID, projectID, unitID, versionID)
}

func (s *CatalogService) ActivateScriptVersion(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, versionID string, expectedRevision int64) (ScriptUnit, error) {
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptUnit{}, err
	}
	if unit.Revision != expectedRevision {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	version, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unitID, versionID)
	if err != nil {
		return ScriptUnit{}, err
	}
	return s.repository.ActivateScriptVersion(ctx, tx, unit, version)
}

func (s *CatalogService) DuplicateScriptUnit(ctx context.Context, tx pgx.Tx, organizationID, projectID, sourceUnitID, createdBy string, expectedCollectionRevision int64, languageVariant *string) (ScriptVersionMutation, error) {
	source, err := s.GetScriptUnit(ctx, tx, organizationID, projectID, sourceUnitID)
	if err != nil {
		return ScriptVersionMutation{}, err
	}
	if source.Status == "archived" {
		return ScriptVersionMutation{}, Error{Code: CodeScriptUnitArchived, Message: "已归档脚本不能复制"}
	}
	content := source.DraftContent
	if source.CurrentSourceVersion != nil {
		content = source.CurrentSourceVersion.Content
	}
	derivationKind := "copy"
	languageMode := source.LanguageMode
	targetLanguage := source.ExplicitTargetLanguage
	title := source.Title + " 副本"
	if languageVariant != nil {
		normalized, err := normalizeLocale(*languageVariant)
		if err != nil {
			return ScriptVersionMutation{}, err
		}
		languageVariant = &normalized
		languageMode = "explicit"
		targetLanguage = languageVariant
		derivationKind = "language_variant"
		title = source.Title + " · " + normalized
	}
	var sourceLanguageHint *string
	if source.CurrentSourceVersion != nil {
		sourceLanguageHint = source.CurrentSourceVersion.SourceLanguageHint
	}
	return s.CreateScriptUnit(ctx, tx, organizationID, projectID, createdBy, expectedCollectionRevision, CreateScriptUnitInput{
		Title: title, Content: content, LanguageMode: languageMode,
		ExplicitTargetLanguage: targetLanguage, TargetDurationSeconds: source.TargetDurationSeconds,
		TargetPlatform: source.TargetPlatform, SourceLanguageHint: sourceLanguageHint,
		DerivedFromScriptUnitID: &source.ID, DerivationKind: &derivationKind,
	})
}

func (s *CatalogService) GetLanguageResolution(ctx context.Context, db rowQuerier, organizationID, projectID, unitID string) (LanguageResolution, error) {
	item, err := s.repository.LoadLatestLanguageResolution(ctx, db, organizationID, projectID, unitID)
	if errors.Is(err, pgx.ErrNoRows) {
		return LanguageResolution{}, Error{Code: CodeLanguageRequired, Message: "当前脚本尚未解析语言", Cause: err}
	}
	return item, err
}

func (s *CatalogService) ResolveLanguage(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, actorID string) (LanguageResolution, error) {
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return LanguageResolution{}, err
	}
	if unit.CurrentSourceVersionID == nil {
		return LanguageResolution{}, Error{Code: CodeScriptRequired, Message: "请先保存广告脚本版本"}
	}
	version, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unitID, *unit.CurrentSourceVersionID)
	if err != nil {
		return LanguageResolution{}, err
	}
	inputHash := hashText(strings.Join([]string{version.ID, version.ContentHash, unit.LanguageMode, optionalString(unit.ExplicitTargetLanguage)}, ":"))
	if existing, existingErr := s.repository.LoadLatestLanguageResolution(ctx, tx, organizationID, projectID, unitID); existingErr == nil && existing.InputHash == inputHash {
		return existing, nil
	} else if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return LanguageResolution{}, existingErr
	}
	source, confidence, mixed := detectScriptLanguage(version.Content)
	if unit.LanguageMode == "explicit" {
		if unit.ExplicitTargetLanguage == nil {
			return LanguageResolution{}, Error{Code: CodeLanguageRequired, Message: "请选择目标语言"}
		}
		return s.repository.InsertLanguageResolution(ctx, tx, unit, version.ID, &source, unit.ExplicitTargetLanguage,
			&confidence, "用户明确指定目标语言", false, "confirmed", &actorID, inputHash)
	}
	_ = mixed
	reasoning := "根据脚本文字分布生成语言候选"
	return s.repository.InsertLanguageResolution(ctx, tx, unit, version.ID, &source, &source,
		&confidence, reasoning, false, "confirmed", &actorID, inputHash)
}

// RecordLanguageResolution persists a validated resolver result against the
// current immutable source script version. Replays with the same frozen input
// return the existing resolution instead of creating competing decisions.
func (s *CatalogService) RecordLanguageResolution(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	actorID string,
	sourceLanguage string,
	targetLanguage string,
	confidence float64,
	reasoning string,
	needsConfirmation bool,
	inputHash string,
) (LanguageResolution, error) {
	sourceLanguage, err := normalizeLocale(sourceLanguage)
	if err != nil {
		return LanguageResolution{}, err
	}
	targetLanguage, err = normalizeLocale(targetLanguage)
	if err != nil {
		return LanguageResolution{}, err
	}
	if confidence < 0 || confidence > 1 {
		return LanguageResolution{}, Error{Code: CodeLanguageUnsupported, Message: "语言判断置信度无效"}
	}
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return LanguageResolution{}, Error{Code: CodeLanguageRequired, Message: "语言判断依据不能为空"}
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return LanguageResolution{}, err
	}
	if unit.CurrentSourceVersionID == nil {
		return LanguageResolution{}, Error{Code: CodeScriptRequired, Message: "请先保存广告脚本版本"}
	}
	version, err := s.repository.LoadScriptVersion(ctx, tx, organizationID, projectID, unitID, *unit.CurrentSourceVersionID)
	if err != nil {
		return LanguageResolution{}, err
	}
	if unit.LanguageMode == "explicit" {
		if unit.ExplicitTargetLanguage == nil {
			return LanguageResolution{}, Error{Code: CodeLanguageRequired, Message: "请选择目标语言"}
		}
		expected, normalizeErr := normalizeLocale(*unit.ExplicitTargetLanguage)
		if normalizeErr != nil || expected != targetLanguage {
			return LanguageResolution{}, Error{Code: CodeLanguageConfirmation, Message: "语言解析结果不能覆盖用户指定的目标语言", Cause: normalizeErr}
		}
	}
	needsConfirmation = false
	inputHash = strings.TrimSpace(inputHash)
	if len(inputHash) != 64 {
		return LanguageResolution{}, Error{Code: CodeLanguageUnsupported, Message: "语言解析输入快照无效"}
	}
	if existing, existingErr := s.repository.LoadLatestLanguageResolution(ctx, tx, organizationID, projectID, unitID); existingErr == nil && existing.InputHash == inputHash {
		return existing, nil
	} else if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return LanguageResolution{}, existingErr
	}
	return s.repository.InsertLanguageResolution(
		ctx, tx, unit, version.ID, &sourceLanguage, &targetLanguage, &confidence,
		reasoning, false, "confirmed", &actorID, inputHash,
	)
}

func (s *CatalogService) ConfirmLanguage(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, resolutionID, locale, actorID string) (LanguageResolution, error) {
	locale, err := normalizeLocale(locale)
	if err != nil {
		return LanguageResolution{}, err
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return LanguageResolution{}, err
	}
	resolution, err := s.repository.LoadLatestLanguageResolution(ctx, tx, organizationID, projectID, unitID)
	if err != nil {
		return LanguageResolution{}, err
	}
	if resolution.ID != resolutionID || unit.CurrentSourceVersionID == nil || resolution.SourceScriptVersionID != *unit.CurrentSourceVersionID {
		return LanguageResolution{}, Error{Code: CodeScriptVersionStale, Message: "语言判断所依据的脚本已变化，请重新判断"}
	}
	if resolution.Status == "confirmed" {
		if resolution.TargetLanguage != nil && *resolution.TargetLanguage == locale {
			return resolution, nil
		}
		return LanguageResolution{}, Error{Code: CodeLanguageConfirmation, Message: "语言已经确认，不能覆盖原确认结果"}
	}
	return s.repository.ConfirmLanguageResolution(ctx, tx, organizationID, projectID, unitID, resolutionID, locale, actorID)
}

func (s *CatalogService) ListLocalizations(ctx context.Context, db rowsQuerier, organizationID, projectID, unitID string) ([]ScriptLocalization, error) {
	return s.repository.ListLocalizations(ctx, db, organizationID, projectID, unitID)
}

func (s *CatalogService) GetLocalization(ctx context.Context, db rowQuerier, organizationID, projectID, unitID, localizationID string) (ScriptLocalization, error) {
	return s.repository.LoadLocalization(ctx, db, organizationID, projectID, unitID, localizationID)
}

func (s *CatalogService) CreateLocalization(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, createdBy string, input LocalizationInput) (ScriptLocalization, TimingEstimate, error) {
	input.LocalizedContent = strings.TrimSpace(input.LocalizedContent)
	if input.LocalizedContent == "" {
		return ScriptLocalization{}, TimingEstimate{}, Error{Code: CodeScriptRequired, Message: "本地化脚本不能为空"}
	}
	sourceLocale, err := normalizeLocale(input.SourceLanguage)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	targetLocale, err := normalizeLocale(input.TargetLanguage)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	input.SourceLanguage = sourceLocale
	input.TargetLanguage = targetLocale
	if len(input.StructuredContract) == 0 {
		input.StructuredContract = []byte(`{}`)
	}
	if len(input.ReviewerOutput) == 0 {
		input.ReviewerOutput = []byte(`{}`)
	}
	if err := validateJSONObject(input.StructuredContract); err != nil {
		return ScriptLocalization{}, TimingEstimate{}, errors.New("localization structured contract must be an object")
	}
	if err := validateJSONObject(input.ReviewerOutput); err != nil {
		return ScriptLocalization{}, TimingEstimate{}, errors.New("localization reviewer output must be an object")
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	if unit.CurrentSourceVersionID == nil || *unit.CurrentSourceVersionID != input.SourceScriptVersionID {
		return ScriptLocalization{}, TimingEstimate{}, Error{Code: CodeScriptVersionStale, Message: "本地化所依据的脚本版本已变化"}
	}
	resolution, err := s.repository.LoadLatestLanguageResolution(ctx, tx, organizationID, projectID, unitID)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	if resolution.ID != input.LanguageResolutionID || resolution.Status != "confirmed" || resolution.TargetLanguage == nil || *resolution.TargetLanguage != targetLocale {
		return ScriptLocalization{}, TimingEstimate{}, Error{Code: CodeLanguageConfirmation, Message: "当前脚本的语言识别尚未完成"}
	}
	timingPolicy := AdvisoryLocalizationTimingPolicy(targetLocale)
	sourceSegments, err := s.repository.ListScriptSegments(ctx, tx, organizationID, projectID, unitID, input.SourceScriptVersionID)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	localizedSegments := splitScriptSegments(input.LocalizedContent)
	if len(sourceSegments) != len(localizedSegments) {
		return ScriptLocalization{}, TimingEstimate{}, Error{
			Code:    CodeScriptRequired,
			Message: "本地化脚本段落数必须与原脚本一致",
			Details: map[string]any{"sourceSegmentCount": len(sourceSegments), "localizedSegmentCount": len(localizedSegments)},
		}
	}
	timingSegments, structured, err := localizationTimingSegments(input, sourceSegments, localizedSegments)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	persistenceSegments, err := localizationPersistenceSegments(input, sourceSegments, localizedSegments)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	var timing TimingEstimate
	if structured {
		timing, err = estimateStructuredScriptTiming(timingSegments, targetLocale, unit.TargetDurationSeconds, timingPolicy)
	} else {
		timing, err = estimateScriptTiming(input.LocalizedContent, targetLocale, unit.TargetDurationSeconds, timingPolicy)
	}
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	localization, err := s.repository.InsertLocalization(ctx, tx, unit, resolution, input, timing, createdBy)
	if err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	if err := s.repository.InsertLocalizationSegments(ctx, tx, unit, localization, persistenceSegments); err != nil {
		return ScriptLocalization{}, TimingEstimate{}, err
	}
	if input.Approve && unit.ActiveUnitGenerationID == nil {
		if _, err := s.repository.ActivateLocalization(ctx, tx, unit, localization); err != nil {
			return ScriptLocalization{}, TimingEstimate{}, err
		}
	}
	return localization, timing, nil
}

func (s *CatalogService) ActivateLocalization(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, localizationID string, expectedRevision int64) (ScriptUnit, error) {
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if err != nil {
		return ScriptUnit{}, err
	}
	if unit.Revision != expectedRevision {
		return ScriptUnit{}, Error{Code: CodeScriptUnitRevision, Message: "脚本已变化，请刷新后重试"}
	}
	if unit.ActiveUnitGenerationID != nil {
		return ScriptUnit{}, Error{Code: CodeScriptVersionStale, Message: "当前脚本已有生产结果，请先确认单元重建影响"}
	}
	localization, err := s.repository.LoadLocalization(ctx, tx, organizationID, projectID, unitID, localizationID)
	if err != nil {
		return ScriptUnit{}, err
	}
	if localization.Status != "approved" || unit.CurrentSourceVersionID == nil || localization.SourceScriptVersionID != *unit.CurrentSourceVersionID {
		return ScriptUnit{}, Error{Code: CodeLanguageConfirmation, Message: "只能激活已审核且基于当前脚本的本地化版本"}
	}
	return s.repository.ActivateLocalization(ctx, tx, unit, localization)
}

func detectScriptLanguage(content string) (string, float64, bool) {
	var han, kana, hangul, latin, total int
	for _, current := range content {
		if unicode.IsSpace(current) || unicode.IsPunct(current) || unicode.IsDigit(current) {
			continue
		}
		total++
		switch {
		case unicode.In(current, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.In(current, unicode.Hangul):
			hangul++
		case unicode.Is(unicode.Han, current):
			han++
		case unicode.Is(unicode.Latin, current):
			latin++
		}
	}
	if total == 0 {
		return "zh-CN", 0, true
	}
	type candidate struct {
		locale string
		count  int
	}
	candidates := []candidate{{"ja-JP", kana}, {"ko-KR", hangul}, {"zh-CN", han}, {"en-US", latin}}
	best := candidates[0]
	second := 0
	for _, item := range candidates[1:] {
		if item.count > best.count {
			second = best.count
			best = item
		} else if item.count > second {
			second = item.count
		}
	}
	confidence := float64(best.count) / float64(total)
	mixed := second > 0 && float64(second)/float64(total) >= 0.2
	return best.locale, confidence, mixed
}

func normalizeScriptUnitInput(input *CreateScriptUnitInput) error {
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	input.LanguageMode = strings.TrimSpace(input.LanguageMode)
	input.TargetPlatform = strings.TrimSpace(input.TargetPlatform)
	if input.Title == "" {
		return Error{Code: CodeScriptUnitRequired, Message: "脚本标题不能为空"}
	}
	if input.LanguageMode == "" {
		input.LanguageMode = "auto"
	}
	if input.TargetDurationSeconds == 0 {
		input.TargetDurationSeconds = 30
	}
	if input.TargetPlatform == "" {
		input.TargetPlatform = "generic"
	}
	if input.TargetDurationSeconds <= 0 {
		return Error{Code: CodeDurationExceeded, Message: "目标时长必须为正整数秒"}
	}
	if input.LanguageMode != "auto" && input.LanguageMode != "explicit" {
		return Error{Code: CodeLanguageRequired, Message: "语言模式必须为自动判断或明确指定"}
	}
	if input.LanguageMode == "explicit" {
		if input.ExplicitTargetLanguage == nil {
			return Error{Code: CodeLanguageRequired, Message: "请选择目标语言"}
		}
		value, err := normalizeLocale(*input.ExplicitTargetLanguage)
		if err != nil {
			return err
		}
		input.ExplicitTargetLanguage = &value
	} else {
		input.ExplicitTargetLanguage = nil
	}
	if input.DerivationKind != nil {
		value := strings.TrimSpace(*input.DerivationKind)
		switch value {
		case "copy", "language_variant", "agent_idea",
			"scene_variant", "hook_variant", "audience_variant", "tone_variant",
			"cta_variant", "custom_variant":
		default:
			return errors.New("script unit derivation kind is invalid")
		}
		input.DerivationKind = &value
	}
	return nil
}

func validateScriptUnitUpdate(current ScriptUnit, input *UpdateScriptUnitInput) error {
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" {
			return Error{Code: CodeScriptUnitRequired, Message: "脚本标题不能为空"}
		}
		input.Title = &value
	}
	if input.DraftContent != nil {
		value := strings.TrimSpace(*input.DraftContent)
		if value == "" {
			return Error{Code: CodeScriptRequired, Message: "广告脚本不能为空"}
		}
		input.DraftContent = &value
	}
	mode := current.LanguageMode
	if input.LanguageMode != nil {
		mode = strings.TrimSpace(*input.LanguageMode)
		if mode != "auto" && mode != "explicit" {
			return Error{Code: CodeLanguageRequired, Message: "语言模式无效"}
		}
		input.LanguageMode = &mode
	}
	if mode == "explicit" {
		target := current.ExplicitTargetLanguage
		if input.ExplicitTargetLanguage != nil {
			target = input.ExplicitTargetLanguage
		}
		if target == nil {
			return Error{Code: CodeLanguageRequired, Message: "请选择目标语言"}
		}
		value, err := normalizeLocale(*target)
		if err != nil {
			return err
		}
		input.ExplicitTargetLanguage = &value
	} else if input.LanguageMode != nil {
		input.ExplicitTargetLanguage = nil
	}
	if input.TargetDurationSeconds != nil && *input.TargetDurationSeconds <= 0 {
		return Error{Code: CodeDurationExceeded, Message: "目标时长必须为正整数秒"}
	}
	if input.TargetPlatform != nil {
		value := strings.TrimSpace(*input.TargetPlatform)
		if value == "" {
			return errors.New("target platform is required")
		}
		input.TargetPlatform = &value
	}
	return nil
}

func normalizeLocale(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !localePattern.MatchString(value) {
		return "", Error{Code: CodeLanguageUnsupported, Message: "语言标识不是有效的 BCP 47 格式"}
	}
	parts := strings.Split(value, "-")
	parts[0] = strings.ToLower(parts[0])
	for index := 1; index < len(parts); index++ {
		if len(parts[index]) == 2 || len(parts[index]) == 3 && allASCII(parts[index]) {
			parts[index] = strings.ToUpper(parts[index])
		}
	}
	return strings.Join(parts, "-"), nil
}

// NormalizeLocale canonicalizes a public BCP 47 locale before it enters a
// durable workflow signal or immutable commerce snapshot.
func NormalizeLocale(value string) (string, error) {
	return normalizeLocale(value)
}

func allASCII(value string) bool {
	for _, current := range value {
		if current > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func splitScriptSegments(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	parts := strings.FieldsFunc(content, func(r rune) bool { return r == '\n' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 && strings.TrimSpace(content) != "" {
		result = append(result, strings.TrimSpace(content))
	}
	return result
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func encodeScriptUnitCursor(sortOrder int64, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%s", sortOrder, id)))
}

func decodeScriptUnitCursor(cursor string) (int64, string, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", errors.New("script unit cursor is invalid")
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", errors.New("script unit cursor is invalid")
	}
	sortOrder, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || sortOrder <= 0 {
		return 0, "", errors.New("script unit cursor is invalid")
	}
	return sortOrder, parts[1], nil
}

func estimateScriptTiming(
	content string,
	locale string,
	targetDuration int,
	policy LocalizationTimingPolicy,
) (TimingEstimate, error) {
	return estimateLocalizationTiming([]string{content}, locale, targetDuration, policy)
}

const CommerceAdvisoryTimingVersion = "commerce-advisory-timing/v1"

// AdvisoryLocalizationTimingPolicy estimates pacing for UI guidance and
// storyboard allocation. It is deliberately derived from the resolved locale
// at runtime: templates do not publish, freeze, approve, or restrict it.
func AdvisoryLocalizationTimingPolicy(locale string) LocalizationTimingPolicy {
	language := strings.ToLower(strings.Split(strings.TrimSpace(locale), "-")[0])
	policy := LocalizationTimingPolicy{
		Version: CommerceAdvisoryTimingVersion,
		Unit:    "word", NormalUnitsPerSecond: 2.5,
		CommaPauseSeconds: 0.12, SentencePauseSeconds: 0.22,
		SegmentGapSeconds: 0.08, AllowedOverrunSeconds: 0,
	}
	switch language {
	case "zh":
		policy.Unit = "han_character"
		policy.NormalUnitsPerSecond = 3.5
	case "ja":
		policy.Unit = "mora"
		policy.NormalUnitsPerSecond = 5
	case "ko":
		policy.Unit = "syllable"
		policy.NormalUnitsPerSecond = 4
	}
	return policy
}

const commerceScriptLocalizationContractVersion = "commerce-script-localization/v1"

type commerceLocalizationTimingContract struct {
	ContractVersion string                                      `json:"contractVersion"`
	Segments        []commerceLocalizationTimingContractSegment `json:"segments"`
}

type commerceLocalizationTimingContractSegment struct {
	Ordinal                 int      `json:"ordinal"`
	SourceSegmentID         string   `json:"sourceSegmentId"`
	SalesBeat               string   `json:"salesBeat"`
	LocalizedText           string   `json:"localizedText"`
	VoiceoverText           string   `json:"voiceoverText"`
	OnscreenText            string   `json:"onscreenText"`
	ProductClaims           []string `json:"productClaims"`
	RequiredProductFeatures []string `json:"requiredProductFeatures"`
}

type localizationSegmentInput struct {
	SourceSegmentID         string
	SegmentNo               int
	SalesBeat               string
	LocalizedText           string
	VoiceoverText           string
	OnscreenText            string
	ProductClaims           []string
	RequiredProductFeatures []string
}

func localizationTimingSegments(
	input LocalizationInput,
	sourceSegments []ScriptSegment,
	localizedSegments []string,
) ([]string, bool, error) {
	segments, structured, err := parseStructuredLocalizationSegments(input, sourceSegments, localizedSegments)
	if err != nil {
		return nil, false, err
	}
	if !structured {
		return localizedSegments, false, nil
	}
	voiceover := make([]string, 0, len(segments))
	for _, segment := range segments {
		voiceover = append(voiceover, strings.TrimSpace(segment.VoiceoverText))
	}
	return voiceover, true, nil
}

func localizationPersistenceSegments(
	input LocalizationInput,
	sourceSegments []ScriptSegment,
	localizedSegments []string,
) ([]localizationSegmentInput, error) {
	segments, structured, err := parseStructuredLocalizationSegments(input, sourceSegments, localizedSegments)
	if err != nil {
		return nil, err
	}
	result := make([]localizationSegmentInput, 0, len(sourceSegments))
	if !structured {
		for index, source := range sourceSegments {
			localized := strings.TrimSpace(localizedSegments[index])
			result = append(result, localizationSegmentInput{
				SourceSegmentID: source.ID, SegmentNo: index + 1, SalesBeat: "script",
				LocalizedText: localized, VoiceoverText: localized,
				ProductClaims: []string{}, RequiredProductFeatures: []string{},
			})
		}
		return result, nil
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment.SalesBeat) == "" {
			return nil, Error{Code: CodeScriptRequired, Message: fmt.Sprintf("本地化结构化契约第 %d 段缺少 salesBeat", segment.Ordinal)}
		}
		result = append(result, localizationSegmentInput{
			SourceSegmentID: segment.SourceSegmentID, SegmentNo: segment.Ordinal,
			SalesBeat: strings.TrimSpace(segment.SalesBeat), LocalizedText: strings.TrimSpace(segment.LocalizedText),
			VoiceoverText: strings.TrimSpace(segment.VoiceoverText), OnscreenText: strings.TrimSpace(segment.OnscreenText),
			ProductClaims:           normalizedStringArray(segment.ProductClaims),
			RequiredProductFeatures: normalizedStringArray(segment.RequiredProductFeatures),
		})
	}
	return result, nil
}

func normalizedStringArray(values []string) []string {
	result := normalizedStrings(values, nil)
	if result == nil {
		return []string{}
	}
	return result
}

func parseStructuredLocalizationSegments(
	input LocalizationInput,
	sourceSegments []ScriptSegment,
	localizedSegments []string,
) ([]commerceLocalizationTimingContractSegment, bool, error) {
	var contract commerceLocalizationTimingContract
	if len(input.StructuredContract) == 0 || string(input.StructuredContract) == "{}" {
		return nil, false, nil
	}
	if err := json.Unmarshal(input.StructuredContract, &contract); err != nil {
		return nil, false, Error{Code: CodeScriptRequired, Message: "本地化结构化契约无法解析"}
	}
	if contract.ContractVersion != commerceScriptLocalizationContractVersion {
		return nil, false, nil
	}
	if len(contract.Segments) != len(sourceSegments) || len(contract.Segments) != len(localizedSegments) {
		return nil, false, Error{Code: CodeScriptRequired, Message: "本地化结构化契约与脚本段落数量不一致"}
	}
	for index, segment := range contract.Segments {
		if segment.Ordinal != index+1 || segment.SourceSegmentID != sourceSegments[index].ID {
			return nil, false, Error{Code: CodeScriptRequired, Message: fmt.Sprintf("本地化结构化契约第 %d 段身份不一致", index+1)}
		}
		if strings.TrimSpace(segment.LocalizedText) != strings.TrimSpace(localizedSegments[index]) {
			return nil, false, Error{Code: CodeScriptRequired, Message: fmt.Sprintf("本地化结构化契约第 %d 段正文不一致", index+1)}
		}
	}
	return contract.Segments, true, nil
}

func estimateStructuredScriptTiming(
	segments []string,
	locale string,
	targetDuration int,
	policy LocalizationTimingPolicy,
) (TimingEstimate, error) {
	return estimateLocalizationTiming(segments, locale, targetDuration, policy)
}

func estimateLocalizationTiming(
	segments []string,
	locale string,
	targetDuration int,
	policy LocalizationTimingPolicy,
) (TimingEstimate, error) {
	locale, err := normalizeLocale(locale)
	if err != nil {
		return TimingEstimate{}, err
	}
	if err := validateLocalizationTimingPolicy(policy); err != nil {
		return TimingEstimate{}, err
	}
	units := 0
	pauseSeconds := 0.0
	spokenSegments := 0
	for _, content := range segments {
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		if spokenSegments > 0 {
			pauseSeconds += policy.SegmentGapSeconds
		}
		spokenSegments++
		currentUnits, countErr := localizationTimingUnits(content, policy.Unit)
		if countErr != nil {
			return TimingEstimate{}, countErr
		}
		units += currentUnits
		pauseSeconds += localizationPunctuationPauseSeconds(content, policy)
	}
	seconds := float64(units)/policy.NormalUnitsPerSecond + pauseSeconds
	return TimingEstimate{
		Locale: locale, PolicyVersion: policy.Version, Units: units, UnitsPerSecond: policy.NormalUnitsPerSecond,
		EstimatedVoiceoverSeconds: seconds, TargetDurationSeconds: targetDuration,
		Exceeded: seconds > float64(targetDuration),
	}, nil
}

func validateLocalizationTimingPolicy(policy LocalizationTimingPolicy) error {
	if strings.TrimSpace(policy.Version) == "" ||
		strings.TrimSpace(policy.Unit) == "" ||
		policy.NormalUnitsPerSecond <= 0 ||
		policy.CommaPauseSeconds < 0 ||
		policy.SentencePauseSeconds < 0 ||
		policy.SegmentGapSeconds < 0 ||
		policy.AllowedOverrunSeconds < 0 {
		return errors.New("localization timing policy is invalid")
	}
	switch policy.Unit {
	case "han_character", "character", "mora", "syllable", "word":
		return nil
	default:
		return fmt.Errorf("unsupported timing unit %q", policy.Unit)
	}
}

func localizationTimingUnits(content, unit string) (int, error) {
	units := 0
	switch unit {
	case "han_character", "character", "syllable":
		for _, current := range content {
			if unicode.IsLetter(current) || unicode.IsDigit(current) || unicode.Is(unicode.Han, current) ||
				unicode.In(current, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
				units++
			}
		}
	case "mora":
		for _, current := range content {
			if !unicode.IsSpace(current) && !unicode.IsPunct(current) {
				units++
			}
		}
	case "word":
		units = len(strings.Fields(content))
	default:
		return 0, fmt.Errorf("unsupported timing unit %q", unit)
	}
	return units, nil
}

func localizationPunctuationPauseSeconds(content string, policy LocalizationTimingPolicy) float64 {
	pauseSeconds := 0.0
	clusterPauseSeconds := 0.0
	for _, current := range content {
		currentPauseSeconds := localizationPunctuationPause(current, policy)
		if currentPauseSeconds > 0 {
			clusterPauseSeconds = max(clusterPauseSeconds, currentPauseSeconds)
			continue
		}
		pauseSeconds += clusterPauseSeconds
		clusterPauseSeconds = 0
	}
	return pauseSeconds + clusterPauseSeconds
}

func localizationPunctuationPause(current rune, policy LocalizationTimingPolicy) float64 {
	switch current {
	case ',', '，', '、', ';', '；', ':', '：':
		return policy.CommaPauseSeconds
	case '.', '。', '!', '！', '?', '？':
		return policy.SentencePauseSeconds
	default:
		return 0
	}
}

func utf8Len(value string) int { return utf8.RuneCountInString(value) }
