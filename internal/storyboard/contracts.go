package storyboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

var (
	ErrInvalidTimingAnalyzerOutput = errors.New("invalid timing analyzer output")
	ErrInvalidContinuityBlueprint  = errors.New("invalid continuity blueprint")
	ErrInvalidShotPlannerOutput    = errors.New("invalid shot planner output")
	ErrInvalidStoryboardReview     = errors.New("invalid storyboard review")
)

type TimingAnalyzerOutput struct {
	Scenes []TimingAnalyzerScene `json:"scenes"`
}

type TimingAnalyzerScene struct {
	SceneKey      string               `json:"sceneKey"`
	ScriptSceneID string               `json:"scriptSceneId,omitempty"`
	SceneOrdinal  int                  `json:"sceneOrdinal"`
	Units         []TimingAnalyzerUnit `json:"units"`
}

type TimingAnalyzerUnit struct {
	UnitKey             string         `json:"unitKey"`
	UnitOrdinal         int            `json:"unitOrdinal"`
	Type                TimingUnitType `json:"type"`
	Track               TimingTrack    `json:"track"`
	ParallelGroup       string         `json:"parallelGroup,omitempty"`
	Speaker             string         `json:"speaker,omitempty"`
	Text                string         `json:"text"`
	Delivery            string         `json:"delivery,omitempty"`
	Language            string         `json:"language,omitempty"`
	ActionKind          ActionKind     `json:"actionKind,omitempty"`
	SuggestedSeconds    float64        `json:"suggestedSeconds,omitempty"`
	ExceptionReason     string         `json:"exceptionReason,omitempty"`
	SourceStartOffset   *int           `json:"sourceStartOffset,omitempty"`
	SourceEndOffset     *int           `json:"sourceEndOffset,omitempty"`
	ForceBoundaryBefore bool           `json:"forceBoundaryBefore,omitempty"`
	ForceBoundaryAfter  bool           `json:"forceBoundaryAfter,omitempty"`
}

type ContinuityBlueprintOutput struct {
	Scenes         []ContinuityBlueprintScene      `json:"scenes"`
	Dependencies   []ContinuityBlueprintDependency `json:"dependencies"`
	SerialGroups   [][]string                      `json:"serialGroups,omitempty"`
	ParallelGroups [][]string                      `json:"parallelGroups,omitempty"`
}

type ContinuityBlueprintScene struct {
	SceneKey             string         `json:"sceneKey"`
	SceneOrdinal         int            `json:"sceneOrdinal"`
	PacingProfile        string         `json:"pacingProfile"`
	SuggestedShotMinimum int            `json:"suggestedShotMinimum"`
	SuggestedShotMaximum int            `json:"suggestedShotMaximum"`
	EntryState           map[string]any `json:"entryState"`
	ExitState            map[string]any `json:"exitState"`
	OneTake              bool           `json:"oneTake,omitempty"`
	ContinuityNotes      []string       `json:"continuityNotes,omitempty"`
}

type ContinuityBlueprintDependency struct {
	FromSceneKey string `json:"fromSceneKey"`
	ToSceneKey   string `json:"toSceneKey"`
	Reason       string `json:"reason"`
	Strong       bool   `json:"strong"`
}

type ShotPlannerOutput struct {
	SceneKey string                  `json:"sceneKey"`
	Shots    []ShotPlannerSuggestion `json:"shots"`
}

type ShotPlannerSuggestion struct {
	SuggestionKey          string                               `json:"suggestionKey"`
	TimingUnitIDs          []string                             `json:"timingUnitIds"`
	CutAfterTimingUnitID   string                               `json:"cutAfterTimingUnitId,omitempty"`
	CutReason              string                               `json:"cutReason"`
	Title                  string                               `json:"title,omitempty"`
	Visual                 string                               `json:"visual"`
	Camera                 string                               `json:"camera"`
	Motion                 string                               `json:"motion"`
	Mood                   string                               `json:"mood"`
	OneTake                bool                                 `json:"oneTake,omitempty"`
	ImagePromptDirection   string                               `json:"imagePromptDirection,omitempty"`
	VideoPromptDirection   string                               `json:"videoPromptDirection,omitempty"`
	AssetRequirements      []ShotPlannerAssetRequirement        `json:"assetRequirements,omitempty"`
	PlannedEntryState      videoproduction.ShotState            `json:"plannedEntryState"`
	PlannedExitState       videoproduction.ShotState            `json:"plannedExitState"`
	TransitionFromPrevious videoproduction.TransitionSuggestion `json:"transitionFromPrevious"`
}

