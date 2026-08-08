package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type commerceScriptRebuildActionInput struct {
	ScriptUnitID                string `json:"scriptUnitId"`
	StableOrdinal               int    `json:"stableOrdinal"`
	ExpectedScriptUnitsRevision int64  `json:"expectedScriptUnitsRevision"`
	ImpactToken                 string `json:"impactToken"`
	ExpectedRevision            int64  `json:"expectedRevision"`
}

func (s *Server) executeCommerceScriptRebuildAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(
		ctx, project, arguments, "scriptUnitId", true,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	input := commerceScriptRebuildActionInput{
		ScriptUnitID:     scriptUnitID,
		ImpactToken:      strings.TrimSpace(agentStringArg(arguments, "impactToken")),
		ExpectedRevision: int64(agentIntArg(arguments, "expectedRevision", 0, 1, 1_000_000_000)),
	}
	run, rebuildID, targetHash, replayed, err := s.createCommerceScriptUnitRebuildCore(
		ctx, principal, project, input, command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := "广告脚本换代任务已创建"
	if replayed {
		summary = "广告脚本换代任务已存在，未重复创建"
	}
	return agentToolOK("commerce.script.rebuild", arguments, summary, map[string]any{
		"workflowRunId": run.ID, "workflowRunIds": []string{run.ID},
		"rebuildId": rebuildID, "targetConfigurationHash": targetHash,
		"idempotentReplay": replayed,
	}), nil
}

func (s *Server) createCommerceScriptUnitRebuildCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	input commerceScriptRebuildActionInput,
	idempotencyKey string,
) (WorkflowRun, string, string, bool, error) {
	input.ScriptUnitID = strings.TrimSpace(input.ScriptUnitID)
	input.ImpactToken = strings.TrimSpace(input.ImpactToken)
	if input.ScriptUnitID == "" || input.ImpactToken == "" || input.ExpectedRevision <= 0 {
		return WorkflowRun{}, "", "", false, controlValidationError("脚本、影响令牌或 revision 无效")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return WorkflowRun{}, "", "", false, err
	}
	defer tx.Rollback(ctx)
	execution, err := s.commerceCatalog.ApproveScriptUnitRebuild(
		ctx, tx, project.OrganizationID, project.ID, input.ScriptUnitID,
		input.ImpactToken, input.ExpectedRevision, idempotencyKey,
	)
	if err != nil {
		return WorkflowRun{}, "", "", false, err
	}
	if execution.IdempotentReplay {
		run, err := scanWorkflowRun(tx.QueryRow(
			ctx, workflowRunSelectSQL(`WHERE id = $1`), execution.WorkflowRunID,
		))
		if err != nil {
			return WorkflowRun{}, "", "", false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return WorkflowRun{}, "", "", false, err
		}
		return run, execution.RebuildID, execution.TargetConfigurationHash, true, nil
	}
	run, err := s.enqueueCommercePreparationRunTx(
		ctx, tx, principal, project, execution.PreparationIdentity, idempotencyKey,
	)
	if err != nil {
		return WorkflowRun{}, "", "", false, err
	}
	if err := s.commerceCatalog.AttachScriptUnitRebuildWorkflow(
		ctx, tx, execution.RebuildID, run.ID,
	); err != nil {
		return WorkflowRun{}, "", "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowRun{}, "", "", false, err
	}
	return run, execution.RebuildID, execution.TargetConfigurationHash, false, nil
}
