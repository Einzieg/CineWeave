package api

import (
	"context"
	"net/http"
	"strings"
)

type finalVideoGateState struct {
	VersionID           string
	NativeAudioStatus   string
	ProductionReadiness string
}

func (s *Server) requireFinalVideoProductionReady(ctx context.Context, projectID, versionID string) (finalVideoGateState, error) {
	var state finalVideoGateState
	err := s.db.QueryRow(ctx, `
		SELECT version.id::text, version.native_audio_status, version.production_readiness
		FROM final_video_versions version
		JOIN projects project ON project.id = version.project_id
		WHERE version.project_id = $1
		  AND ((NULLIF($2, '') IS NOT NULL AND version.id = NULLIF($2, '')::uuid)
		       OR (NULLIF($2, '') IS NULL AND (version.id = project.active_final_video_version_id OR project.active_final_video_version_id IS NULL)))
		ORDER BY CASE WHEN version.id = project.active_final_video_version_id THEN 0 WHEN version.status = 'active' THEN 1 WHEN version.status = 'ready' THEN 2 ELSE 3 END,
		         version.version DESC, version.created_at DESC
		LIMIT 1
	`, projectID, strings.TrimSpace(versionID)).Scan(&state.VersionID, &state.NativeAudioStatus, &state.ProductionReadiness)
	if err != nil {
		return finalVideoGateState{}, err
	}
	if state.ProductionReadiness != "ready" {
		return state, apiError{
			Status: http.StatusConflict, Code: "AUDIO_VERIFICATION_REQUIRED",
			Message: "成片包含未通过审核的原生音频，只能预览，不能激活或正式导出",
			Details: map[string]any{"finalVideoVersionId": state.VersionID, "nativeAudioStatus": state.NativeAudioStatus, "productionReadiness": state.ProductionReadiness},
		}
	}
	return state, nil
}

func finalVideoOptionString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}