type ShotPlannerAssetRequirement struct {
	AssetID         string `json:"assetId"`
	RequirementType string `json:"requirementType"`
	RoleInShot      string `json:"roleInShot,omitempty"`
	Costume         string `json:"costume,omitempty"`
	Pose            string `json:"pose,omitempty"`
	Expression      string `json:"expression,omitempty"`
	Action          string `json:"action,omitempty"`
	CameraRelation  string `json:"cameraRelation,omitempty"`
	SceneState      string `json:"sceneState,omitempty"`
	PropState       string `json:"propState,omitempty"`
}

type StoryboardReviewerOutput struct {
	Approved    bool                      `json:"approved"`
	Issues      []StoryboardReviewerIssue `json:"issues"`
	Corrections []StoryboardCorrection    `json:"corrections,omitempty"`
}

type StoryboardReviewerIssue struct {
	Code          string   `json:"code"`
	Severity      string   `json:"severity"`
	Message       string   `json:"message"`
	SceneKey      string   `json:"sceneKey,omitempty"`
	ShotOrdinal   *int     `json:"shotOrdinal,omitempty"`
	TimingUnitIDs []string `json:"timingUnitIds,omitempty"`
}

type StoryboardCorrection struct {
	Type          string   `json:"type"`
	SceneKey      string   `json:"sceneKey,omitempty"`
	ShotOrdinal   *int     `json:"shotOrdinal,omitempty"`
	TimingUnitIDs []string `json:"timingUnitIds,omitempty"`
	Reason        string   `json:"reason"`
}

func ParseTimingAnalyzerOutput(raw string) (TimingAnalyzerOutput, error) {
	output, err := DecodeTimingAnalyzerOutput(raw)
	if err != nil {
		return TimingAnalyzerOutput{}, err
	}
	if err := ValidateTimingAnalyzerOutput(output); err != nil {
		return TimingAnalyzerOutput{}, err
	}
	return output, nil
}

// DecodeTimingAnalyzerOutput performs strict JSON decoding and enum
// normalization without trusting model-supplied ordering identifiers. Batch
// runtimes canonicalize those identifiers before calling Validate.
func DecodeTimingAnalyzerOutput(raw string) (TimingAnalyzerOutput, error) {
	var output TimingAnalyzerOutput
	if err := decodeStrictJSON(raw, &output); err != nil {
		return TimingAnalyzerOutput{}, fmt.Errorf("%w: %v", ErrInvalidTimingAnalyzerOutput, err)
	}
	return NormalizeTimingAnalyzerOutput(output), nil
}

// NormalizeTimingAnalyzerOutput canonicalizes a deliberately small set of
// common model aliases. Unknown values remain invalid so schema drift is not
// silently accepted.
func NormalizeTimingAnalyzerOutput(output TimingAnalyzerOutput) TimingAnalyzerOutput {
	for sceneIndex := range output.Scenes {
		for unitIndex := range output.Scenes[sceneIndex].Units {
			unit := &output.Scenes[sceneIndex].Units[unitIndex]
			rawType := strings.ToLower(strings.TrimSpace(string(unit.Type)))
			unit.Type = canonicalTimingUnitType(unit.Type)
			unit.Track = canonicalTimingTrack(unit.Track, unit.Type)
			if rawType == "combat" && unit.ActionKind == "" {
				unit.ActionKind = ActionCombat
			}
		}
	}
	return output
}

