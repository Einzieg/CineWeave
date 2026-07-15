package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Einzieg/cineweave/internal/provider"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
)

const (
	promptKeyStoryboardTimingBatchAnalyzer = "storyboard_timing_batch_analyzer"
	timingBatchMaximumRunes                = 3_500
	timingBatchConcurrency                 = 3
)

type timingTextBatch struct {
	Key               string   `json:"key"`
	Ordinal           int      `json:"ordinal"`
	SceneOrdinal      int      `json:"sceneOrdinal"`
	Title             string   `json:"title"`
	Content           string   `json:"content"`
	SourceStartOffset int      `json:"sourceStartOffset"`
	SourceEndOffset   int      `json:"sourceEndOffset"`
	SourceHash        string   `json:"sourceHash"`
	ScriptSceneIDs    []string `json:"scriptSceneIds,omitempty"`
}

type timingBatchActivityOutput struct {
	BatchKey          string                             `json:"batchKey"`
	BatchOrdinal      int                                `json:"batchOrdinal"`
	SourceStart       int                                `json:"sourceStartOffset"`
	SourceEnd         int                                `json:"sourceEndOffset"`
	SourceHash        string                             `json:"sourceHash"`
	Semantic          storyboardpkg.TimingAnalyzerOutput `json:"semantic"`
	ProviderCallID    string                             `json:"providerCallId,omitempty"`
	ModelID           string                             `json:"modelId,omitempty"`
	PromptVersionID   string                             `json:"promptVersionId,omitempty"`
	PromptHash        string                             `json:"promptHash,omitempty"`
	PromptTemplateKey string                             `json:"promptTemplateKey,omitempty"`
}

type timingAnalysisProvenance struct {
	ProviderCallIDs  []string                    `json:"providerCallIds,omitempty"`
	ModelIDs         []string                    `json:"modelIds,omitempty"`
	PromptVersionIDs []string                    `json:"promptVersionIds,omitempty"`
	PromptHashes     []string                    `json:"promptHashes,omitempty"`
	Batches          []timingBatchActivityOutput `json:"batches"`
}

func splitEpisodeTimingBatches(content string, scenes []ScriptSceneRecord) []timingTextBatch {
	runes := []rune(content)
	if len(runes) == 0 {
		return nil
	}
	sectionStarts := markdownHeadingOffsets(runes, 2)
	if len(sectionStarts) == 0 {
		sectionStarts = markdownHeadingOffsets(runes, 3)
	}
	if len(sectionStarts) == 0 {
		sectionStarts = []int{0}
	}
	sections := make([][2]int, 0, len(sectionStarts))
	for index, start := range sectionStarts {
		end := len(runes)
		if index+1 < len(sectionStarts) {
			end = sectionStarts[index+1]
		}
		if strings.TrimSpace(string(runes[start:end])) != "" {
			sections = append(sections, [2]int{start, end})
		}
	}
	if len(sections) == 0 {
		sections = append(sections, [2]int{0, len(runes)})
	}

	batches := make([]timingTextBatch, 0, len(sections))
	for sceneOrdinal, section := range sections {
		parts := splitTimingTextRange(runes, section[0], section[1], timingBatchMaximumRunes)
		for _, part := range parts {
			batchContent := string(runes[part[0]:part[1]])
			batch := timingTextBatch{
				Ordinal:           len(batches),
				SceneOrdinal:      sceneOrdinal,
				Title:             timingBatchTitle(batchContent, sceneOrdinal),
				Content:           batchContent,
				SourceStartOffset: part[0],
				SourceEndOffset:   part[1],
				SourceHash:        timingSourceHash(batchContent),
			}
			hashKey := strings.TrimPrefix(batch.SourceHash, "sha256:")
			batch.Key = fmt.Sprintf("batch_%03d_%s", batch.Ordinal, hashKey[:12])
			switch {
			case len(sections) == 1 && len(scenes) > 0:
				batch.ScriptSceneIDs = make([]string, 0, len(scenes))
				for _, scene := range scenes {
					batch.ScriptSceneIDs = append(batch.ScriptSceneIDs, scene.ID)
				}
			case sceneOrdinal < len(scenes):
				batch.ScriptSceneIDs = []string{scenes[sceneOrdinal].ID}
			}
			batches = append(batches, batch)
		}
	}
	return batches
}

func markdownHeadingOffsets(content []rune, level int) []int {
	if level < 1 {
		return nil
	}
	prefix := strings.Repeat("#", level) + " "
	result := make([]int, 0)
	lineStart := 0
	for lineStart < len(content) {
		lineEnd := lineStart
		for lineEnd < len(content) && content[lineEnd] != '\n' {
			lineEnd++
		}
		line := strings.TrimLeft(string(content[lineStart:lineEnd]), " \t")
		if strings.HasPrefix(line, prefix) && !strings.HasPrefix(line, "#"+prefix) {
			result = append(result, lineStart)
		}
		lineStart = lineEnd + 1
	}
	return result
}

