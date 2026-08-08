package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type scriptGenerateFromSourceActionInput struct {
	SourceID        string   `json:"sourceId,omitempty"`
	ScriptID        string   `json:"scriptId,omitempty"`
	CreateNewScript bool     `json:"createNewScript,omitempty"`
	PlanID          string   `json:"planId,omitempty"`
	Title           string   `json:"title,omitempty"`
	Instruction     string   `json:"instruction,omitempty"`
	ChapterIDs      []string `json:"chapterIds,omitempty"`
	ChapterRange    string   `json:"chapterRange,omitempty"`
	MaxConcurrency  int      `json:"maxConcurrency,omitempty"`
}

type scriptGenerateFromSourceActionResult struct {
	Run              WorkflowRun
	Source           ProjectSource
	SourceID         string
	TargetScriptID   string
	CreateNewScript  bool
	ChapterIDs       []string
	EpisodeCount     int
	MaxConcurrency   int
	WorkflowInput    map[string]any
	IdempotentReplay bool
}

func (s *Server) generateScriptFromSourceCore(ctx context.Context, principal auth.Principal, project Project, input scriptGenerateFromSourceActionInput, scopeTextExtra, commandID, agentTaskID, agentStepID string) (scriptGenerateFromSourceActionResult, error) {
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.Title = strings.TrimSpace(input.Title)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ChapterRange = strings.TrimSpace(input.ChapterRange)
	input.ChapterIDs = uniqueNonEmptyStrings(input.ChapterIDs)
	if input.ScriptID != "" && input.CreateNewScript {
		return scriptGenerateFromSourceActionResult{}, controlValidationError("scriptId 与 createNewScript 不能同时使用")
	}
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 2
	}
	if input.MaxConcurrency < 1 || input.MaxConcurrency > 4 {
		return scriptGenerateFromSourceActionResult{}, controlValidationError("maxConcurrency 必须在 1 到 4 之间")
	}
	sourceID := input.SourceID
	if sourceID == "" {
		if err := s.db.QueryRow(ctx, `
			SELECT id::text FROM project_sources
			WHERE project_id = $1 AND COALESCE(status, 'ready') <> 'archived'
			ORDER BY created_at DESC LIMIT 1
		`, project.ID).Scan(&sourceID); err != nil {
			return scriptGenerateFromSourceActionResult{}, err
		}
	}
	source, err := s.projectSourceContext(ctx, project.ID, sourceID)
	if err != nil {
		return scriptGenerateFromSourceActionResult{}, err
	}
	title := firstNonEmpty(input.Title, source.Title+" Script")
	scopeText := strings.Join([]string{scopeTextExtra, title, input.Instruction, input.ChapterRange}, "\n")
	chapterIDs := input.ChapterIDs
	if source.SourceType == "novel" && len(chapterIDs) == 0 {
		if resolvedSourceID, resolvedChapterIDs, matched, resolveErr := s.resolveNovelChapterRangeScopeContext(ctx, project.ID, sourceID, scopeText); resolveErr != nil {
			return scriptGenerateFromSourceActionResult{}, resolveErr
		} else if matched {
			sourceID, chapterIDs = resolvedSourceID, resolvedChapterIDs
		} else if resolvedSourceID, resolvedChapterIDs, matched, resolveErr := s.resolveNovelChapterScopeContext(ctx, project.ID, sourceID, scopeText); resolveErr != nil {
			return scriptGenerateFromSourceActionResult{}, resolveErr
		} else if matched {
			sourceID, chapterIDs = resolvedSourceID, resolvedChapterIDs
		}
		if sourceID != source.ID {
			source, err = s.projectSourceContext(ctx, project.ID, sourceID)
			if err != nil {
				return scriptGenerateFromSourceActionResult{}, err
			}
		}
	}
	chapterContexts := []scriptNovelChapterContext{}
	if source.SourceType == "novel" {
		if len(chapterIDs) == 0 {
			allChapters, loadErr := s.scriptNovelChaptersContext(ctx, project.ID, sourceID, nil)
			if loadErr != nil {
				return scriptGenerateFromSourceActionResult{}, loadErr
			}
			switch len(allChapters) {
			case 0:
				return scriptGenerateFromSourceActionResult{}, newAPIError(422, "CHAPTER_RANGE_REQUIRED", "当前小说来源没有可生成的分集，请先重新导入或拆分原文。")
			case 1:
				chapterIDs = []string{allChapters[0].ID}
			default:
				return scriptGenerateFromSourceActionResult{}, newAPIError(422, "CHAPTER_RANGE_REQUIRED", "生成小说剧本必须指定 chapterIds 或 chapterRange；一条小说分集只能生成一条剧本分集。当前来源分集数："+intToString(len(allChapters)))
			}
		}
		chapters, loadErr := s.scriptNovelChaptersContext(ctx, project.ID, sourceID, chapterIDs)
		if loadErr != nil {
			return scriptGenerateFromSourceActionResult{}, loadErr
		}
		if len(chapters) != len(uniqueNonEmptyStrings(chapterIDs)) {
			return scriptGenerateFromSourceActionResult{}, controlValidationError("chapterIds 中包含不属于当前小说来源的分集")
		}
		chapterContexts = scriptNovelChapterContexts(chapters)
	}
	commandID = strings.TrimSpace(commandID)
	if commandID != "" {
		existing, found, findErr := s.workflowRunForProjectControlCommand(ctx, project.ID, "source_to_script", commandID)
		if findErr != nil {
			return scriptGenerateFromSourceActionResult{}, findErr
		}
		if found {
			return scriptGenerateFromSourceActionResult{
				Run: existing, Source: source, SourceID: sourceID, TargetScriptID: input.ScriptID,
				CreateNewScript: input.CreateNewScript, ChapterIDs: chapterIDs,
				EpisodeCount: maxInt(1, len(chapterContexts)), MaxConcurrency: input.MaxConcurrency,
				WorkflowInput: map[string]any{}, IdempotentReplay: true,
			}, nil
		}
	}
	workflowInput := map[string]any{
		"sourceId":        sourceID,
		"scriptId":        input.ScriptID,
		"createNewScript": input.CreateNewScript,
		"planId":          strings.TrimSpace(input.PlanID),
		"title":           title,
		"instruction":     input.Instruction,
		"chapterIds":      chapterIDs,
		"maxConcurrency":  input.MaxConcurrency,
	}
	if commandID != "" {
		workflowInput["projectControlCommandId"] = commandID
		workflowInput["idempotencyKey"] = "project-control-command:" + commandID
	} else if strings.TrimSpace(agentTaskID) != "" && strings.TrimSpace(agentStepID) != "" {
		workflowInput["idempotencyKey"] = "agent-step:" + agentTaskID + ":" + agentStepID + ":script.generate_from_source"
	}
	if strings.TrimSpace(agentTaskID) != "" {
		workflowInput["agentTaskId"] = agentTaskID
	}
	if strings.TrimSpace(agentStepID) != "" {
		workflowInput["agentStepId"] = agentStepID
	}
	run, err := s.startProjectWorkflowCore(ctx, principal, project, "source_to_script", workflowInput, workflows.SourceToScriptWorkflow)
	if err != nil {
		return scriptGenerateFromSourceActionResult{}, err
	}
	return scriptGenerateFromSourceActionResult{
		Run: run, Source: source, SourceID: sourceID, TargetScriptID: input.ScriptID,
		CreateNewScript: input.CreateNewScript, ChapterIDs: chapterIDs,
		EpisodeCount: maxInt(1, len(chapterContexts)), MaxConcurrency: input.MaxConcurrency,
		WorkflowInput: workflowInput,
	}, nil
}