func ValidateTimingAnalyzerOutput(output TimingAnalyzerOutput) error {
	if len(output.Scenes) == 0 {
		return fmt.Errorf("%w: scenes are required", ErrInvalidTimingAnalyzerOutput)
	}
	seenScenes := map[string]bool{}
	seenUnits := map[string]bool{}
	expectedUnitOrdinal := 0
	for sceneIndex, scene := range output.Scenes {
		if strings.TrimSpace(scene.SceneKey) == "" || seenScenes[scene.SceneKey] || scene.SceneOrdinal != sceneIndex {
			return fmt.Errorf("%w: scene keys must be unique and scene ordinals contiguous", ErrInvalidTimingAnalyzerOutput)
		}
		seenScenes[scene.SceneKey] = true
		if len(scene.Units) == 0 {
			return fmt.Errorf("%w: scene %s has no timing units", ErrInvalidTimingAnalyzerOutput, scene.SceneKey)
		}
		for _, unit := range scene.Units {
			unit.UnitKey = strings.TrimSpace(unit.UnitKey)
			if unit.UnitKey == "" || seenUnits[unit.UnitKey] || unit.UnitOrdinal != expectedUnitOrdinal {
				return fmt.Errorf("%w: timing unit keys must be unique and ordinals contiguous", ErrInvalidTimingAnalyzerOutput)
			}
			if !validTimingUnitType(unit.Type) || !validTimingTrack(unit.Track) {
				return fmt.Errorf("%w: timing unit %s has invalid type or track", ErrInvalidTimingAnalyzerOutput, unit.UnitKey)
			}
			if isSpeechTimingUnit(unit.Type) && strings.TrimSpace(unit.Text) == "" {
				return fmt.Errorf("%w: speech timing unit %s has no source text", ErrInvalidTimingAnalyzerOutput, unit.UnitKey)
			}
			if unit.SourceStartOffset != nil || unit.SourceEndOffset != nil {
				if unit.SourceStartOffset == nil || unit.SourceEndOffset == nil || *unit.SourceStartOffset < 0 || *unit.SourceEndOffset <= *unit.SourceStartOffset {
					return fmt.Errorf("%w: timing unit %s has invalid source offsets", ErrInvalidTimingAnalyzerOutput, unit.UnitKey)
				}
			}
			seenUnits[unit.UnitKey] = true
			expectedUnitOrdinal++
		}
	}
	return nil
}

func ParseContinuityBlueprint(raw string, sceneKeys []string) (ContinuityBlueprintOutput, error) {
	var output ContinuityBlueprintOutput
	if err := decodeStrictJSON(raw, &output); err != nil {
		return ContinuityBlueprintOutput{}, fmt.Errorf("%w: %v", ErrInvalidContinuityBlueprint, err)
	}
	if err := ValidateContinuityBlueprint(output, sceneKeys); err != nil {
		return ContinuityBlueprintOutput{}, err
	}
	return output, nil
}

func ValidateContinuityBlueprint(output ContinuityBlueprintOutput, expectedSceneKeys []string) error {
	if len(output.Scenes) != len(expectedSceneKeys) || len(expectedSceneKeys) == 0 {
		return fmt.Errorf("%w: blueprint must contain every scene exactly once", ErrInvalidContinuityBlueprint)
	}
	known := make(map[string]bool, len(expectedSceneKeys))
	for index, key := range expectedSceneKeys {
		known[key] = true
		if output.Scenes[index].SceneKey != key || output.Scenes[index].SceneOrdinal != index {
			return fmt.Errorf("%w: blueprint scene order differs from script order", ErrInvalidContinuityBlueprint)
		}
		if output.Scenes[index].SuggestedShotMinimum <= 0 || output.Scenes[index].SuggestedShotMaximum < output.Scenes[index].SuggestedShotMinimum {
			return fmt.Errorf("%w: scene %s has an invalid shot range", ErrInvalidContinuityBlueprint, key)
		}
	}
	edges := make(map[string][]string, len(expectedSceneKeys))
	for _, dependency := range output.Dependencies {
		if !known[dependency.FromSceneKey] || !known[dependency.ToSceneKey] || dependency.FromSceneKey == dependency.ToSceneKey {
			return fmt.Errorf("%w: dependency references an unknown or identical scene", ErrInvalidContinuityBlueprint)
		}
		edges[dependency.FromSceneKey] = append(edges[dependency.FromSceneKey], dependency.ToSceneKey)
	}
	if hasCycle(expectedSceneKeys, edges) {
		return fmt.Errorf("%w: dependency graph contains a cycle", ErrInvalidContinuityBlueprint)
	}
	if err := validateSceneGroups(output.SerialGroups, known, "serial"); err != nil {
		return err
	}
	if err := validateSceneGroups(output.ParallelGroups, known, "parallel"); err != nil {
		return err
	}
	return nil
}