func splitTimingTextRange(content []rune, start, end, maximum int) [][2]int {
	if maximum <= 0 || end-start <= maximum {
		return [][2]int{{start, end}}
	}
	result := make([][2]int, 0, (end-start)/maximum+1)
	cursor := start
	for end-cursor > maximum {
		target := cursor + maximum
		split := lastParagraphBoundary(content, cursor+maximum/2, target)
		if split <= cursor {
			split = nextParagraphBoundary(content, target, timingMinInt(end, target+maximum/3))
		}
		if split <= cursor || split >= end {
			split = target
		}
		result = append(result, [2]int{cursor, split})
		cursor = split
	}
	if cursor < end {
		if len(result) > 0 && end-cursor < maximum/4 && end-result[len(result)-1][0] <= maximum+maximum/3 {
			result[len(result)-1][1] = end
		} else {
			result = append(result, [2]int{cursor, end})
		}
	}
	return result
}

func lastParagraphBoundary(content []rune, start, end int) int {
	for index := timingMinInt(end, len(content)-1); index > start; index-- {
		if index >= 2 && content[index-1] == '\n' && content[index-2] == '\n' {
			return index
		}
	}
	return -1
}

func nextParagraphBoundary(content []rune, start, end int) int {
	for index := maxInt(start, 2); index < end && index < len(content); index++ {
		if content[index-1] == '\n' && content[index-2] == '\n' {
			return index
		}
	}
	return -1
}

func timingBatchTitle(content string, sceneOrdinal int) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			return line
		}
	}
	return fmt.Sprintf("场景段 %d", sceneOrdinal+1)
}

func timingSourceHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (a Activities) analyzeEpisodeTimingBatches(
	ctx context.Context,
	input AnalyzeEpisodeTimingInput,
	project ProjectProductionSettings,
	episode ScriptStoryboardEpisodeRecord,
	scenes []ScriptSceneRecord,
	parentNodeExecution NodeExecution,
) ([]timingBatchActivityOutput, error) {
	batches := splitEpisodeTimingBatches(episode.Content, scenes)
	if len(batches) == 0 {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "script episode has no analyzable content", Retryable: false, RetryabilityKnown: true}
	}
	workerCount := timingBatchConcurrency
	if workerCount > len(batches) {
		workerCount = len(batches)
	}
	type batchResult struct {
		ordinal int
		output  timingBatchActivityOutput
		err     error
	}
	jobs := make(chan timingTextBatch)
	results := make(chan batchResult, len(batches))
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batch := range jobs {
				output, err := a.analyzeEpisodeTimingBatch(ctx, input, project, episode, batch, len(batches))
				results <- batchResult{ordinal: batch.Ordinal, output: output, err: err}
			}
		}()
	}
	go func() {
		for _, batch := range batches {
			jobs <- batch
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	outputs := make([]timingBatchActivityOutput, len(batches))
	completed := 0
	failed := 0
	var firstErr error
	for result := range results {
		if result.err != nil {
			failed++
			if firstErr == nil {
				firstErr = result.err
			}
		} else {
			outputs[result.ordinal] = result.output
			completed++
		}
		_ = ProgressNodeRun(ctx, a.db, parentNodeExecution, mustJSON(map[string]any{
			"status":           "analyzing_batches",
			"batchCount":       len(batches),
			"completedBatches": completed,
			"failedBatches":    failed,
		}))
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return outputs, nil
}

func (a Activities) analyzeEpisodeTimingBatch(
	ctx context.Context,
	input AnalyzeEpisodeTimingInput,
	project ProjectProductionSettings,
	episode ScriptStoryboardEpisodeRecord,
	batch timingTextBatch,
	batchCount int,
) (timingBatchActivityOutput, error) {
	contextJSON := mustJSON(map[string]any{
		"project": map[string]any{
			"timelineTimebase": project.TimelineTimebase,
			"fpsNumerator":     project.FPSNumerator,
			"fpsDenominator":   project.FPSDenominator,
		},
		"episode": map[string]any{
			"id":      episode.ID,
			"index":   episode.EpisodeIndex,
			"title":   episode.EpisodeTitle,
			"content": batch.Content,
		},
		"analysisScope": map[string]any{
			"batchKey":          batch.Key,
			"batchOrdinal":      batch.Ordinal,
			"batchCount":        batchCount,
			"sourceStartOffset": batch.SourceStartOffset,
			"sourceEndOffset":   batch.SourceEndOffset,
			"sourceHash":        batch.SourceHash,
			"scriptSceneIds":    batch.ScriptSceneIDs,
		},
	})
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardTimingBatchAnalyzer, map[string]any{
		"context": map[string]any{"json": string(contextJSON)},
	})
	if err != nil {
		return timingBatchActivityOutput{}, err
	}
	if existing, ok, err := a.existingTimingBatchOutput(ctx, input.WorkflowRunID, input.ScriptEpisodeID, batch, rendered.RenderedHash); err != nil {
		return timingBatchActivityOutput{}, err
	} else if ok {
		return existing, nil
	}

	nodeKey := fmt.Sprintf("%s_%s", nodeKeyForID(nodeAnalyzeEpisodeTimingPrefix, input.ScriptEpisodeID), batch.Key)
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "agent.storyboard_timing_batch_analyze",
		Input: mustJSON(map[string]any{
			"scriptEpisodeId":   input.ScriptEpisodeID,
			"batchKey":          batch.Key,
			"batchOrdinal":      batch.Ordinal,
			"batchCount":        batchCount,
			"sourceStartOffset": batch.SourceStartOffset,
			"sourceEndOffset":   batch.SourceEndOffset,
			"sourceHash":        batch.SourceHash,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
		}),
	})
	if err != nil {
		return timingBatchActivityOutput{}, err
	}
	fail := func(cause error) (timingBatchActivityOutput, error) {
		code, message := workflowErrorFields(cause, codeActivityFailed)
		persistCtx, cancel := workflowFailurePersistenceContext(ctx)
		defer cancel()
		_ = FailNodeRun(persistCtx, a.db, nodeExecution, code, message)
		return timingBatchActivityOutput{}, cause
	}

	gatewayResp, err := a.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeExecution.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustJSON(map[string]any{
			"prompt":          rendered.RenderedText,
			"responseFormat":  "json",
			"maxOutputTokens": 16_000,
		}),
		Options: providerTextGatewayOptions(),
	})
	if err != nil {
		return fail(workflowErrorFromProvider(err, codeActivityFailed))
	}
	semantic, err := storyboardpkg.DecodeTimingAnalyzerOutput(stripJSONFence(gatewayResp.Output.Text))
	if err != nil {
		return fail(workflowError{Code: "TIMING_BATCH_OUTPUT_INVALID", Message: err.Error(), Retryable: true, RetryabilityKnown: true})
	}
	semantic = canonicalizeTimingBatchOutput(semantic, episode.EpisodeIndex, batch)
	if err := storyboardpkg.ValidateTimingAnalyzerOutput(semantic); err != nil {
		return fail(workflowError{Code: "TIMING_BATCH_OUTPUT_INVALID", Message: err.Error(), Retryable: true, RetryabilityKnown: true})
	}
	if _, err := storyboardpkg.AnalyzeSemanticTiming(semantic, storyboardpkg.AnalyzeTimingOptions{
		Timebase: storyboardpkg.Timebase{
			TicksPerSecond: project.TimelineTimebase,
			FPSNumerator:   int64(project.FPSNumerator),
			FPSDenominator: int64(project.FPSDenominator),
		},
		EpisodeContent: batch.Content,
	}); err != nil {
		return fail(workflowError{Code: "TIMING_BATCH_SOURCE_MISMATCH", Message: err.Error(), Retryable: true, RetryabilityKnown: true})
	}
	output := timingBatchActivityOutput{
		BatchKey:          batch.Key,
		BatchOrdinal:      batch.Ordinal,
		SourceStart:       batch.SourceStartOffset,
		SourceEnd:         batch.SourceEndOffset,
		SourceHash:        batch.SourceHash,
		Semantic:          semantic,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptTemplateKey: rendered.TemplateKey,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeExecution, mustJSON(output)); err != nil {
		return timingBatchActivityOutput{}, err
	}
	return output, nil
}

func canonicalizeTimingBatchOutput(output storyboardpkg.TimingAnalyzerOutput, episodeIndex int, batch timingTextBatch) storyboardpkg.TimingAnalyzerOutput {
	output = storyboardpkg.NormalizeTimingAnalyzerOutput(output)
	allowedSceneIDs := make(map[string]bool, len(batch.ScriptSceneIDs))
	for _, sceneID := range batch.ScriptSceneIDs {
		allowedSceneIDs[strings.TrimSpace(sceneID)] = true
	}
	unitOrdinal := 0
	for sceneIndex := range output.Scenes {
		scene := &output.Scenes[sceneIndex]
		scene.SceneKey = fmt.Sprintf("ep%03d_b%03d_s%03d", maxInt(episodeIndex, 1), batch.Ordinal, sceneIndex)
		scene.SceneOrdinal = sceneIndex
		if !allowedSceneIDs[strings.TrimSpace(scene.ScriptSceneID)] {
			scene.ScriptSceneID = ""
		}
		if scene.ScriptSceneID == "" && sceneIndex < len(batch.ScriptSceneIDs) {
			scene.ScriptSceneID = batch.ScriptSceneIDs[sceneIndex]
		}
		for index := range scene.Units {
			unit := &scene.Units[index]
			unit.UnitKey = fmt.Sprintf("ep%03d_b%03d_u%04d", maxInt(episodeIndex, 1), batch.Ordinal, unitOrdinal)
			unit.UnitOrdinal = unitOrdinal
			unit.SourceStartOffset = nil
			unit.SourceEndOffset = nil
			if strings.TrimSpace(unit.ParallelGroup) != "" {
				unit.ParallelGroup = fmt.Sprintf("b%03d_%s", batch.Ordinal, strings.TrimSpace(unit.ParallelGroup))
			}
			unitOrdinal++
		}
	}
	return output
}

