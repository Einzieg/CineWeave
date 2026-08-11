package commerce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

const (
	CommerceDirectVideoContractV1 = "commerce-direct-video/v1"
	CodeDirectVideoUnavailable    = "COMMERCE_DIRECT_VIDEO_UNAVAILABLE"
	CodeDirectVideoInvalid        = "COMMERCE_DIRECT_VIDEO_INVALID"
	CodeDirectVideoNotFound       = "COMMERCE_DIRECT_VIDEO_NOT_FOUND"
	CodeDirectVideoStateConflict  = "COMMERCE_DIRECT_VIDEO_STATE_CONFLICT"
)

type DirectVideoInputSlot struct {
	Role      string `json:"role"`
	MediaType string `json:"mediaType"`
	Semantics string `json:"semantics"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Ordered   bool   `json:"ordered"`
}

type DirectVideoInputContract struct {
	ContractKey            string                 `json:"contractKey"`
	RequestMode            string                 `json:"requestMode"`
	Slots                  []DirectVideoInputSlot `json:"slots"`
	MutuallyExclusiveRoles [][]string             `json:"mutuallyExclusiveRoles,omitempty"`
}

type DirectVideoNativeAudio struct {
	Support           string `json:"support"`
	SupportsDialogue  *bool  `json:"supportsDialogue,omitempty"`
	SupportsVoiceover *bool  `json:"supportsVoiceover,omitempty"`
}

type DirectVideoPromptConstraint struct {
	MaxLength int    `json:"maxLength"`
	Unit      string `json:"unit"`
}

type DirectVideoRoute struct {
	RouteKey                  string                      `json:"routeKey"`
	ModelProfileID            string                      `json:"modelProfileId"`
	ModelProfileKey           string                      `json:"modelProfileKey"`
	ModelProfileBindingID     string                      `json:"modelProfileBindingId"`
	ProviderModelID           string                      `json:"providerModelId"`
	ProviderAccountID         string                      `json:"providerAccountId"`
	ProviderModelKey          string                      `json:"providerModelKey"`
	Priority                  int                         `json:"priority"`
	Weight                    int                         `json:"weight"`
	VariantKey                string                      `json:"variantKey"`
	CapabilitySnapshotHash    string                      `json:"capabilitySnapshotHash"`
	ExecutableDurationSeconds []int                       `json:"executableDurationSeconds"`
	Resolutions               []string                    `json:"resolutions"`
	AspectRatios              []string                    `json:"aspectRatios"`
	PromptConstraint          DirectVideoPromptConstraint `json:"promptConstraint"`
	InputContract             DirectVideoInputContract    `json:"inputContract"`
	NativeAudio               DirectVideoNativeAudio      `json:"nativeAudio"`
}

type DirectVideoOptions struct {
	ContractVersion                    string                      `json:"contractVersion"`
	ProjectProductionGenerationID      string                      `json:"projectProductionGenerationId"`
	VideoProductionBindingID           string                      `json:"videoProductionBindingId"`
	VideoProductionBindingRevision     int64                       `json:"videoProductionBindingRevision"`
	VideoProductionProfileVersionID    string                      `json:"videoProductionProfileVersionId"`
	VideoProductionProfileSnapshotHash string                      `json:"videoProductionProfileSnapshotHash"`
	DefaultAspectRatio                 string                      `json:"defaultAspectRatio"`
	DefaultResolution                  string                      `json:"defaultResolution"`
	DefaultDurationSeconds             int                         `json:"defaultDurationSeconds"`
	ScriptPromptConstraint             DirectVideoPromptConstraint `json:"scriptPromptConstraint"`
	ExecutableDurationSeconds          []int                       `json:"executableDurationSeconds"`
	Resolutions                        []string                    `json:"resolutions"`
	AspectRatios                       []string                    `json:"aspectRatios"`
	Routes                             []DirectVideoRoute          `json:"routes"`
}

type ScriptReferenceImage struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organizationId"`
	ProjectID        string     `json:"projectId"`
	ProductID        string     `json:"productId"`
	ScriptUnitID     string     `json:"scriptUnitId"`
	ArtifactID       string     `json:"artifactId"`
	MediaFileID      string     `json:"mediaFileId"`
	StorageKey       string     `json:"-"`
	OriginalFileName string     `json:"fileName"`
	MimeType         string     `json:"mimeType"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
	ByteSize         int64      `json:"byteSize"`
	ContentHash      string     `json:"contentHash"`
	Status           string     `json:"status"`
	Revision         int64      `json:"revision"`
	PreviewURL       string     `json:"previewUrl,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	ArchivedAt       *time.Time `json:"archivedAt,omitempty"`
}