func ParseShotPlannerOutput(raw, expectedSceneKey string, validTimingUnitIDs []string) (ShotPlannerOutput, error) {
	output, err := DecodeShotPlannerOutput(raw, validTimingUnitIDs)
	if err != nil {
		return ShotPlannerOutput{}, err
	}
	if err := ValidateShotPlannerOutput(output, expectedSceneKey, validTimingUnitIDs); err != nil {
		return ShotPlannerOutput{}, err
	}
	return output, nil
}

// DecodeShotPlannerOutput enforces the JSON schema and applies lossless model
// normalization without accepting the model's timing coverage as authority.
// The workflow compiler assigns deterministic slot timing before calling the
// final validator.
func DecodeShotPlannerOutput(raw string, validTimingUnitIDs []string) (ShotPlannerOutput, error) {
	canonicalRaw, err := normalizeShotPlannerJSONShape(raw)
	if err != nil {
		return ShotPlannerOutput{}, err
	}
	var output ShotPlannerOutput
	if err := decodeStrictJSON(canonicalRaw, &output); err != nil {
		return ShotPlannerOutput{}, fmt.Errorf("%w: %v", ErrInvalidShotPlannerOutput, err)
	}
	output = NormalizeShotPlannerOutput(output, validTimingUnitIDs)
	return output, nil
}

// normalizeShotPlannerJSONShape repairs unambiguous model serialization noise
// while preserving strict rejection for every other unknown field. Some
// models attach screenDirection to action or add a descriptive state string
// to scene/character records even though those facts already belong to the
// surrounding ShotState action and locked visual. Conflicting values remain
// invalid, and props[].state is never removed because it is contractual.
func normalizeShotPlannerJSONShape(raw string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return raw, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return raw, nil
	}
	root, ok := document.(map[string]any)
	if !ok {
		return raw, nil
	}
	shots, ok := root["shots"].([]any)
	if !ok {
		return raw, nil
	}
	changed := false
	for shotIndex, item := range shots {
		shot, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, stateField := range []string{"plannedEntryState", "plannedExitState"} {
			state, ok := shot[stateField].(map[string]any)
			if !ok {
				continue
			}
			if scene, ok := state["scene"].(map[string]any); ok {
				if _, exists := scene["state"]; exists {
					delete(scene, "state")
					changed = true
				}
			}
			if characters, ok := state["characters"].([]any); ok {
				for _, characterValue := range characters {
					character, ok := characterValue.(map[string]any)
					if !ok {
						continue
					}
					if _, exists := character["state"]; exists {
						delete(character, "state")
						changed = true
					}
				}
			}
			action, ok := state["action"].(map[string]any)
			if !ok {
				continue
			}
			nested, exists := action["screenDirection"]
			if !exists {
				continue
			}
			if current, exists := state["screenDirection"]; exists && fmt.Sprint(current) != fmt.Sprint(nested) {
				return "", fmt.Errorf("%w: shots[%d].%s has conflicting screenDirection values", ErrInvalidShotPlannerOutput, shotIndex, stateField)
			}
			state["screenDirection"] = nested
			delete(action, "screenDirection")
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize screenDirection: %v", ErrInvalidShotPlannerOutput, err)
	}
	return string(canonical), nil
}

