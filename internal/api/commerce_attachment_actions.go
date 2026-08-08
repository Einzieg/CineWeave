package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type commerceAttachmentAssignActionInput struct {
	AttachmentID                string `json:"attachmentId"`
	Scope                       string `json:"scope"`
	ScriptUnitID                string `json:"scriptUnitId"`
	StableOrdinal               int    `json:"stableOrdinal"`
	ExpectedScriptUnitsRevision int64  `json:"expectedScriptUnitsRevision"`
	ReferenceRole               string `json:"referenceRole"`
	SetPrimary                  bool   `json:"setPrimary"`
}

func (s *Server) assignCommerceAttachmentActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	agentTaskID string,
	input commerceAttachmentAssignActionInput,
) (map[string]any, error) {
	input.AttachmentID = strings.TrimSpace(input.AttachmentID)
	input.Scope = strings.TrimSpace(input.Scope)
	input.ScriptUnitID = strings.TrimSpace(input.ScriptUnitID)
	if input.AttachmentID == "" || (input.Scope != "product_common" && input.Scope != "script_custom") {
		return nil, controlValidationError("attachmentId 或图片用途无效")
	}
	if input.Scope == "script_custom" && input.ScriptUnitID == "" {
		return nil, controlValidationError("绑定脚本自定义参考图时必须指定广告脚本")
	}
	if strings.TrimSpace(agentTaskID) != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
			  SELECT 1
			  FROM agent_task_image_attachments
			  WHERE task_id = $1 AND attachment_id = $2
			)
		`, agentTaskID, input.AttachmentID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, apiError{Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_INVALID", Message: "只能绑定当前助手任务附加的图片"}
		}
	}
	attachment, err := loadAgentImageAttachment(
		ctx, tx, project.OrganizationID, project.ID, input.AttachmentID, true,
	)
	if err != nil {
		return nil, err
	}
	if attachment.Status != "completed" {
		return nil, apiError{Status: http.StatusConflict, Code: "AGENT_IMAGE_ATTACHMENT_NOT_READY", Message: "助手图片尚未完成入库"}
	}
	source, err := commercepkg.LoadExistingImageReference(
		ctx, tx, project.OrganizationID, project.ID,
		attachment.ArtifactID, attachment.MediaFileID, attachment.FileName,
	)
	if err != nil {
		return nil, err
	}
	product, err := s.commerceCatalog.GetProduct(ctx, tx, project.OrganizationID, project.ID)
	if err != nil {
		return nil, err
	}
	response := map[string]any{"attachmentId": attachment.ID, "scope": input.Scope}
	if input.Scope == "product_common" {
		item, duplicate, err := s.commerceCatalog.BindExistingProductReference(
			ctx, tx, project.OrganizationID, project.ID, product.ID, source,
			input.ReferenceRole, input.SetPrimary, actorUserID,
		)
		if err != nil {
			return nil, err
		}
		if !duplicate {
			if err := appendCommerceProductReferenceEvent(
				ctx, tx, project.OrganizationID, project.ID, "commerce.product.reference.added", item,
			); err != nil {
				return nil, err
			}
		}
		response["productReference"] = item
		response["productReferenceId"] = item.ID
		response["duplicate"] = duplicate
	} else {
		item, duplicate, err := s.commerceDirect.BindExistingScriptReference(
			ctx, tx, project.OrganizationID, project.ID, product.ID,
			input.ScriptUnitID, source, actorUserID,
		)
		if err != nil {
			return nil, err
		}
		if !duplicate {
			if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID,
				"commerce.script_reference.added", "commerce_script_reference_image", item.ID,
				mustRawJSON(map[string]any{
					"commerceScriptUnitId": item.ScriptUnitID,
					"scriptReferenceId":    item.ID,
					"revision":             item.Revision,
				})); err != nil {
				return nil, err
			}
		}
		response["scriptReference"] = item
		response["scriptReferenceId"] = item.ID
		response["commerceScriptUnitId"] = item.ScriptUnitID
		response["duplicate"] = duplicate
	}
	if strings.TrimSpace(agentTaskID) != "" {
		if err := recordAgentTaskImageAttachmentUsageTx(ctx, tx, agentTaskID, input.AttachmentID, input.Scope); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (s *Server) executeCommerceAttachmentAssignSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	if _, exists := arguments["scriptUnitId"]; exists || agentIntArg(arguments, "stableOrdinal", 0, 0, 1000000) > 0 {
		resolved, err := s.resolveCommerceActionScriptUnitID(ctx, project, arguments, "scriptUnitId", false)
		if err != nil {
			return agentToolResult{}, err
		}
		if resolved != "" {
			arguments["scriptUnitId"] = resolved
		}
	}
	normalized, err := json.Marshal(arguments)
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceAttachmentAssignActionInput](normalized, "助手图片绑定参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	result, err := s.assignCommerceAttachmentActionTx(
		ctx, tx, project, principal.UserID, command.AgentTaskID, input,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("commerce.attachment.assign", arguments, "助手图片已绑定", result), nil
}