func mergeTimingBatchOutputs(outputs []timingBatchActivityOutput, episodeIndex int) (storyboardpkg.TimingAnalyzerOutput, timingAnalysisProvenance, error) {
	sorted := append([]timingBatchActivityOutput(nil), outputs...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].BatchOrdinal < sorted[right].BatchOrdinal })
	merged := storyboardpkg.TimingAnalyzerOutput{Scenes: make([]storyboardpkg.TimingAnalyzerScene, 0)}
	provenance := timingAnalysisProvenance{Batches: sorted}
	unitOrdinal := 0
	for _, batch := range sorted {
		if len(batch.Semantic.Scenes) == 0 {
			return storyboardpkg.TimingAnalyzerOutput{}, timingAnalysisProvenance{}, fmt.Errorf("timing batch %s has no scenes", batch.BatchKey)
		}
		provenance.ProviderCallIDs = appendUniqueString(provenance.ProviderCallIDs, batch.ProviderCallID)
		provenance.ModelIDs = appendUniqueString(provenance.ModelIDs, batch.ModelID)
		provenance.PromptVersionIDs = appendUniqueString(provenance.PromptVersionIDs, batch.PromptVersionID)
		provenance.PromptHashes = appendUniqueString(provenance.PromptHashes, batch.PromptHash)
		for _, sourceScene := range batch.Semantic.Scenes {
			scene := sourceScene
			scene.SceneOrdinal = len(merged.Scenes)
			scene.SceneKey = fmt.Sprintf("ep%03d_scene_%03d", maxInt(episodeIndex, 1), scene.SceneOrdinal)
			for index := range scene.Units {
				scene.Units[index].UnitOrdinal = unitOrdinal
				scene.Units[index].UnitKey = fmt.Sprintf("ep%03d_u%04d", maxInt(episodeIndex, 1), unitOrdinal)
				scene.Units[index].SourceStartOffset = nil
				scene.Units[index].SourceEndOffset = nil
				unitOrdinal++
			}
			merged.Scenes = append(merged.Scenes, scene)
		}
	}
	if err := storyboardpkg.ValidateTimingAnalyzerOutput(merged); err != nil {
		return storyboardpkg.TimingAnalyzerOutput{}, timingAnalysisProvenance{}, err
	}
	return merged, provenance, nil
}

func (a Activities) existingTimingBatchOutput(ctx context.Context, workflowRunID, scriptEpisodeID string, batch timingTextBatch, promptHash string) (timingBatchActivityOutput, bool, error) {
	nodeKey := fmt.Sprintf("%s_%s", nodeKeyForID(nodeAnalyzeEpisodeTimingPrefix, scriptEpisodeID), batch.Key)
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_key = $2
		  AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return timingBatchActivityOutput{}, false, nil
	}
	if err != nil {
		return timingBatchActivityOutput{}, false, err
	}
	var output timingBatchActivityOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return timingBatchActivityOutput{}, false, err
	}
	if output.SourceHash != batch.SourceHash || output.PromptHash != promptHash || len(output.Semantic.Scenes) == 0 {
		return timingBatchActivityOutput{}, false, nil
	}
	return output, true, nil
}

func firstTimingPromptVersion(provenance timingAnalysisProvenance) string {
	if len(provenance.PromptVersionIDs) == 0 {
		return ""
	}
	return provenance.PromptVersionIDs[0]
}

func firstTimingPromptHash(provenance timingAnalysisProvenance) string {
	if len(provenance.PromptHashes) == 0 {
		return ""
	}
	return provenance.PromptHashes[0]
}

func firstTimingProviderCall(provenance timingAnalysisProvenance) string {
	if len(provenance.ProviderCallIDs) == 0 {
		return ""
	}
	return provenance.ProviderCallIDs[0]
}

func firstTimingModel(provenance timingAnalysisProvenance) string {
	if len(provenance.ModelIDs) == 0 {
		return ""
	}
	return provenance.ModelIDs[0]
}

func timingMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
