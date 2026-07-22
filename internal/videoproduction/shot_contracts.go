package videoproduction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	StateRolePlannedEntry = "planned_entry"
	StateRolePlannedExit  = "planned_exit"
	StateRoleObservedExit = "observed_exit"

	TransitionMatchAction   = "match_action_cut"
	TransitionSameScene     = "same_scene_cut"
	TransitionCameraCut     = "camera_cut"
	TransitionSubjectChange = "subject_change"
	TransitionSceneCut      = "scene_cut"
	TransitionTimeJump      = "time_jump"
	TransitionMontage       = "montage_cut"
	TransitionUnclassified  = "unclassified"

	TailPolicySoft = "soft"
	TailPolicyNone = "none"

	AnchorPolicyNew         = "new_anchor"
	AnchorPolicyMatchAction = "match_action_anchor"
	AnchorPolicyIndependent = "independent_anchor"
)

type ShotState struct {
	Scene           SceneState       `json:"scene"`
	Characters      []CharacterState `json:"characters"`
	Props           []PropState      `json:"props"`
	Camera          CameraState      `json:"camera"`
	Action          ActionState      `json:"action"`
	ScreenDirection string           `json:"screenDirection"`
}

type SceneState struct {
	AssetID   string `json:"assetId"`
	VariantID string `json:"variantId,omitempty"`
	TimeOfDay string `json:"timeOfDay,omitempty"`
	Weather   string `json:"weather,omitempty"`
	Lighting  string `json:"lighting,omitempty"`
}

type CharacterState struct {
	AssetID             string        `json:"assetId"`
	AppearanceVersionID string        `json:"appearanceVersionId,omitempty"`
	CostumeVariantID    string        `json:"costumeVariantId,omitempty"`
	Pose                string        `json:"pose,omitempty"`
	Expression          string        `json:"expression,omitempty"`
	Blocking            BlockingState `json:"blocking"`
}

type BlockingState struct {
	Horizontal           string `json:"horizontal"`
	Depth                string `json:"depth"`
	Facing               string `json:"facing"`
	EyelineTargetAssetID string `json:"eyelineTargetAssetId,omitempty"`
}

type PropState struct {
	AssetID       string `json:"assetId"`
	State         string `json:"state"`
	HolderAssetID string `json:"holderAssetId,omitempty"`
}

type CameraState struct {
	ShotSize   string `json:"shotSize"`
	Angle      string `json:"angle"`
	AxisSide   string `json:"axisSide"`
	LensIntent string `json:"lensIntent"`
	Movement   string `json:"movement"`
}

type ActionState struct {
	Entry string `json:"entry"`
	Exit  string `json:"exit"`
}

type TransitionSuggestion struct {
	TransitionType string   `json:"transitionType,omitempty"`
	Carry          []string `json:"carry,omitempty"`
	Reset          []string `json:"reset,omitempty"`
	TailPolicy     string   `json:"tailPolicy,omitempty"`
	AnchorPolicy   string   `json:"anchorPolicy,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`
}

type ShotTransition struct {
	TransitionType string   `json:"transitionType"`
	Carry          []string `json:"carry"`
	Reset          []string `json:"reset"`
	TailPolicy     string   `json:"tailPolicy"`
	AnchorPolicy   string   `json:"anchorPolicy"`
	Confidence     float64  `json:"confidence"`
}

type ContractIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type ShotContractReview struct {
	Approved bool              `json:"approved"`
	Checks   map[string]string `json:"checks"`
	Issues   []ContractIssue   `json:"issues"`
}