// NormalizeShotPlannerOutput repairs lossless model noise without inventing
// coverage. Unknown unit IDs are dropped only when the same suggestion still
// contains at least one valid unit; an entirely unknown suggestion remains
// invalid and is retried by the workflow.
func NormalizeShotPlannerOutput(output ShotPlannerOutput, validTimingUnitIDs []string) ShotPlannerOutput {
	validUnits := make(map[string]bool, len(validTimingUnitIDs))
	for _, id := range validTimingUnitIDs {
		validUnits[id] = true
	}
	for shotIndex := range output.Shots {
		shot := &output.Shots[shotIndex]
		validReferences := make([]string, 0, len(shot.TimingUnitIDs))
		seenUnits := map[string]bool{}
		for _, id := range shot.TimingUnitIDs {
			id = strings.TrimSpace(id)
			if id == "" || seenUnits[id] || !validUnits[id] {
				continue
			}
			seenUnits[id] = true
			validReferences = append(validReferences, id)
		}
		if len(validReferences) > 0 {
			shot.TimingUnitIDs = validReferences
		}
		if shot.CutAfterTimingUnitID != "" && !validUnits[shot.CutAfterTimingUnitID] {
			shot.CutAfterTimingUnitID = ""
		}
		shot.AssetRequirements = mergeDuplicatePlannerAssetRequirements(shot.AssetRequirements)
		shot.PlannedEntryState = deduplicatePlannerShotState(shot.PlannedEntryState)
		shot.PlannedExitState = deduplicatePlannerShotState(shot.PlannedExitState)
	}
	return output
}

// ShotState is an identity set, not an on-screen instance list. Models may
// repeat a group asset to describe members on both sides of the frame, but
// downstream continuity and reference selection are keyed by canonical asset
// ID. Preserve the first, most prominent placement and keep the richer group
// composition in the shot's visual/action text.
func deduplicatePlannerShotState(state videoproduction.ShotState) videoproduction.ShotState {
	characters := make([]videoproduction.CharacterState, 0, len(state.Characters))
	seenCharacters := make(map[string]bool, len(state.Characters))
	for _, character := range state.Characters {
		assetID := strings.TrimSpace(character.AssetID)
		if assetID != "" && seenCharacters[assetID] {
			continue
		}
		if assetID != "" {
			seenCharacters[assetID] = true
		}
		characters = append(characters, character)
	}
	state.Characters = characters

	props := make([]videoproduction.PropState, 0, len(state.Props))
	seenProps := make(map[string]bool, len(state.Props))
	for _, prop := range state.Props {
		assetID := strings.TrimSpace(prop.AssetID)
		if assetID != "" && seenProps[assetID] {
			continue
		}
		if assetID != "" {
			seenProps[assetID] = true
		}
		props = append(props, prop)
	}
	state.Props = props
	return state
}

func mergeDuplicatePlannerAssetRequirements(items []ShotPlannerAssetRequirement) []ShotPlannerAssetRequirement {
	result := make([]ShotPlannerAssetRequirement, 0, len(items))
	indexByKey := map[string]int{}
	for _, item := range items {
		item.AssetID = strings.TrimSpace(item.AssetID)
		item.RequirementType = strings.TrimSpace(item.RequirementType)
		key := item.AssetID + "\x00" + item.RequirementType
		if index, exists := indexByKey[key]; exists && item.AssetID != "" && item.RequirementType != "" {
			existing := &result[index]
			existing.RoleInShot = firstNonEmptyContract(existing.RoleInShot, item.RoleInShot)
			existing.Costume = firstNonEmptyContract(existing.Costume, item.Costume)
			existing.Pose = firstNonEmptyContract(existing.Pose, item.Pose)
			existing.Expression = firstNonEmptyContract(existing.Expression, item.Expression)
			existing.Action = firstNonEmptyContract(existing.Action, item.Action)
			existing.CameraRelation = firstNonEmptyContract(existing.CameraRelation, item.CameraRelation)
			existing.SceneState = firstNonEmptyContract(existing.SceneState, item.SceneState)
			existing.PropState = firstNonEmptyContract(existing.PropState, item.PropState)
			continue
		}
		indexByKey[key] = len(result)
		result = append(result, item)
	}
	return result
}

