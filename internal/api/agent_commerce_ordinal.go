package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	sourceutil "github.com/Einzieg/cineweave/internal/sources"
)

var commerceScriptOrdinalPattern = regexp.MustCompile(
	`第\s*([0-9０-９零〇一二两三四五六七八九十百千万壹贰叁肆伍陆柒捌玖拾佰仟]+)\s*(?:条|个)\s*(?:广告)?脚本`,
)

var commerceScriptBareOrdinalPattern = regexp.MustCompile(
	`(?:^|[\s，,。；;：:])第\s*([0-9０-９零〇一二两三四五六七八九十百千万壹贰叁肆伍陆柒捌玖拾佰仟]+)\s*(?:条|个)(?:$|[\s，,。；;：:])`,
)

var commerceScriptTargetArgument = map[string]string{
	"commerce.script.get":            "scriptUnitId",
	"commerce.script.update":         "scriptUnitId",
	"commerce.script.archive":        "scriptUnitId",
	"commerce.script.derive.preview": "sourceScriptUnitId",
	"commerce.script.derive.batch":   "sourceScriptUnitId",
	"commerce.video.options":         "scriptUnitId",
	"commerce.video.generate":        "scriptUnitId",
}

func commerceScriptOrdinalFromGoal(goal string) int {
	goal = strings.TrimSpace(goal)
	for _, pattern := range []*regexp.Regexp{commerceScriptOrdinalPattern, commerceScriptBareOrdinalPattern} {
		match := pattern.FindStringSubmatch(goal)
		if len(match) == 2 {
			return sourceutil.ParseOrdinalNumber(match[1])
		}
	}
	return 0
}

type commerceAgentScriptSelection struct {
	ScriptUnitID        string
	StableOrdinal       int
	ScriptUnitsRevision int64
}

func (s *Server) normalizeAndValidateAgentPlanProjectContext(
	ctx context.Context,
	project Project,
	task AgentTask,
	plan *agent.Plan,
) error {
	if !project.ProjectKind.IsCommerce() || plan == nil {
		return nil
	}
	goalOrdinal := commerceScriptOrdinalFromGoal(task.UserGoal)
	var scripts *commercepkg.ScriptUnitList
	for index := range plan.Steps {
		step := &plan.Steps[index]
		if step.Tool == "agent.ask_user" || step.Tool == "commerce.script.list" {
			continue
		}
		argumentName, targetsScript := commerceScriptTargetArgument[step.Tool]
		if !targetsScript && step.Tool != "commerce.attachment.assign" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal(step.Args, &args); err != nil {
			return fmt.Errorf("invalid args for %s: %w", step.Tool, err)
		}
		if step.Tool == "commerce.attachment.assign" {
			if strings.TrimSpace(stringValueFromAny(args["scope"])) != "script_custom" {
				continue
			}
			argumentName = "scriptUnitId"
		}
		if scripts == nil {
			loaded, err := s.listAgentCommerceScriptsForSelection(ctx, project)
			if err != nil {
				return err
			}
			scripts = &loaded
		}
		if _, err := resolveCommerceAgentScriptSelection(args, argumentName, goalOrdinal, *scripts); err != nil {
			return fmt.Errorf("%s 脚本选择无效: %w", step.Tool, err)
		}
		step.Args = mustRawJSON(args)
	}
	return nil
}

func (s *Server) normalizeAgentCommerceScriptArgs(
	ctx context.Context,
	project Project,
	task AgentTask,
	toolName string,
	args map[string]any,
) error {
	argumentName, targetsScript := commerceScriptTargetArgument[toolName]
	if !targetsScript && toolName != "commerce.attachment.assign" {
		return nil
	}
	if toolName == "commerce.attachment.assign" {
		if strings.TrimSpace(stringValueFromAny(args["scope"])) != "script_custom" {
			return nil
		}
		argumentName = "scriptUnitId"
	}
	scripts, err := s.listAgentCommerceScriptsForSelection(ctx, project)
	if err != nil {
		return err
	}
	_, err = resolveCommerceAgentScriptSelection(
		args,
		argumentName,
		commerceScriptOrdinalFromGoal(task.UserGoal),
		scripts,
	)
	if err != nil {
		return newAPIError(
			409,
			"COMMERCE_SCRIPT_SELECTION_STALE",
			"广告脚本列表已变化或选择无效，请重新读取脚本列表后重试",
		)
	}
	return nil
}

