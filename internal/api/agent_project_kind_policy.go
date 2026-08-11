package api

import (
	"context"
	"strings"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
)

func (s *Server) commerceAgentPlannerContext(ctx context.Context, project Project) (map[string]any, error) {
	product, err := s.commerceCatalog.GetProduct(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		return nil, err
	}
	references, err := s.commerceCatalog.ListProductReferences(ctx, s.db, project.OrganizationID, project.ID, "active")
	if err != nil {
		return nil, err
	}
	scripts, err := s.commerceCatalog.ListScriptUnits(ctx, s.db, project.OrganizationID, project.ID, "active", "", 50)
	if err != nil {
		return nil, err
	}
	options, optionsErr := s.commerceDirect.Options(ctx, s.db, project.OrganizationID, project.ID)
	jobs, jobsErr := s.commerceDirect.ListJobs(ctx, s.db, project.OrganizationID, project.ID, commercepkg.DirectVideoJobListFilter{Limit: 20})

	scriptSummaries := make([]map[string]any, 0, len(scripts.Items))
	for index, item := range scripts.Items {
		scriptSummaries = append(scriptSummaries, map[string]any{
			"stableOrdinal":           index + 1,
			"id":                      item.ID,
			"unitNo":                  item.UnitNo,
			"title":                   item.Title,
			"status":                  item.Status,
			"targetDurationSeconds":   item.TargetDurationSeconds,
			"targetPlatform":          item.TargetPlatform,
			"languageMode":            item.LanguageMode,
			"revision":                item.Revision,
			"derivedFromScriptUnitId": item.DerivedFromScriptUnitID,
			"derivationKind":          item.DerivationKind,
		})
	}
	jobSummaries := make([]map[string]any, 0)
	if jobsErr == nil {
		for _, item := range jobs {
			jobSummaries = append(jobSummaries, map[string]any{
				"id":              item.ID,
				"scriptUnitId":    item.ScriptUnitID,
				"status":          item.Status,
				"durationSeconds": item.RequestedDurationSeconds,
				"resolution":      item.Resolution,
				"workflowRunId":   item.WorkflowRunID,
				"errorCode":       item.ErrorCode,
			})
		}
	}
	primaryReferenceID := ""
	for _, item := range references {
		if item.IsPrimary {
			primaryReferenceID = item.ID
			break
		}
	}
	out := map[string]any{
		"product": map[string]any{
			"id":                  product.ID,
			"status":              product.Status,
			"revision":            product.Revision,
			"scriptUnitsRevision": product.ScriptUnitsRevision,
			"configured":          product.CurrentVersionID != nil,
		},
		"references": map[string]any{
			"activeCount":        len(references),
			"primaryReferenceId": primaryReferenceID,
		},
		"scripts": map[string]any{
			"activeCount": len(scripts.Items),
			"items":       scriptSummaries,
			"hasMore":     scripts.HasMore,
		},
		"directVideos": map[string]any{
			"items": jobSummaries,
		},
	}
	if optionsErr == nil {
		out["videoOptions"] = commerceAgentVideoOptionsSummary(options)
	} else {
		out["videoOptions"] = map[string]any{"error": optionsErr.Error()}
	}
	if jobsErr != nil {
		out["directVideos"] = map[string]any{"error": jobsErr.Error()}
	}
	return out, nil
}

func commerceAgentVideoOptionsSummary(options commercepkg.DirectVideoOptions) map[string]any {
	resolutions := append([]string(nil), options.Resolutions...)
	durations := append([]int(nil), options.ExecutableDurationSeconds...)
	return map[string]any{
		"defaultDurationSeconds":    options.DefaultDurationSeconds,
		"defaultResolution":         options.DefaultResolution,
		"defaultAspectRatio":        options.DefaultAspectRatio,
		"executableDurationSeconds": durations,
		"resolutions":               resolutions,
		"scriptPromptLimit":         options.ScriptPromptConstraint,
		"routeCount":                len(options.Routes),
		"available":                 len(options.Routes) > 0 && len(durations) > 0 && strings.TrimSpace(options.DefaultResolution) != "",
	}
}