func NormalizeShotState(state ShotState) ShotState {
	state.Scene.AssetID = strings.TrimSpace(state.Scene.AssetID)
	state.Scene.VariantID = strings.TrimSpace(state.Scene.VariantID)
	state.Scene.TimeOfDay = enumValue(state.Scene.TimeOfDay)
	state.Scene.Weather = enumValue(state.Scene.Weather)
	state.Scene.Lighting = enumValue(state.Scene.Lighting)
	state.ScreenDirection = enumValue(state.ScreenDirection)
	state.Camera.ShotSize = enumValue(state.Camera.ShotSize)
	state.Camera.Angle = enumValue(state.Camera.Angle)
	state.Camera.AxisSide = strings.ToUpper(strings.TrimSpace(state.Camera.AxisSide))
	state.Camera.LensIntent = enumValue(state.Camera.LensIntent)
	state.Camera.Movement = enumValue(state.Camera.Movement)
	state.Action.Entry = strings.TrimSpace(state.Action.Entry)
	state.Action.Exit = strings.TrimSpace(state.Action.Exit)
	for index := range state.Characters {
		character := &state.Characters[index]
		character.AssetID = strings.TrimSpace(character.AssetID)
		character.AppearanceVersionID = strings.TrimSpace(character.AppearanceVersionID)
		character.CostumeVariantID = strings.TrimSpace(character.CostumeVariantID)
		character.Pose = strings.TrimSpace(character.Pose)
		character.Expression = strings.TrimSpace(character.Expression)
		character.Blocking.Horizontal = enumValue(character.Blocking.Horizontal)
		character.Blocking.Depth = enumValue(character.Blocking.Depth)
		character.Blocking.Facing = enumValue(character.Blocking.Facing)
		if character.Blocking.Horizontal == "offscreen" && (character.Blocking.Facing == "" || character.Blocking.Facing == "unknown") {
			character.Blocking.Facing = "away"
		}
		character.Blocking.EyelineTargetAssetID = strings.TrimSpace(character.Blocking.EyelineTargetAssetID)
	}
	for index := range state.Props {
		state.Props[index].AssetID = strings.TrimSpace(state.Props[index].AssetID)
		state.Props[index].State = enumValue(state.Props[index].State)
		state.Props[index].HolderAssetID = strings.TrimSpace(state.Props[index].HolderAssetID)
	}
	sort.Slice(state.Characters, func(left, right int) bool { return state.Characters[left].AssetID < state.Characters[right].AssetID })
	sort.Slice(state.Props, func(left, right int) bool { return state.Props[left].AssetID < state.Props[right].AssetID })
	if state.Characters == nil {
		state.Characters = []CharacterState{}
	}
	if state.Props == nil {
		state.Props = []PropState{}
	}
	return state
}