func (s *Server) listAgentCommerceScriptsForSelection(
	ctx context.Context,
	project Project,
) (commercepkg.ScriptUnitList, error) {
	const maxItems = 1000
	result := commercepkg.ScriptUnitList{Items: make([]commercepkg.ScriptUnit, 0, 100)}
	cursor := ""
	for len(result.Items) < maxItems {
		page, err := s.commerceCatalog.ListScriptUnits(
			ctx,
			s.db,
			project.OrganizationID,
			project.ID,
			"active",
			cursor,
			100,
		)
		if err != nil {
			return commercepkg.ScriptUnitList{}, err
		}
		if result.ScriptUnitsRevision == 0 {
			result.ScriptUnitsRevision = page.ScriptUnitsRevision
		} else if page.ScriptUnitsRevision != result.ScriptUnitsRevision {
			return commercepkg.ScriptUnitList{}, fmt.Errorf("广告脚本列表在读取过程中发生变化，请重试")
		}
		result.Items = append(result.Items, page.Items...)
		if !page.HasMore || strings.TrimSpace(page.NextCursor) == "" {
			result.HasMore = false
			return result, nil
		}
		cursor = page.NextCursor
	}
	result.HasMore = true
	result.NextCursor = cursor
	return result, nil
}

func resolveCommerceAgentScriptSelection(
	args map[string]any,
	argumentName string,
	goalOrdinal int,
	scripts commercepkg.ScriptUnitList,
) (commerceAgentScriptSelection, error) {
	explicitOrdinal := agentIntArg(args, "stableOrdinal", 0, 0, 1000000)
	if goalOrdinal > 0 && explicitOrdinal > 0 && goalOrdinal != explicitOrdinal {
		return commerceAgentScriptSelection{}, fmt.Errorf(
			"用户指定第 %d 条脚本，但工具参数选择了第 %d 条",
			goalOrdinal,
			explicitOrdinal,
		)
	}
	ordinal := explicitOrdinal
	if goalOrdinal > 0 {
		ordinal = goalOrdinal
	}
	expectedRevision := agentInt64Value(args["expectedScriptUnitsRevision"])
	if expectedRevision > 0 &&
		scripts.ScriptUnitsRevision > 0 &&
		expectedRevision != scripts.ScriptUnitsRevision {
		return commerceAgentScriptSelection{}, fmt.Errorf(
			"脚本集合版本已从 %d 变为 %d",
			expectedRevision,
			scripts.ScriptUnitsRevision,
		)
	}

	if ordinal > 0 {
		if ordinal > len(scripts.Items) {
			return commerceAgentScriptSelection{}, fmt.Errorf(
				"无法按稳定排序定位第 %d 条活动脚本；请重新列出脚本，仍无法确定时询问用户",
				ordinal,
			)
		}
		selected := scripts.Items[ordinal-1]
		return applyCommerceAgentScriptSelection(args, argumentName, selected.ID, ordinal, scripts.ScriptUnitsRevision), nil
	}

	actualID := strings.TrimSpace(stringValueFromAny(args[argumentName]))
	if actualID == "" {
		return commerceAgentScriptSelection{}, fmt.Errorf("缺少广告脚本 ID 或 stableOrdinal")
	}
	for index, item := range scripts.Items {
		if strings.TrimSpace(item.ID) == actualID {
			return applyCommerceAgentScriptSelection(args, argumentName, item.ID, index+1, scripts.ScriptUnitsRevision), nil
		}
	}
	return commerceAgentScriptSelection{}, fmt.Errorf("脚本 %s 不在当前活动脚本列表中", actualID)
}

func applyCommerceAgentScriptSelection(
	args map[string]any,
	argumentName string,
	scriptUnitID string,
	stableOrdinal int,
	scriptUnitsRevision int64,
) commerceAgentScriptSelection {
	args[argumentName] = strings.TrimSpace(scriptUnitID)
	args["stableOrdinal"] = stableOrdinal
	if scriptUnitsRevision > 0 {
		args["expectedScriptUnitsRevision"] = scriptUnitsRevision
	}
	return commerceAgentScriptSelection{
		ScriptUnitID:        strings.TrimSpace(scriptUnitID),
		StableOrdinal:       stableOrdinal,
		ScriptUnitsRevision: scriptUnitsRevision,
	}
}

func agentInt64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