func scriptGenerateFromSourceAgentResult(actionName string, raw json.RawMessage, result scriptGenerateFromSourceActionResult) agentToolResult {
	summary := fmt.Sprintf("已启动原文转剧本工作流 %s。", result.Run.ID)
	if result.IdempotentReplay {
		summary = fmt.Sprintf("已存在原文转剧本工作流 %s，未重复启动。", result.Run.ID)
	}
	return agentToolOK(actionName, workflowActionArguments(raw), summary, map[string]any{
		"workflowRunId": result.Run.ID, "workflowType": "source_to_script", "status": result.Run.Status,
		"sourceId": result.SourceID, "sourceType": result.Source.SourceType, "sourceTitle": result.Source.Title,
		"scriptId": result.TargetScriptID, "createNewScript": result.CreateNewScript,
		"chapterIds": result.ChapterIDs, "episodeCount": result.EpisodeCount,
		"maxConcurrency": result.MaxConcurrency, "idempotent": result.IdempotentReplay,
	})
}

func (s *Server) executeScriptGenerateFromSourceAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input scriptGenerateFromSourceActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	userGoal := ""
	if strings.TrimSpace(command.AgentTaskID) != "" {
		_ = s.db.QueryRow(ctx, `SELECT user_goal FROM agent_tasks WHERE id = $1 AND project_id = $2`, command.AgentTaskID, project.ID).Scan(&userGoal)
	}
	result, err := s.generateScriptFromSourceCore(ctx, principal, project, input, userGoal, command.ID, command.AgentTaskID, command.AgentStepID)
	if err != nil {
		return agentToolResult{}, err
	}
	return scriptGenerateFromSourceAgentResult(command.ActionName, raw, result), nil
}
