package assetprompts

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CardDraft is the normalized structured result produced by the asset-card
// prompt. It is shared by synchronous API calls and durable Workflow runs.
type CardDraft struct {
	Profile           json.RawMessage `json:"profile"`
	BasePrompt        string          `json:"basePrompt"`
	ConsistencyPrompt string          `json:"consistencyPrompt"`
	NegativePrompt    string          `json:"negativePrompt"`
}

func NormalizeCardDraft(text string) (CardDraft, error) {
	candidate := strings.TrimSpace(text)
	if strings.HasPrefix(candidate, "```") {
		candidate = strings.TrimPrefix(candidate, "```json")
		candidate = strings.TrimPrefix(candidate, "```")
		candidate = strings.TrimSuffix(candidate, "```")
		candidate = strings.TrimSpace(candidate)
	}
	var draft CardDraft
	if err := json.Unmarshal([]byte(candidate), &draft); err != nil {
		return CardDraft{}, err
	}
	if len(draft.Profile) == 0 {
		draft.Profile = json.RawMessage(`{}`)
	}
	var profile map[string]any
	if err := json.Unmarshal(draft.Profile, &profile); err != nil {
		return CardDraft{}, fmt.Errorf("profile must be a JSON object")
	}
	normalized, err := json.Marshal(profile)
	if err != nil {
		return CardDraft{}, err
	}
	draft.Profile = normalized
	draft.BasePrompt = strings.TrimSpace(draft.BasePrompt)
	draft.ConsistencyPrompt = strings.TrimSpace(draft.ConsistencyPrompt)
	draft.NegativePrompt = strings.TrimSpace(draft.NegativePrompt)
	if draft.BasePrompt == "" || draft.ConsistencyPrompt == "" {
		return CardDraft{}, fmt.Errorf("basePrompt and consistencyPrompt are required")
	}
	return draft, nil
}