func ValidateShotState(state ShotState) error {
	state = NormalizeShotState(state)
	if err := requiredUUID("scene.assetId", state.Scene.AssetID); err != nil {
		return err
	}
	if err := optionalUUID("scene.variantId", state.Scene.VariantID); err != nil {
		return err
	}
	if !oneOf(state.Scene.TimeOfDay, "", "dawn", "morning", "day", "afternoon", "dusk", "night", "unknown") {
		return fmt.Errorf("scene.timeOfDay has unsupported value %q", state.Scene.TimeOfDay)
	}
	if !oneOf(state.Scene.Weather, "", "clear", "cloudy", "overcast", "fog", "light_rain", "rain", "storm", "snow", "indoor", "unknown") {
		return fmt.Errorf("scene.weather has unsupported value %q", state.Scene.Weather)
	}
	if !oneOf(state.Camera.ShotSize, "extreme_wide", "wide", "full", "medium_wide", "medium", "medium_close_up", "close_up", "extreme_close_up", "insert") {
		return fmt.Errorf("camera.shotSize has unsupported value %q", state.Camera.ShotSize)
	}
	if !oneOf(state.Camera.Angle, "eye_level", "low", "high", "overhead", "dutch", "over_shoulder", "point_of_view") {
		return fmt.Errorf("camera.angle has unsupported value %q", state.Camera.Angle)
	}
	if !oneOf(state.Camera.AxisSide, "A", "B", "NEUTRAL") {
		return fmt.Errorf("camera.axisSide has unsupported value %q", state.Camera.AxisSide)
	}
	if !oneOf(state.Camera.LensIntent, "wide", "normal", "telephoto", "macro") {
		return fmt.Errorf("camera.lensIntent has unsupported value %q", state.Camera.LensIntent)
	}
	if !oneOf(state.Camera.Movement, "static", "pan", "tilt", "dolly_in", "dolly_out", "tracking", "crane", "handheld", "orbit", "zoom_in", "zoom_out") {
		return fmt.Errorf("camera.movement has unsupported value %q", state.Camera.Movement)
	}
	if !oneOf(state.ScreenDirection, "left_to_right", "right_to_left", "static", "toward_camera", "away_from_camera", "unknown") {
		return fmt.Errorf("screenDirection has unsupported value %q", state.ScreenDirection)
	}
	if state.Action.Entry == "" || state.Action.Exit == "" {
		return fmt.Errorf("action.entry and action.exit are required")
	}
	seenCharacters := map[string]bool{}
	for index, character := range state.Characters {
		prefix := fmt.Sprintf("characters[%d]", index)
		if err := requiredUUID(prefix+".assetId", character.AssetID); err != nil {
			return err
		}
		if seenCharacters[character.AssetID] {
			return fmt.Errorf("characters contains duplicate asset %s", character.AssetID)
		}
		seenCharacters[character.AssetID] = true
		if err := optionalUUID(prefix+".appearanceVersionId", character.AppearanceVersionID); err != nil {
			return err
		}
		if err := optionalUUID(prefix+".costumeVariantId", character.CostumeVariantID); err != nil {
			return err
		}
		if !oneOf(character.Blocking.Horizontal, "left", "center", "right", "offscreen") ||
			!oneOf(character.Blocking.Depth, "foreground", "midground", "background") ||
			!oneOf(character.Blocking.Facing, "screen_left", "screen_right", "camera", "away", "profile") {
			return fmt.Errorf("%s.blocking contains an unsupported enum", prefix)
		}
		if err := optionalUUID(prefix+".blocking.eyelineTargetAssetId", character.Blocking.EyelineTargetAssetID); err != nil {
			return err
		}
	}
	seenProps := map[string]bool{}
	for index, prop := range state.Props {
		prefix := fmt.Sprintf("props[%d]", index)
		if err := requiredUUID(prefix+".assetId", prop.AssetID); err != nil {
			return err
		}
		if seenProps[prop.AssetID] {
			return fmt.Errorf("props contains duplicate asset %s", prop.AssetID)
		}
		seenProps[prop.AssetID] = true
		if !oneOf(prop.State, "present", "held", "worn", "placed", "moving", "damaged", "hidden", "consumed", "unknown") {
			return fmt.Errorf("%s.state has unsupported value %q", prefix, prop.State)
		}
		if err := optionalUUID(prefix+".holderAssetId", prop.HolderAssetID); err != nil {
			return err
		}
		if prop.State == "held" && (prop.HolderAssetID == "" || !seenCharacters[prop.HolderAssetID]) {
			return fmt.Errorf("%s held prop requires a holder present in characters", prefix)
		}
	}
	return nil
}

func HashShotState(state ShotState) (string, error) {
	state = NormalizeShotState(state)
	if err := ValidateShotState(state); err != nil {
		return "", err
	}
	return canonicalHash(state)
}