type ScriptReferenceUpload struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	ProjectID         string     `json:"projectId"`
	ProductID         string     `json:"productId"`
	ScriptUnitID      string     `json:"scriptUnitId"`
	StorageKey        string     `json:"-"`
	RequestedMimeType string     `json:"mimeType"`
	OriginalFileName  string     `json:"fileName"`
	Status            string     `json:"status"`
	IdempotencyKey    string     `json:"-"`
	ReferenceImageID  *string    `json:"referenceImageId,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	AbandonedAt       *time.Time `json:"abandonedAt,omitempty"`
}

type DirectVideoReferenceSelection struct {
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
}

type DirectVideoReferenceSnapshot struct {
	ID                     string          `json:"id"`
	SourceType             string          `json:"sourceType"`
	SourceID               string          `json:"sourceId"`
	ProductReferenceID     *string         `json:"productReferenceId,omitempty"`
	ScriptReferenceImageID *string         `json:"scriptReferenceImageId,omitempty"`
	ArtifactID             string          `json:"artifactId"`
	MediaFileID            string          `json:"mediaFileId"`
	StorageKey             string          `json:"storageKey"`
	MimeType               string          `json:"mimeType"`
	ReferenceRole          string          `json:"referenceRole"`
	Ordinal                int             `json:"ordinal"`
	ContentHash            string          `json:"contentHash"`
	SourceRevision         int64           `json:"sourceRevision"`
	Snapshot               json.RawMessage `json:"snapshot"`
	PreviewURL             string          `json:"previewUrl,omitempty"`
}

type DirectVideoJob struct {
	ID                             string                         `json:"id"`
	OrganizationID                 string                         `json:"organizationId"`
	ProjectID                      string                         `json:"projectId"`
	ProductID                      string                         `json:"productId"`
	ProductVersionID               string                         `json:"productVersionId"`
	ScriptUnitID                   string                         `json:"scriptUnitId"`
	ScriptUnitRevision             int64                          `json:"scriptUnitRevision"`
	ProjectProductionGenerationID  string                         `json:"projectProductionGenerationId"`
	VideoProductionBindingID       string                         `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64                          `json:"videoProductionBindingRevision"`
	VideoProfileVersionID          string                         `json:"videoProfileVersionId"`
	VideoProfileSnapshotHash       string                         `json:"videoProfileSnapshotHash"`
	ModelProfileKey                string                         `json:"modelProfileKey"`
	ModelProfileID                 *string                        `json:"modelProfileId,omitempty"`
	ModelProfileBindingID          *string                        `json:"modelProfileBindingId,omitempty"`
	ProviderModelID                *string                        `json:"providerModelId,omitempty"`
	ProviderAccountID              *string                        `json:"providerAccountId,omitempty"`
	ProviderModelKey               string                         `json:"providerModelKey"`
	RouteKey                       string                         `json:"routeKey"`
	VariantKey                     string                         `json:"variantKey"`
	CapabilitySnapshotHash         string                         `json:"capabilitySnapshotHash"`
	RequestedDurationSeconds       int                            `json:"requestedDurationSeconds"`
	AspectRatio                    string                         `json:"aspectRatio"`
	Resolution                     string                         `json:"resolution"`
	GenerateAudio                  bool                           `json:"generateAudio"`
	ScriptSnapshot                 string                         `json:"scriptSnapshot"`
	ScriptHash                     string                         `json:"scriptHash"`
	ProductSnapshot                json.RawMessage                `json:"productSnapshot"`
	ProductSnapshotHash            string                         `json:"productSnapshotHash"`
	ExecutionContract              json.RawMessage                `json:"executionContract"`
	ExecutionContractHash          string                         `json:"executionContractHash"`
	ReferenceSetHash               string                         `json:"referenceSetHash"`
	PromptHash                     string                         `json:"promptHash"`
	Status                         string                         `json:"status"`
	AttemptGeneration              int                            `json:"attemptGeneration"`
	WorkflowRunID                  *string                        `json:"workflowRunId,omitempty"`
	NodeRunID                      *string                        `json:"nodeRunId,omitempty"`
	ProviderRequestID              *string                        `json:"providerRequestId,omitempty"`
	ProviderCallID                 *string                        `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID            *string                        `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID                 *string                        `json:"externalTaskId,omitempty"`
	OutputArtifactID               *string                        `json:"outputArtifactId,omitempty"`
	OutputMediaFileID              *string                        `json:"outputMediaFileId,omitempty"`
	OutputStorageKey               *string                        `json:"outputStorageKey,omitempty"`
	OutputMimeType                 *string                        `json:"outputMimeType,omitempty"`
	OutputPreviewURL               string                         `json:"outputPreviewUrl,omitempty"`
	OutputWarnings                 json.RawMessage                `json:"outputWarnings"`
	ErrorCode                      *string                        `json:"errorCode,omitempty"`
	ErrorMessage                   *string                        `json:"errorMessage,omitempty"`
	CreatedBy                      *string                        `json:"createdBy,omitempty"`
	CreatedAt                      time.Time                      `json:"createdAt"`
	StartedAt                      *time.Time                     `json:"startedAt,omitempty"`
	CompletedAt                    *time.Time                     `json:"completedAt,omitempty"`
	CancelledAt                    *time.Time                     `json:"cancelledAt,omitempty"`
	UpdatedAt                      time.Time                      `json:"updatedAt"`
	References                     []DirectVideoReferenceSnapshot `json:"references"`
}