func firstNonEmptyContract(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func ValidateShotPlannerOutput(output ShotPlannerOutput, expectedSceneKey string, validTimingUnitIDs []string) error {
	if output.SceneKey != expectedSceneKey || len(output.Shots) == 0 {
		return fmt.Errorf("%w: planner scene key or shots are invalid", ErrInvalidShotPlannerOutput)
	}
	validUnits := make(map[string]bool, len(validTimingUnitIDs))
	for _, id := range validTimingUnitIDs {
		validUnits[id] = true
	}
	seenSuggestions := map[string]bool{}
	for _, shot := range output.Shots {
		if strings.TrimSpace(shot.SuggestionKey) == "" || seenSuggestions[shot.SuggestionKey] || strings.TrimSpace(shot.Visual) == "" || len(shot.TimingUnitIDs) == 0 {
			return fmt.Errorf("%w: each suggestion needs a unique key, visual, and timing units", ErrInvalidShotPlannerOutput)
		}
		seenSuggestions[shot.SuggestionKey] = true
		for _, id := range shot.TimingUnitIDs {
			if !validUnits[id] {
				return fmt.Errorf("%w: suggestion %s references unknown timing unit %s", ErrInvalidShotPlannerOutput, shot.SuggestionKey, id)
			}
		}
		if shot.CutAfterTimingUnitID != "" && !validUnits[shot.CutAfterTimingUnitID] {
			return fmt.Errorf("%w: suggestion %s has an unknown cut point", ErrInvalidShotPlannerOutput, shot.SuggestionKey)
		}
		seenAssets := map[string]bool{}
		for _, requirement := range shot.AssetRequirements {
			assetID := strings.TrimSpace(requirement.AssetID)
			requirementType := strings.TrimSpace(requirement.RequirementType)
			if assetID == "" || requirementType == "" {
				return fmt.Errorf("%w: suggestion %s asset requirements need assetId and requirementType", ErrInvalidShotPlannerOutput, shot.SuggestionKey)
			}
			key := assetID + "\x00" + requirementType
			if seenAssets[key] {
				return fmt.Errorf("%w: suggestion %s contains a duplicate asset requirement", ErrInvalidShotPlannerOutput, shot.SuggestionKey)
			}
			seenAssets[key] = true
		}
	}
	return nil
}

func ValidateShotStatePlannerOutput(output ShotPlannerOutput) error {
	for _, shot := range output.Shots {
		if err := videoproduction.ValidateShotState(shot.PlannedEntryState); err != nil {
			return fmt.Errorf("%w: suggestion %s plannedEntryState: %v", ErrInvalidShotPlannerOutput, shot.SuggestionKey, err)
		}
		if err := videoproduction.ValidateShotState(shot.PlannedExitState); err != nil {
			return fmt.Errorf("%w: suggestion %s plannedExitState: %v", ErrInvalidShotPlannerOutput, shot.SuggestionKey, err)
		}
	}
	return nil
}

func ParseStoryboardReviewerOutput(raw string, validSceneKeys, validTimingUnitIDs []string, shotCount int) (StoryboardReviewerOutput, error) {
	var output StoryboardReviewerOutput
	if err := decodeStrictJSON(raw, &output); err != nil {
		return StoryboardReviewerOutput{}, fmt.Errorf("%w: %v", ErrInvalidStoryboardReview, err)
	}
	if err := ValidateStoryboardReviewerOutput(output, validSceneKeys, validTimingUnitIDs, shotCount); err != nil {
		return StoryboardReviewerOutput{}, err
	}
	return output, nil
}

func ValidateStoryboardReviewerOutput(output StoryboardReviewerOutput, validSceneKeys, validTimingUnitIDs []string, shotCount int) error {
	scenes := stringSet(validSceneKeys)
	units := stringSet(validTimingUnitIDs)
	for _, issue := range output.Issues {
		if strings.TrimSpace(issue.Code) == "" || strings.TrimSpace(issue.Message) == "" || !validReviewSeverity(issue.Severity) {
			return fmt.Errorf("%w: issue code, severity, and message are required", ErrInvalidStoryboardReview)
		}
		if issue.SceneKey != "" && !scenes[issue.SceneKey] {
			return fmt.Errorf("%w: issue references unknown scene", ErrInvalidStoryboardReview)
		}
		if issue.ShotOrdinal != nil && (*issue.ShotOrdinal < 0 || *issue.ShotOrdinal >= shotCount) {
			return fmt.Errorf("%w: issue references unknown shot", ErrInvalidStoryboardReview)
		}
		for _, id := range issue.TimingUnitIDs {
			if !units[id] {
				return fmt.Errorf("%w: issue references unknown timing unit", ErrInvalidStoryboardReview)
			}
		}
	}
	if output.Approved {
		for _, issue := range output.Issues {
			if issue.Severity == "error" || issue.Severity == "critical" {
				return fmt.Errorf("%w: approved review contains blocking issues", ErrInvalidStoryboardReview)
			}
		}
	}
	return nil
}

func decodeStrictJSON(raw string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func validTimingUnitType(value TimingUnitType) bool {
	switch value {
	case UnitDialogue, UnitVoiceover, UnitNarration, UnitSystem, UnitAction, UnitReaction, UnitEstablishing, UnitPause, UnitAmbientHold, UnitTransition:
		return true
	default:
		return false
	}
}

func validTimingTrack(value TimingTrack) bool {
	return value == TrackAudio || value == TrackVisual
}

func canonicalTimingUnitType(value TimingUnitType) TimingUnitType {
	normalized := strings.ToLower(strings.TrimSpace(string(value)))
	switch normalized {
	case "dialogue", "dialog", "speech":
		return UnitDialogue
	case "voiceover", "voice_over", "voice-over", "vo", "internal_monologue", "inner_voice":
		return UnitVoiceover
	case "narration", "narrator":
		return UnitNarration
	case "system", "broadcast", "announcement":
		return UnitSystem
	case "action", "visual_action", "action_beat", "combat":
		return UnitAction
	case "reaction", "reaction_beat":
		return UnitReaction
	case "establishing", "establish", "establishing_shot":
		return UnitEstablishing
	case "pause", "beat", "dramatic_pause":
		return UnitPause
	case "ambient_hold", "ambient", "ambience", "sfx", "sound_effect":
		return UnitAmbientHold
	case "transition", "montage", "flashback", "memory", "memory_montage", "insert", "cutaway":
		return UnitTransition
	default:
		return TimingUnitType(normalized)
	}
}

func canonicalTimingTrack(value TimingTrack, unitType TimingUnitType) TimingTrack {
	normalized := strings.ToLower(strings.TrimSpace(string(value)))
	switch normalized {
	case "audio", "sound", "voice":
		return TrackAudio
	case "visual", "video", "image":
		return TrackVisual
	}
	if isSpeechTimingUnit(unitType) || unitType == UnitSystem || unitType == UnitAmbientHold {
		return TrackAudio
	}
	if validTimingUnitType(unitType) {
		return TrackVisual
	}
	return TimingTrack(normalized)
}

func isSpeechTimingUnit(value TimingUnitType) bool {
	return value == UnitDialogue || value == UnitVoiceover || value == UnitNarration || value == UnitSystem
}

func validateSceneGroups(groups [][]string, known map[string]bool, kind string) error {
	seen := map[string]bool{}
	for _, group := range groups {
		if len(group) == 0 {
			return fmt.Errorf("%w: %s group cannot be empty", ErrInvalidContinuityBlueprint, kind)
		}
		for _, key := range group {
			if !known[key] || seen[key] {
				return fmt.Errorf("%w: %s groups contain an unknown or duplicate scene", ErrInvalidContinuityBlueprint, kind)
			}
			seen[key] = true
		}
	}
	return nil
}

func hasCycle(nodes []string, edges map[string][]string) bool {
	state := make(map[string]uint8, len(nodes))
	var visit func(string) bool
	visit = func(node string) bool {
		switch state[node] {
		case 1:
			return true
		case 2:
			return false
		}
		state[node] = 1
		for _, next := range edges[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = 2
		return false
	}
	ordered := append([]string(nil), nodes...)
	sort.Strings(ordered)
	for _, node := range ordered {
		if visit(node) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func validReviewSeverity(value string) bool {
	switch value {
	case "info", "warning", "error", "critical":
		return true
	default:
		return false
	}
}