func ClassifyTransition(previous *ShotState, current ShotState, suggested TransitionSuggestion) (ShotTransition, error) {
	current = NormalizeShotState(current)
	if err := ValidateShotState(current); err != nil {
		return ShotTransition{}, fmt.Errorf("target shot state: %w", err)
	}
	if previous == nil {
		return normalizeTransition(ShotTransition{
			TransitionType: TransitionUnclassified,
			TailPolicy:     TailPolicyNone,
			AnchorPolicy:   AnchorPolicyIndependent,
			Reset:          []string{"camera", "character.blocking", "frame.composition"},
			Confidence:     1,
		}), nil
	}
	prior := NormalizeShotState(*previous)
	if err := ValidateShotState(prior); err != nil {
		return ShotTransition{}, fmt.Errorf("source shot state: %w", err)
	}
	typeValue := TransitionSameScene
	confidence := 0.95
	switch {
	case prior.Scene.AssetID != current.Scene.AssetID || prior.Scene.VariantID != current.Scene.VariantID:
		typeValue = TransitionSceneCut
	case prior.Scene.TimeOfDay != current.Scene.TimeOfDay || prior.Scene.Weather != current.Scene.Weather || prior.Scene.Lighting != current.Scene.Lighting:
		typeValue = TransitionTimeJump
	case !sameStringSet(characterIDs(prior), characterIDs(current)):
		typeValue = TransitionSubjectChange
	case cameraResetRequired(prior.Camera, current.Camera):
		typeValue = TransitionCameraCut
	case enumValue(suggested.TransitionType) == TransitionMontage:
		typeValue = TransitionMontage
	case enumValue(suggested.TransitionType) == TransitionMatchAction && actionMatches(prior.Action.Exit, current.Action.Entry):
		typeValue = TransitionMatchAction
	default:
		typeValue = TransitionSameScene
	}
	transition := transitionPolicy(typeValue)
	transition.Confidence = confidence
	if suggested.Confidence > 0 && suggested.Confidence < transition.Confidence {
		transition.Confidence = suggested.Confidence
	}
	return normalizeTransition(transition), nil
}

func HashTransition(transition ShotTransition) (string, error) {
	transition = normalizeTransition(transition)
	if err := ValidateTransition(transition); err != nil {
		return "", err
	}
	return canonicalHash(transition)
}

func ValidateTransition(transition ShotTransition) error {
	transition = normalizeTransition(transition)
	if !oneOf(transition.TransitionType, TransitionMatchAction, TransitionSameScene, TransitionCameraCut, TransitionSubjectChange, TransitionSceneCut, TransitionTimeJump, TransitionMontage, TransitionUnclassified) {
		return fmt.Errorf("unsupported transitionType %q", transition.TransitionType)
	}
	if !oneOf(transition.TailPolicy, TailPolicySoft, TailPolicyNone) {
		return fmt.Errorf("cross-shot tailPolicy must be soft or none")
	}
	if !oneOf(transition.AnchorPolicy, AnchorPolicyNew, AnchorPolicyMatchAction, AnchorPolicyIndependent) {
		return fmt.Errorf("unsupported anchorPolicy %q", transition.AnchorPolicy)
	}
	if transition.Confidence < 0 || transition.Confidence > 1 {
		return fmt.Errorf("transition confidence must be between 0 and 1")
	}
	return nil
}

func ReviewShotContract(entry, exit ShotState, transition ShotTransition, requiredAssetIDs []string) ShotContractReview {
	review := ShotContractReview{Approved: true, Checks: map[string]string{}, Issues: []ContractIssue{}}
	if err := ValidateShotState(entry); err != nil {
		review.addIssue("INVALID_ENTRY_STATE", "plannedEntryState", err.Error())
	} else {
		review.Checks["plannedEntryState"] = "passed"
	}
	if err := ValidateShotState(exit); err != nil {
		review.addIssue("INVALID_EXIT_STATE", "plannedExitState", err.Error())
	} else {
		review.Checks["plannedExitState"] = "passed"
	}
	if err := ValidateTransition(transition); err != nil {
		review.addIssue("INVALID_TRANSITION", "transitionFromPrevious", err.Error())
	} else {
		review.Checks["transition"] = "passed"
	}
	present := visibleStateAssetSet(entry)
	for _, assetID := range requiredAssetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID != "" && !present[assetID] {
			review.addIssue("MISSING_REQUIRED_ASSET", "plannedEntryState", "required asset "+assetID+" is absent from the planned first frame")
		}
	}
	if review.Checks["requiredAssets"] == "" {
		review.Checks["requiredAssets"] = "passed"
	}
	if entry.Scene.AssetID != exit.Scene.AssetID || !sameStringSet(characterIDs(entry), characterIDs(exit)) {
		review.addIssue("UNREACHABLE_IDENTITY_CHANGE", "plannedExitState", "a single storyboard shot cannot change scene or character identity set")
	} else {
		review.Checks["identityReachability"] = "passed"
	}
	return review
}