type CreateDirectVideoJobInput struct {
	DurationSeconds int                             `json:"durationSeconds"`
	Resolution      string                          `json:"resolution"`
	AspectRatio     string                          `json:"aspectRatio"`
	GenerateAudio   *bool                           `json:"generateAudio,omitempty"`
	References      []DirectVideoReferenceSelection `json:"references,omitempty"`
}

type DirectVideoJobListFilter struct {
	ScriptUnitID string
	Status       string
	Limit        int
}

func (filter DirectVideoJobListFilter) normalized() (DirectVideoJobListFilter, error) {
	filter.ScriptUnitID = strings.TrimSpace(filter.ScriptUnitID)
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	if filter.Status == "all" {
		filter.Status = ""
	}
	switch filter.Status {
	case "", "queued", "running", "succeeded", "failed", "cancelling", "cancelled":
	default:
		return DirectVideoJobListFilter{}, Error{
			Code: CodeDirectVideoInvalid, Message: "视频任务状态筛选无效",
		}
	}
	if filter.Limit < 0 || filter.Limit > 200 {
		return DirectVideoJobListFilter{}, Error{
			Code: CodeDirectVideoInvalid, Message: "视频任务返回数量必须在 1 到 200 之间",
		}
	}
	return filter, nil
}

type PreparedDirectVideoJob struct {
	Job         DirectVideoJob
	Route       DirectVideoRoute
	References  []DirectVideoReferenceSnapshot
	Production  ProductionContext
	PayloadHash string
}

type directVideoCapabilityCandidate struct {
	ModelProfileID        string                      `json:"modelProfileId"`
	ModelProfileKey       string                      `json:"modelProfileKey"`
	ModelProfileBindingID string                      `json:"modelProfileBindingId"`
	ProviderModelID       string                      `json:"providerModelId"`
	ProviderAccountID     string                      `json:"providerAccountId"`
	ModelKey              string                      `json:"modelKey"`
	Priority              int                         `json:"priority"`
	Weight                int                         `json:"weight"`
	PromptConstraint      DirectVideoPromptConstraint `json:"promptConstraint"`
	Capabilities          []struct {
		InputLimits           json.RawMessage `json:"inputLimits"`
		ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
	} `json:"capabilities"`
	Variants []struct {
		VariantKey                string   `json:"variantKey"`
		CapabilitySnapshotHash    string   `json:"capabilitySnapshotHash"`
		ExecutableDurationSeconds []int    `json:"executableDurationSeconds"`
		Resolutions               []string `json:"resolutions"`
		AspectRatios              []string `json:"aspectRatios"`
		Capability                struct {
			InputContract DirectVideoInputContract `json:"inputContract"`
			NativeAudio   DirectVideoNativeAudio   `json:"nativeAudio"`
		} `json:"capability"`
	} `json:"videoGenerationVariants"`
}

