package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	sourceutil "github.com/Einzieg/cineweave/internal/sources"
)

var commerceScriptOrdinalPattern = regexp.MustCompile(
	`第\s*([0-9０-９零〇一二两三四五六七八九十百千万壹贰叁肆伍陆柒捌玖拾佰仟]+)\s*(?:条|个)\s*(?:广告)?脚本`,
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
	match := commerceScriptOrdinalPattern.FindStringSubmatch(strings.TrimSpace(goal))
	if len(match) != 2 {
		return 0
	}
	return sourceutil.ParseOrdinalNumber(match[1])
}

func (s *Server) validateAgentPlanProjectContext(
	ctx context.Context,
	project Project,
	task AgentTask,
	plan agent.Plan,
) error {
	if !project.ProjectKind.IsCommerce() {
		return nil
	}
	ordinal := commerceScriptOrdinalFromGoal(task.UserGoal)
	if ordinal <= 0 {
		return nil
	}
	scripts, err := s.commerceCatalog.ListScriptUnits(
		ctx,
		s.db,
		project.OrganizationID,
		project.ID,
		"active",
		"",
		100,
	)
	if err != nil {
		return err
	}
	return validateCommerceScriptOrdinalPlan(plan, ordinal, scripts.Items)
}

func validateCommerceScriptOrdinalPlan(
	plan agent.Plan,
	ordinal int,
	scripts []commercepkg.ScriptUnit,
) error {
	if ordinal <= 0 {
		return nil
	}
	expectedID := ""
	if ordinal <= len(scripts) {
		expectedID = strings.TrimSpace(scripts[ordinal-1].ID)
	}
	for _, step := range plan.Steps {
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
		actualID := strings.TrimSpace(stringValueFromAny(args[argumentName]))
		if expectedID == "" {
			return fmt.Errorf(
				"无法按稳定排序定位第 %d 条活动脚本；请先使用 commerce.script.list，仍无法唯一确定时使用 agent.ask_user",
				ordinal,
			)
		}
		if actualID != expectedID {
			return fmt.Errorf(
				"第 %d 条活动脚本的稳定 ID 是 %s，但 %s.%s 使用了 %s",
				ordinal,
				expectedID,
				step.Tool,
				argumentName,
				firstNonEmpty(actualID, "<empty>"),
			)
		}
	}
	return nil
}