func ReviewFirstLastFrameContract(
	entry ShotState,
	exit ShotState,
	transition ShotTransition,
	requiredAssetIDs []string,
	durationTicks int64,
	timelineTimebase int64,
) ShotContractReview {
	review := ReviewShotContract(entry, exit, transition, requiredAssetIDs)
	entry = NormalizeShotState(entry)
	exit = NormalizeShotState(exit)

	if entry.Scene != exit.Scene {
		review.addIssue("FIRST_LAST_SCENE_DRIFT", "plannedExitState.scene", "首尾帧必须保持同一场景、时间、天气和光线")
	} else {
		review.Checks["firstLastSceneIdentity"] = "passed"
	}
	entryCharacters := characterStateByID(entry.Characters)
	exitCharacters := characterStateByID(exit.Characters)
	if sameStringSet(characterIDs(entry), characterIDs(exit)) {
		stable := true
		for assetID, first := range entryCharacters {
			last := exitCharacters[assetID]
			if first.AppearanceVersionID != last.AppearanceVersionID || first.CostumeVariantID != last.CostumeVariantID {
				stable = false
				review.addIssue("FIRST_LAST_CHARACTER_IDENTITY_DRIFT", "plannedExitState.characters", "角色 "+assetID+" 的外观或服装版本在首尾帧之间发生变化")
			}
		}
		if stable {
			review.Checks["firstLastCharacterIdentity"] = "passed"
		}
	}
	if !sameStringSet(propIDs(entry), propIDs(exit)) {
		review.addIssue("FIRST_LAST_PROP_IDENTITY_DRIFT", "plannedExitState.props", "首尾帧必须保持同一道具身份集合")
	} else {
		review.Checks["firstLastPropIdentity"] = "passed"
	}
	if entry.Camera.AxisSide != exit.Camera.AxisSide {
		review.addIssue("FIRST_LAST_AXIS_CROSSING", "plannedExitState.camera.axisSide", "单镜头首尾帧不得跨越空间轴")
	} else {
		review.Checks["firstLastSpatialAxis"] = "passed"
	}
	cameraChanged := entry.Camera.ShotSize != exit.Camera.ShotSize || entry.Camera.Angle != exit.Camera.Angle || entry.Camera.LensIntent != exit.Camera.LensIntent
	if cameraChanged && (entry.Camera.Movement == "static" || exit.Camera.Movement == "static") {
		review.addIssue("FIRST_LAST_CAMERA_UNREACHABLE", "plannedExitState.camera", "静止机位不能在同一镜头内到达不同的尾帧构图")
	} else {
		review.Checks["firstLastCameraReachability"] = "passed"
	}

	if durationTicks <= 0 || timelineTimebase <= 0 {
		review.addIssue("FIRST_LAST_DURATION_INVALID", "duration", "首尾帧审核需要有效的镜头时长和时间基准")
	} else {
		availableSeconds := float64(durationTicks) / float64(timelineTimebase)
		requiredSeconds := minimumFirstLastTransitionSeconds(entry, exit)
		if availableSeconds+0.001 < requiredSeconds {
			review.addIssue(
				"FIRST_LAST_DURATION_TOO_SHORT",
				"duration",
				fmt.Sprintf("首尾状态变化至少需要 %.2f 秒，当前镜头只有 %.2f 秒", requiredSeconds, availableSeconds),
			)
		} else {
			review.Checks["firstLastDurationReachability"] = "passed"
		}
	}
	return review
}

func characterStateByID(values []CharacterState) map[string]CharacterState {
	result := make(map[string]CharacterState, len(values))
	for _, value := range values {
		result[value.AssetID] = value
	}
	return result
}

