package videoproduction

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const CodePromptContextLimitExceeded = "PROMPT_CONTEXT_LIMIT_EXCEEDED"

type DialogueCue struct {
	TimingUnitID          string `json:"timingUnitId,omitempty"`
	Speaker               string `json:"speaker,omitempty"`
	Text                  string `json:"text"`
	Delivery              string `json:"delivery,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	StartTick             int64  `json:"startTick,omitempty"`
	EndTick               int64  `json:"endTick,omitempty"`
	ContinuesFromPrevious bool   `json:"continuesFromPrevious,omitempty"`
	ContinuesToNext       bool   `json:"continuesToNext,omitempty"`
}

type AdjacentSceneSummary struct {
	SceneID  string `json:"sceneId,omitempty"`
	Ordinal  int    `json:"ordinal"`
	Relation string `json:"relation"`
	Summary  string `json:"summary"`
}

type PromptLayerBudget struct {
	Allocated int `json:"allocated"`
	Used      int `json:"used"`
}

type PromptBudgetAllocation struct {
	Unit                    string                       `json:"unit"`
	Limit                   int                          `json:"limit"`
	ReservedHardConstraints int                          `json:"reservedHardConstraints"`
	Layers                  map[string]PromptLayerBudget `json:"layers"`
}

type PromptContextPlan struct {
	EpisodeContinuityDigest string                 `json:"episodeContinuityDigest"`
	CurrentSceneScript      string                 `json:"currentSceneScript"`
	AdjacentSceneSummaries  []AdjacentSceneSummary `json:"adjacentSceneSummaries"`
	CurrentShotState        ShotState              `json:"currentShotState"`
	VerbatimDialogueCues    []DialogueCue          `json:"verbatimDialogueCues"`
	ModelContextLimit       int                    `json:"modelContextLimit"`
	ModelPromptLimit        int                    `json:"modelPromptLimit"`
	BudgetAllocation        PromptBudgetAllocation `json:"budgetAllocation"`
	SourceHashes            map[string]string      `json:"sourceHashes"`
	PlanHash                string                 `json:"planHash"`
}

type PromptContextCompileInput struct {
	EpisodeScript           string
	EpisodeContinuityDigest string
	CurrentSceneScript      string
	AdjacentSceneSummaries  []AdjacentSceneSummary
	CurrentShotState        ShotState
	VerbatimDialogueCues    []DialogueCue
	ModelContextLimit       int
	ModelPromptLimit        int
}

func CompilePromptContextPlan(input PromptContextCompileInput) (PromptContextPlan, error) {
	state := NormalizeShotState(input.CurrentShotState)
	stateHash, err := HashShotState(state)
	if err != nil {
		return PromptContextPlan{}, err
	}
	limit := minimumPositive(input.ModelContextLimit, input.ModelPromptLimit)
	if limit <= 0 {
		return PromptContextPlan{}, Error{Code: CodePromptContextLimitExceeded, Message: "模型上下文或 Prompt 上限未配置"}
	}
	dialogue := normalizeDialogueCues(input.VerbatimDialogueCues)
	stateRaw, _ := json.Marshal(state)
	dialogueRaw, _ := json.Marshal(dialogue)
	const fixedContractReserve = 512
	hardUsage := utf8.RuneCount(stateRaw) + utf8.RuneCount(dialogueRaw) + fixedContractReserve
	if hardUsage >= limit {
		return PromptContextPlan{}, Error{Code: CodePromptContextLimitExceeded, Message: fmt.Sprintf("结构化镜头状态和逐字台词需要 %d 字符，超过模型限制 %d", hardUsage, limit)}
	}

	digest := compactWhitespace(input.EpisodeContinuityDigest)
	if digest == "" {
		digest = deterministicEpisodeDigest(input.EpisodeScript, limit/4)
	}
	scene := compactWhitespace(input.CurrentSceneScript)
	adjacent := normalizeAdjacentSummaries(input.AdjacentSceneSummaries)
	available := limit - hardUsage
	sceneBudget := available * 60 / 100
	digestBudget := available * 25 / 100
	adjacentBudget := available - sceneBudget - digestBudget
	usedScene := truncateRunes(scene, sceneBudget)
	usedDigest := truncateRunes(digest, digestBudget)
	usedAdjacent := truncateAdjacentSummaries(adjacent, adjacentBudget)

	// Reuse unspent capacity in priority order: current scene, episode digest,
	// then adjacent summaries. This keeps hard constraints and local facts stable.
	used := runeCount(usedScene) + runeCount(usedDigest) + adjacentRuneCount(usedAdjacent)
	remaining := available - used
	if remaining > 0 && runeCount(usedScene) < runeCount(scene) {
		usedScene = truncateRunes(scene, runeCount(usedScene)+remaining)
	}
	used = runeCount(usedScene) + runeCount(usedDigest) + adjacentRuneCount(usedAdjacent)
	remaining = available - used
	if remaining > 0 && runeCount(usedDigest) < runeCount(digest) {
		usedDigest = truncateRunes(digest, runeCount(usedDigest)+remaining)
	}
	used = runeCount(usedScene) + runeCount(usedDigest) + adjacentRuneCount(usedAdjacent)
	remaining = available - used
	if remaining > 0 && len(usedAdjacent) < len(adjacent) {
		usedAdjacent = truncateAdjacentSummaries(adjacent, adjacentRuneCount(usedAdjacent)+remaining)
	}

	sourceHashes := map[string]string{}
	for key, value := range map[string]any{
		"episodeScript":    compactWhitespace(input.EpisodeScript),
		"continuityDigest": digest,
		"currentScene":     scene,
		"adjacentScenes":   adjacent,
		"shotState":        state,
		"dialogueCues":     dialogue,
	} {
		hash, hashErr := canonicalHash(value)
		if hashErr != nil {
			return PromptContextPlan{}, hashErr
		}
		sourceHashes[key] = hash
	}
	sourceHashes["shotState"] = stateHash
	plan := PromptContextPlan{
		EpisodeContinuityDigest: usedDigest,
		CurrentSceneScript:      usedScene,
		AdjacentSceneSummaries:  usedAdjacent,
		CurrentShotState:        state,
		VerbatimDialogueCues:    dialogue,
		ModelContextLimit:       input.ModelContextLimit,
		ModelPromptLimit:        input.ModelPromptLimit,
		BudgetAllocation: PromptBudgetAllocation{
			Unit:                    "characters",
			Limit:                   limit,
			ReservedHardConstraints: hardUsage,
			Layers: map[string]PromptLayerBudget{
				"currentSceneScript":      {Allocated: sceneBudget, Used: runeCount(usedScene)},
				"episodeContinuityDigest": {Allocated: digestBudget, Used: runeCount(usedDigest)},
				"adjacentSceneSummaries":  {Allocated: adjacentBudget, Used: adjacentRuneCount(usedAdjacent)},
			},
		},
		SourceHashes: sourceHashes,
	}
	hashInput := plan
	hashInput.PlanHash = ""
	plan.PlanHash, err = canonicalHash(hashInput)
	if err != nil {
		return PromptContextPlan{}, err
	}
	return plan, nil
}

func normalizeDialogueCues(values []DialogueCue) []DialogueCue {
	result := make([]DialogueCue, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value.TimingUnitID = strings.TrimSpace(value.TimingUnitID)
		value.Speaker = strings.TrimSpace(value.Speaker)
		value.Text = strings.TrimSpace(value.Text)
		value.Delivery = strings.TrimSpace(value.Delivery)
		value.Kind = enumValue(value.Kind)
		if value.Text == "" {
			continue
		}
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", value.Speaker, value.Text, value.StartTick, value.EndTick)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	if result == nil {
		return []DialogueCue{}
	}
	return result
}

func normalizeAdjacentSummaries(values []AdjacentSceneSummary) []AdjacentSceneSummary {
	result := make([]AdjacentSceneSummary, 0, len(values))
	for _, value := range values {
		value.SceneID = strings.TrimSpace(value.SceneID)
		value.Relation = enumValue(value.Relation)
		value.Summary = compactWhitespace(value.Summary)
		if value.Summary != "" {
			result = append(result, value)
		}
	}
	return result
}

func deterministicEpisodeDigest(script string, budget int) string {
	script = compactWhitespace(script)
	if budget <= 0 || script == "" {
		return ""
	}
	if runeCount(script) <= budget {
		return script
	}
	runes := []rune(script)
	head := budget * 2 / 3
	tail := budget - head
	if head <= 0 || tail <= 0 {
		return truncateRunes(script, budget)
	}
	return strings.TrimSpace(string(runes[:head])) + " … " + strings.TrimSpace(string(runes[len(runes)-tail:]))
}

func truncateAdjacentSummaries(values []AdjacentSceneSummary, budget int) []AdjacentSceneSummary {
	if budget <= 0 {
		return []AdjacentSceneSummary{}
	}
	result := make([]AdjacentSceneSummary, 0, len(values))
	remaining := budget
	for _, value := range values {
		if remaining <= 0 {
			break
		}
		value.Summary = truncateRunes(value.Summary, remaining)
		used := runeCount(value.Summary)
		if used == 0 {
			continue
		}
		result = append(result, value)
		remaining -= used
	}
	return result
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func adjacentRuneCount(values []AdjacentSceneSummary) int {
	total := 0
	for _, value := range values {
		total += runeCount(value.Summary)
	}
	return total
}

func runeCount(value string) int { return utf8.RuneCountInString(value) }

func minimumPositive(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}