func BuildDirectVideoOptions(production ProductionContext) (DirectVideoOptions, error) {
	var configuration struct {
		ProductionConfiguration videoproduction.ProductionConfigurationSnapshot `json:"productionConfiguration"`
	}
	if err := json.Unmarshal(production.CommerceBinding.ConfigurationSnapshot, &configuration); err != nil {
		return DirectVideoOptions{}, Error{Code: CodeDirectVideoUnavailable, Message: "带货视频生产配置无法解析", Cause: err}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(production.CommerceBinding.CapabilitySnapshot, &root); err != nil {
		return DirectVideoOptions{}, Error{Code: CodeDirectVideoUnavailable, Message: "视频模型能力快照无法解析", Cause: err}
	}
	videoRaw, ok := root["videoGenerator"]
	if !ok {
		return DirectVideoOptions{}, Error{Code: CodeDirectVideoUnavailable, Message: "当前项目没有可用的视频模型路由"}
	}
	var video struct {
		Candidates []directVideoCapabilityCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(videoRaw, &video); err != nil {
		return DirectVideoOptions{}, Error{Code: CodeDirectVideoUnavailable, Message: "视频模型路由快照无法解析", Cause: err}
	}
	options := DirectVideoOptions{
		ContractVersion:                    CommerceDirectVideoContractV1,
		ProjectProductionGenerationID:      production.Generation.ID,
		VideoProductionBindingID:           production.VideoBinding.ID,
		VideoProductionBindingRevision:     production.VideoBinding.Revision,
		VideoProductionProfileVersionID:    production.VideoBinding.ProfileVersionID,
		VideoProductionProfileSnapshotHash: production.VideoBinding.ProfileSnapshotHash,
		DefaultAspectRatio:                 firstNonBlank(configuration.ProductionConfiguration.VideoRatio, configuration.ProductionConfiguration.AspectRatio, "9:16"),
	}
	resolutionSet := map[string]bool{}
	aspectSet := map[string]bool{}
	durationSet := map[int]bool{}
	for _, candidate := range video.Candidates {
		promptConstraint := normalizedDirectVideoPromptConstraint(candidate.PromptConstraint)
		if promptConstraint.MaxLength == 0 {
			promptConstraint = directVideoPromptConstraintFromCapabilities(candidate.Capabilities)
		}
		for _, variant := range candidate.Variants {
			if strings.TrimSpace(variant.VariantKey) == "" || !validSHA256(variant.CapabilitySnapshotHash) {
				continue
			}
			route := DirectVideoRoute{
				RouteKey:       candidate.ModelProfileBindingID + ":" + candidate.ProviderModelID + ":" + variant.VariantKey,
				ModelProfileID: candidate.ModelProfileID, ModelProfileKey: candidate.ModelProfileKey,
				ModelProfileBindingID: candidate.ModelProfileBindingID, ProviderModelID: candidate.ProviderModelID,
				ProviderAccountID: candidate.ProviderAccountID, ProviderModelKey: candidate.ModelKey,
				Priority: candidate.Priority, Weight: candidate.Weight, VariantKey: variant.VariantKey,
				CapabilitySnapshotHash:    variant.CapabilitySnapshotHash,
				ExecutableDurationSeconds: normalizedDirectVideoInts(variant.ExecutableDurationSeconds),
				Resolutions:               normalizedDirectVideoStrings(variant.Resolutions),
				AspectRatios:              normalizedDirectVideoStrings(variant.AspectRatios),
				PromptConstraint:          promptConstraint,
				InputContract:             variant.Capability.InputContract, NativeAudio: variant.Capability.NativeAudio,
			}
			if len(route.ExecutableDurationSeconds) == 0 || len(route.Resolutions) == 0 {
				continue
			}
			route.InputContract = effectiveDirectVideoInputContract(route.InputContract)
			options.Routes = append(options.Routes, route)
			for _, value := range route.ExecutableDurationSeconds {
				durationSet[value] = true
			}
			for _, value := range route.Resolutions {
				resolutionSet[value] = true
			}
			for _, value := range route.AspectRatios {
				aspectSet[value] = true
			}
		}
	}
	if len(options.Routes) == 0 {
		return DirectVideoOptions{}, Error{Code: CodeDirectVideoUnavailable, Message: "当前视频模型没有支持商品图片输入的可执行时长与分辨率"}
	}
	sort.SliceStable(options.Routes, func(i, j int) bool {
		if options.Routes[i].Priority != options.Routes[j].Priority {
			return options.Routes[i].Priority < options.Routes[j].Priority
		}
		if options.Routes[i].Weight != options.Routes[j].Weight {
			return options.Routes[i].Weight > options.Routes[j].Weight
		}
		return options.Routes[i].RouteKey < options.Routes[j].RouteKey
	})
	options.ExecutableDurationSeconds = sortedDirectVideoInts(durationSet)
	options.Resolutions = sortedDirectVideoStrings(resolutionSet)
	options.AspectRatios = sortedDirectVideoStrings(aspectSet)
	options.DefaultDurationSeconds = options.ExecutableDurationSeconds[len(options.ExecutableDurationSeconds)-1]
	defaultResolutionSet := map[string]bool{}
	for _, route := range options.Routes {
		if containsDirectVideoInt(route.ExecutableDurationSeconds, options.DefaultDurationSeconds) {
			for _, resolution := range route.Resolutions {
				defaultResolutionSet[resolution] = true
			}
		}
	}
	options.DefaultResolution = configuredDirectVideoResolution(
		configuration.ProductionConfiguration,
		sortedDirectVideoStrings(defaultResolutionSet),
	)
	defaultRoute, err := SelectDirectVideoRoute(options, options.DefaultDurationSeconds, options.DefaultResolution)
	if err != nil {
		return DirectVideoOptions{}, err
	}
	options.ScriptPromptConstraint = defaultRoute.PromptConstraint
	return options, nil
}

func SelectDirectVideoRoute(options DirectVideoOptions, duration int, resolution string) (DirectVideoRoute, error) {
	resolution = normalizeDirectVideoString(resolution)
	if duration <= 0 {
		return DirectVideoRoute{}, Error{Code: CodeDirectVideoInvalid, Message: "请选择视频模型支持的生成时长"}
	}
	if resolution == "" {
		resolution = options.DefaultResolution
	}
	for _, route := range options.Routes {
		if containsDirectVideoInt(route.ExecutableDurationSeconds, duration) && containsDirectVideoString(route.Resolutions, resolution) {
			return route, nil
		}
	}
	return DirectVideoRoute{}, Error{
		Code: CodeDirectVideoInvalid, Message: "当前视频模型不支持所选时长与分辨率组合",
		Details: map[string]any{"durationSeconds": duration, "resolution": resolution},
	}
}

func ValidateDirectVideoScript(content string, constraint DirectVideoPromptConstraint) error {
	constraint = normalizedDirectVideoPromptConstraint(constraint)
	length := MeasureDirectVideoPromptLength(content, constraint.Unit)
	if constraint.MaxLength <= 0 || length <= constraint.MaxLength {
		return nil
	}
	unitLabel := "个 Unicode 字符"
	if constraint.Unit == "utf8_bytes" {
		unitLabel = "个 UTF-8 字节"
	}
	return Error{
		Code: CodeScriptPromptTooLong,
		Message: fmt.Sprintf(
			"广告脚本长度为 %d%s，超过当前视频模型上限 %d%s",
			length, unitLabel, constraint.MaxLength, unitLabel,
		),
		Details: map[string]any{
			"actualLength": length,
			"maxLength":    constraint.MaxLength,
			"unit":         constraint.Unit,
		},
	}
}

func MeasureDirectVideoPromptLength(content string, unit string) int {
	content = strings.TrimSpace(content)
	if normalizeDirectVideoPromptLengthUnit(unit) == "utf8_bytes" {
		return len([]byte(content))
	}
	return len([]rune(content))
}

func AssignDirectVideoReferenceRoles(contract DirectVideoInputContract, count int) ([]string, error) {
	if count <= 0 {
		return nil, Error{Code: CodeProductPrimaryImage, Message: "请至少选择一张商品参考图"}
	}
	firstMin, firstMax := directVideoSlotBounds(contract, "first_frame")
	semanticMin, semanticMax := directVideoSlotBounds(contract, "semantic_reference")
	roles := make([]string, 0, count)
	switch {
	case firstMax > 0 && semanticMax > 0:
		roles = append(roles, "first_frame")
		remaining := directMinInt(count-1, semanticMax)
		for index := 0; index < remaining; index++ {
			roles = append(roles, "semantic_reference")
		}
	case firstMax > 0:
		roles = append(roles, "first_frame")
	case semanticMax > 0:
		limit := directMinInt(count, semanticMax)
		for index := 0; index < limit; index++ {
			roles = append(roles, "semantic_reference")
		}
	default:
		return nil, Error{Code: CodeDirectVideoUnavailable, Message: "当前视频模型不能接收商品参考图"}
	}
	firstCount, semanticCount := 0, 0
	for _, role := range roles {
		if role == "first_frame" {
			firstCount++
		} else {
			semanticCount++
		}
	}
	if firstCount < firstMin || semanticCount < semanticMin {
		return nil, Error{Code: CodeDirectVideoInvalid, Message: "所选参考图数量不满足当前视频模型输入要求"}
	}
	return roles, nil
}

func prioritizeDirectVideoReferences(references []DirectVideoReferenceSnapshot) {
	sort.SliceStable(references, func(left, right int) bool {
		return directVideoReferenceIsPrimaryProduct(references[left]) &&
			!directVideoReferenceIsPrimaryProduct(references[right])
	})
}

func directVideoReferenceIsPrimaryProduct(reference DirectVideoReferenceSnapshot) bool {
	if reference.SourceType != "product" || len(reference.Snapshot) == 0 {
		return false
	}
	var snapshot struct {
		IsPrimary bool `json:"isPrimary"`
	}
	return json.Unmarshal(reference.Snapshot, &snapshot) == nil && snapshot.IsPrimary
}

func DirectVideoHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return "", err
	}
	raw, err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func DirectVideoTextHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func DirectVideoReferenceSetHash(references []DirectVideoReferenceSnapshot) (string, error) {
	identities := make([]DirectVideoReferenceSnapshot, len(references))
	copy(identities, references)
	for index := range identities {
		identities[index].PreviewURL = ""
	}
	return DirectVideoHash(identities)
}

func directVideoContractSupportsImages(contract DirectVideoInputContract) bool {
	for _, slot := range contract.Slots {
		if strings.EqualFold(strings.TrimSpace(slot.MediaType), "image") &&
			(strings.EqualFold(strings.TrimSpace(slot.Role), "first_frame") ||
				strings.EqualFold(strings.TrimSpace(slot.Role), "semantic_reference")) &&
			slot.Max > 0 {
			return true
		}
	}
	return false
}

func effectiveDirectVideoInputContract(contract DirectVideoInputContract) DirectVideoInputContract {
	if directVideoContractSupportsImages(contract) {
		return contract
	}
	return DirectVideoInputContract{
		ContractKey: "first_frame",
		RequestMode: firstNonBlank(contract.RequestMode, "async_create"),
		Slots: []DirectVideoInputSlot{{
			Role: "first_frame", MediaType: "image", Semantics: "initial_frame",
			Min: 1, Max: 1, Ordered: true,
		}},
	}
}

func directVideoSlotBounds(contract DirectVideoInputContract, role string) (int, int) {
	for _, slot := range contract.Slots {
		if strings.EqualFold(strings.TrimSpace(slot.Role), role) && strings.EqualFold(strings.TrimSpace(slot.MediaType), "image") {
			return maxInt(slot.Min, 0), maxInt(slot.Max, 0)
		}
	}
	return 0, 0
}

func normalizedDirectVideoInts(values []int) []int {
	seen := map[int]bool{}
	for _, value := range values {
		if value > 0 {
			seen[value] = true
		}
	}
	return sortedDirectVideoInts(seen)
}

func sortedDirectVideoInts(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func normalizedDirectVideoStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if normalized := normalizeDirectVideoString(value); normalized != "" {
			seen[normalized] = true
		}
	}
	return sortedDirectVideoStrings(seen)
}