func propIDs(state ShotState) []string {
	result := make([]string, 0, len(state.Props))
	for _, item := range state.Props {
		result = append(result, item.AssetID)
	}
	return result
}

func minimumFirstLastTransitionSeconds(entry, exit ShotState) float64 {
	entryCharacters := characterStateByID(entry.Characters)
	exitCharacters := characterStateByID(exit.Characters)
	required := 1.0
	for assetID, first := range entryCharacters {
		last, ok := exitCharacters[assetID]
		if !ok {
			continue
		}
		movement := 0.0
		movement += enumDistance(first.Blocking.Horizontal, last.Blocking.Horizontal, []string{"left", "center", "right"}) * 0.75
		movement += enumDistance(first.Blocking.Depth, last.Blocking.Depth, []string{"foreground", "midground", "background"})
		if first.Blocking.Facing != last.Blocking.Facing {
			movement += 0.75
		}
		if first.Pose != last.Pose || first.Expression != last.Expression {
			movement += 0.5
		}
		if movement > required {
			required = movement
		}
	}
	cameraSeconds := enumDistance(entry.Camera.ShotSize, exit.Camera.ShotSize, []string{
		"extreme_wide", "wide", "full", "medium_wide", "medium", "medium_close_up", "close_up", "extreme_close_up", "insert",
	}) * 0.5
	if entry.Camera.Angle != exit.Camera.Angle {
		cameraSeconds += 1
	}
	if entry.Camera.LensIntent != exit.Camera.LensIntent {
		cameraSeconds += 0.5
	}
	if cameraSeconds > required {
		required = cameraSeconds
	}
	entryProps := make(map[string]PropState, len(entry.Props))
	for _, item := range entry.Props {
		entryProps[item.AssetID] = item
	}
	for _, last := range exit.Props {
		first, ok := entryProps[last.AssetID]
		if ok && (first.State != last.State || first.HolderAssetID != last.HolderAssetID) && required < 1.5 {
			required = 1.5
		}
	}
	return required
}

func enumDistance(left, right string, ordered []string) float64 {
	if left == right {
		return 0
	}
	leftIndex, rightIndex := -1, -1
	for index, value := range ordered {
		if left == value {
			leftIndex = index
		}
		if right == value {
			rightIndex = index
		}
	}
	if leftIndex < 0 || rightIndex < 0 {
		return 1
	}
	distance := leftIndex - rightIndex
	if distance < 0 {
		distance = -distance
	}
	return float64(distance)
}

func (review *ShotContractReview) addIssue(code, field, message string) {
	review.Approved = false
	review.Checks[field] = "failed"
	if code == "MISSING_REQUIRED_ASSET" {
		review.Checks["requiredAssets"] = "failed"
	}
	review.Issues = append(review.Issues, ContractIssue{Code: code, Field: field, Message: message})
}

func transitionPolicy(transitionType string) ShotTransition {
	base := ShotTransition{
		TransitionType: transitionType,
		TailPolicy:     TailPolicyNone,
		AnchorPolicy:   AnchorPolicyNew,
		Carry:          []string{"character.identity", "character.costume", "prop.state", "scene.weather"},
		Reset:          []string{"camera", "character.blocking", "frame.composition"},
	}
	switch transitionType {
	case TransitionMatchAction:
		base.TailPolicy = TailPolicySoft
		base.AnchorPolicy = AnchorPolicyMatchAction
		base.Carry = append(base.Carry, "action.phase", "screen_direction")
	case TransitionSameScene:
		base.TailPolicy = TailPolicySoft
	case TransitionSceneCut, TransitionTimeJump, TransitionMontage, TransitionUnclassified:
		base.AnchorPolicy = AnchorPolicyIndependent
		base.Carry = []string{"character.identity"}
		base.Reset = []string{"scene", "camera", "character.blocking", "frame.composition", "screen_direction"}
	case TransitionSubjectChange:
		base.Reset = append(base.Reset, "character.visible_set")
	case TransitionCameraCut:
		base.Reset = append(base.Reset, "camera.axis_side")
	}
	return base
}

