package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var ErrUnsupportedFixTarget = errors.New("review fix target is not supported")

func ValidateReviewPatch(entityType string, patch map[string]any) error {
	allowed := EditableFieldsForEntity(entityType)
	if len(allowed) == 0 {
		return ErrUnsupportedFixTarget
	}
	for field := range patch {
		if !allowed[field] {
			return fmt.Errorf("field %q is not editable for %s", field, entityType)
		}
	}
	return nil
}

func ApplyReviewPatchPreview(before map[string]any, patch map[string]any) map[string]any {
	after := copyMap(before)
	for field, value := range patch {
		if existing, ok := after[field].(map[string]any); ok {
			if incoming, ok := value.(map[string]any); ok {
				merged := copyMap(existing)
				for nestedKey, nestedValue := range incoming {
					merged[nestedKey] = nestedValue
				}
				after[field] = merged
				continue
			}
		}
		after[field] = value
	}
	return after
}

func SnapshotsEqual(left, right map[string]any) bool {
	var normalizedLeft any
	var normalizedRight any
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	_ = json.Unmarshal(leftRaw, &normalizedLeft)
	_ = json.Unmarshal(rightRaw, &normalizedRight)
	return reflect.DeepEqual(normalizedLeft, normalizedRight)
}

func EditableFieldsForEntity(entityType string) map[string]bool {
	fields := map[string][]string{
		"script_scene": {
			"title", "summary", "location", "timeOfDay", "atmosphere", "characters", "scenes", "props",
			"action", "dialogue", "visualGoal", "emotionalTone", "conflict", "outcome", "content",
		},
		"canonical_asset": {
			"name", "description", "profile", "basePrompt", "consistencyPrompt", "negativePrompt", "lockReference",
		},
		"storyboard_shot": {
			"visual", "camera", "motion", "mood", "durationSeconds", "imagePrompt", "videoPrompt",
		},
		"shot_asset_requirement": {
			"costume", "pose", "expression", "action", "cameraRelation", "sceneState", "propState", "prompt",
		},
		"timeline_clip": {
			"title", "enabled", "trimStartSeconds", "trimEndSeconds", "targetDurationSeconds", "notes",
		},
		"project_timeline": {
			"title", "aspectRatio", "resolution", "metadata",
		},
	}
	allowed := map[string]bool{}
	for _, field := range fields[entityType] {
		allowed[field] = true
	}
	return allowed
}

func SupportedFixTarget(entityType string) bool {
	return len(EditableFieldsForEntity(entityType)) > 0
}

func copyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			out[key] = copyMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}