func normalizedDirectVideoPromptConstraint(constraint DirectVideoPromptConstraint) DirectVideoPromptConstraint {
	if constraint.MaxLength < 0 {
		constraint.MaxLength = 0
	}
	constraint.Unit = normalizeDirectVideoPromptLengthUnit(constraint.Unit)
	if constraint.Unit == "" {
		constraint.Unit = "characters"
	}
	return constraint
}

func directVideoPromptConstraintFromCapabilities(capabilities []struct {
	InputLimits           json.RawMessage `json:"inputLimits"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
}) DirectVideoPromptConstraint {
	constraint := DirectVideoPromptConstraint{Unit: "characters"}
	for _, capability := range capabilities {
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			values := directVideoPromptConstraintValues(raw)
			candidate := directVideoPositivePromptLimit(values)
			if candidate <= 0 || (constraint.MaxLength > 0 && candidate >= constraint.MaxLength) {
				continue
			}
			constraint.MaxLength = candidate
			constraint.Unit = normalizeDirectVideoPromptLengthUnit(
				directVideoPromptConstraintString(values, "promptLengthUnit", "promptLimitUnit"),
			)
			if constraint.Unit == "" {
				constraint.Unit = "characters"
			}
		}
	}
	return constraint
}

func directVideoPromptConstraintValues(raw json.RawMessage) map[string]any {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	if nested, ok := values["xCapabilities"].(map[string]any); ok {
		return nested
	}
	return values
}

func directVideoPositivePromptLimit(values map[string]any) int {
	for _, key := range []string{"promptMaxLength", "maxPromptLength", "maxPromptCharacters"} {
		if value := directVideoPromptConstraintInt(values[key]); value > 0 {
			return value
		}
	}
	return 0
}

func directVideoPromptConstraintInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func directVideoPromptConstraintString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeDirectVideoPromptLengthUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "byte", "bytes", "utf8_byte", "utf8_bytes", "utf-8-byte", "utf-8-bytes":
		return "utf8_bytes"
	case "character", "characters", "char", "chars", "rune", "runes":
		return "characters"
	default:
		return ""
	}
}

func sortedDirectVideoStrings(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeDirectVideoString(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsDirectVideoString(values []string, expected string) bool {
	expected = normalizeDirectVideoString(expected)
	for _, value := range values {
		if normalizeDirectVideoString(value) == expected {
			return true
		}
	}
	return false
}

func containsDirectVideoInt(values []int, expected int) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func configuredDirectVideoResolution(configuration videoproduction.ProductionConfigurationSnapshot, available []string) string {
	var settings map[string]any
	if len(configuration.Settings) > 0 && string(configuration.Settings) != "null" {
		_ = json.Unmarshal(configuration.Settings, &settings)
	}
	for _, key := range []string{"videoResolution", "resolution"} {
		if value, ok := settings[key].(string); ok && containsDirectVideoString(available, value) {
			return normalizeDirectVideoString(value)
		}
	}
	for _, preferred := range []string{"720p", "1280x720", "720x1280", "1080p", "1920x1080", "1080x1920"} {
		if containsDirectVideoString(available, preferred) {
			return preferred
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return ""
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func directMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validateDirectVideoJobInput(input CreateDirectVideoJobInput) error {
	if input.DurationSeconds < 0 {
		return errors.New("durationSeconds cannot be negative")
	}
	for _, reference := range input.References {
		if reference.SourceType != "product" && reference.SourceType != "custom" {
			return fmt.Errorf("unsupported reference source type %q", reference.SourceType)
		}
		if strings.TrimSpace(reference.SourceID) == "" {
			return errors.New("reference sourceId is required")
		}
	}
	return nil
}