func normalizeTransition(value ShotTransition) ShotTransition {
	value.TransitionType = enumValue(value.TransitionType)
	value.TailPolicy = enumValue(value.TailPolicy)
	value.AnchorPolicy = enumValue(value.AnchorPolicy)
	value.Carry = normalizedUniqueStrings(value.Carry)
	value.Reset = normalizedUniqueStrings(value.Reset)
	return value
}

func stateAssetSet(state ShotState) map[string]bool {
	result := map[string]bool{state.Scene.AssetID: state.Scene.AssetID != ""}
	for _, character := range state.Characters {
		result[character.AssetID] = true
	}
	for _, prop := range state.Props {
		result[prop.AssetID] = true
	}
	return result
}

// AlignShotStateVisibility keeps continuity-only identities in the state while
// making the first-frame visibility set authoritative from asset requirements.
// A planner may retain an off-screen opponent for eyelines and action context,
// but that identity must not become a required image reference.
func AlignShotStateVisibility(state ShotState, visibleAssetIDs []string) ShotState {
	state = NormalizeShotState(state)
	visible := make(map[string]bool, len(visibleAssetIDs))
	for _, assetID := range visibleAssetIDs {
		if assetID = strings.TrimSpace(assetID); assetID != "" {
			visible[assetID] = true
		}
	}
	for index := range state.Characters {
		if visible[state.Characters[index].AssetID] {
			if state.Characters[index].Blocking.Horizontal == "offscreen" {
				state.Characters[index].Blocking.Horizontal = "center"
			}
		} else {
			state.Characters[index].Blocking.Horizontal = "offscreen"
		}
	}
	for index := range state.Props {
		if visible[state.Props[index].AssetID] {
			if state.Props[index].State == "hidden" {
				state.Props[index].State = "present"
			}
		} else {
			state.Props[index].State = "hidden"
		}
	}
	return NormalizeShotState(state)
}

func visibleStateAssetSet(state ShotState) map[string]bool {
	state = NormalizeShotState(state)
	result := map[string]bool{state.Scene.AssetID: state.Scene.AssetID != ""}
	for _, character := range state.Characters {
		if character.Blocking.Horizontal != "offscreen" {
			result[character.AssetID] = character.AssetID != ""
		}
	}
	for _, prop := range state.Props {
		if prop.State != "hidden" {
			result[prop.AssetID] = prop.AssetID != ""
		}
	}
	return result
}

func RequiredReferenceAssetIDs(state ShotState) []string {
	state = NormalizeShotState(state)
	assets := visibleStateAssetSet(state)
	result := make([]string, 0, len(assets))
	for assetID, present := range assets {
		if present {
			result = append(result, assetID)
		}
	}
	sort.Strings(result)
	return result
}

func characterIDs(state ShotState) []string {
	values := make([]string, 0, len(state.Characters))
	for _, item := range state.Characters {
		values = append(values, item.AssetID)
	}
	sort.Strings(values)
	return values
}

func cameraResetRequired(left, right CameraState) bool {
	return left.ShotSize != right.ShotSize || left.Angle != right.Angle || left.AxisSide != right.AxisSide || left.LensIntent != right.LensIntent
}

func actionMatches(left, right string) bool {
	left = strings.ToLower(strings.Join(strings.Fields(left), " "))
	right = strings.ToLower(strings.Join(strings.Fields(right), " "))
	return left != "" && right != "" && (left == right || strings.Contains(left, right) || strings.Contains(right, left))
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func requiredUUID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return optionalUUID(field, value)
}

func optionalUUID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%s must be a UUID", field)
	}
	return nil
}

func enumValue(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func normalizedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = enumValue(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	if result == nil {
		return []string{}
	}
	return result
}

func canonicalHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// HashCanonicalContract returns the stable SHA-256 used by persisted production
// contracts. Callers must pass normalized, JSON-serializable values.
func HashCanonicalContract(value any) (string, error) {
	return canonicalHash(value)
}
