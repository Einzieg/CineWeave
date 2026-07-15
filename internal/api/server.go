package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
)

type temporalClient interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
	CancelWorkflow(ctx context.Context, workflowID string, runID string) error
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
}

type Server struct {
	db                           *pgxpool.Pool
	auth                         *auth.Service
	authorizer                   *authz.Authorizer
	providers                    *provider.Service
	storage                      *storage.Client
	temporal                     temporalClient
	assetBatchSnapshotLockedHook func()
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"createdAt"`
}

type Workspace struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Project struct {
	ID                         string          `json:"id"`
	OrganizationID             string          `json:"organizationId"`
	WorkspaceID                string          `json:"workspaceId"`
	Name                       string          `json:"name"`
	Description                *string         `json:"description,omitempty"`
	ProjectType                *string         `json:"projectType,omitempty"`
	ContentType                *string         `json:"contentType,omitempty"`
	AspectRatio                *string         `json:"aspectRatio,omitempty"`
	VideoRatio                 string          `json:"videoRatio"`
	ArtStyle                   string          `json:"artStyle"`
	DirectorManual             string          `json:"directorManual"`
	VisualManual               string          `json:"visualManual"`
	ImageModelProfileKey       string          `json:"imageModelProfileKey"`
	VideoModelProfileKey       string          `json:"videoModelProfileKey"`
	ScriptModelProfileKey      string          `json:"scriptModelProfileKey"`
	TTSModelProfileKey         string          `json:"ttsModelProfileKey"`
	ASRModelProfileKey         string          `json:"asrModelProfileKey"`
	AudioStrategy              string          `json:"audioStrategy"`
	AudioRequirement           string          `json:"audioRequirement"`
	AudioConfigurationRevision int             `json:"audioConfigurationRevision"`
	Revision                   int64           `json:"revision"`
	ImageQuality               string          `json:"imageQuality"`
	ProductionMode             string          `json:"productionMode"`
	TimelineTimebase           int64           `json:"timelineTimebase"`
	FPSNumerator               int             `json:"fpsNumerator"`
	FPSDenominator             int             `json:"fpsDenominator"`
	ActiveFinalVideoVersionID  *string         `json:"activeFinalVideoVersionId,omitempty"`
	ActiveAudioMixVersionID    *string         `json:"activeAudioMixVersionId,omitempty"`
	Settings                   json.RawMessage `json:"settings"`
	CreatedAt                  time.Time       `json:"createdAt"`
	UpdatedAt                  time.Time       `json:"updatedAt"`
}

func New(pool *pgxpool.Pool, authService *auth.Service, providerService *provider.Service, storageClient *storage.Client, temporalClient client.Client, authorizers ...*authz.Authorizer) *Server {
	authorizer := authz.New(pool)
	if len(authorizers) > 0 && authorizers[0] != nil {
		authorizer = authorizers[0]
	}
	return &Server{db: pool, auth: authService, authorizer: authorizer, providers: providerService, storage: storageClient, temporal: temporalClient}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.HealthHandler("api"))
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /api/system/status", s.systemStatus)
	mux.HandleFunc("GET /api/system/setup-state", s.systemSetupState)
	mux.HandleFunc("POST /api/system/setup", s.systemSetup)

	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/refresh", s.refresh)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.withAuth(s.me))
	mux.HandleFunc("POST /api/provider-webhooks/{providerAccountId}/{webhookSecret}", s.providerWebhook)

	mux.HandleFunc("GET /api/organizations", s.withAuth(s.listOrganizations))
	mux.HandleFunc("POST /api/organizations", s.withAuth(s.createOrganization))
	mux.HandleFunc("GET /api/organizations/{organizationId}", s.withAuth(s.getOrganization))

	mux.HandleFunc("GET /api/workspaces", s.withAuth(s.listWorkspaces))
	mux.HandleFunc("POST /api/workspaces", s.withAuth(s.createWorkspace))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}", s.withAuth(s.getWorkspace))
	mux.HandleFunc("GET /api/teams", s.withAuth(s.listTeams))
	mux.HandleFunc("POST /api/teams", s.withAuth(s.createTeam))
	mux.HandleFunc("GET /api/teams/{teamId}", s.withAuth(s.getTeam))
	mux.HandleFunc("PATCH /api/teams/{teamId}", s.withAuth(s.updateTeam))
	mux.HandleFunc("DELETE /api/teams/{teamId}", s.withAuth(s.deleteTeam))
	mux.HandleFunc("GET /api/teams/{teamId}/members", s.withAuth(s.listTeamMembers))
	mux.HandleFunc("POST /api/teams/{teamId}/members", s.withAuth(s.addTeamMember))
	mux.HandleFunc("DELETE /api/teams/{teamId}/members/{userId}", s.withAuth(s.removeTeamMember))
	mux.HandleFunc("GET /api/roles", s.withAuth(s.listRoles))
	mux.HandleFunc("GET /api/permissions", s.withAuth(s.listPermissions))
	mux.HandleFunc("GET /api/role-bindings", s.withAuth(s.listRoleBindings))
	mux.HandleFunc("POST /api/role-bindings", s.withAuth(s.createRoleBinding))
	mux.HandleFunc("DELETE /api/role-bindings/{roleBindingId}", s.withAuth(s.deleteRoleBinding))

	mux.HandleFunc("GET /api/projects", s.withAuth(s.listProjects))
	mux.HandleFunc("POST /api/projects", s.withAuth(s.createProject))
	mux.HandleFunc("GET /api/projects/{projectId}", s.withAuth(s.getProject))
	mux.HandleFunc("PATCH /api/projects/{projectId}", s.withAuth(s.updateProject))
	mux.HandleFunc("DELETE /api/projects/{projectId}", s.withAuth(s.deleteProject))
	mux.HandleFunc("GET /api/project-manual-templates", s.withAuth(s.listProjectManualTemplates))
	mux.HandleFunc("GET /api/projects/{projectId}/manual-bindings", s.withAuth(s.listProjectManualBindings))
	mux.HandleFunc("PUT /api/projects/{projectId}/manual-bindings/{manualKind}", s.withAuth(s.bindProjectManual))
	mux.HandleFunc("GET /api/projects/{projectId}/production/status", s.withAuth(s.getProductionStatus))
	mux.HandleFunc("POST /api/projects/{projectId}/production/actions", s.withAuth(s.runProductionAction))
	mux.HandleFunc("POST /api/projects/{projectId}/regenerate", s.withAuth(s.regenerateCreativeObject))
	mux.HandleFunc("GET /api/projects/{projectId}/exports", s.withAuth(s.listProjectExports))
	mux.HandleFunc("POST /api/projects/{projectId}/exports", s.withAuth(s.createProjectExport))
	mux.HandleFunc("GET /api/projects/{projectId}/exports/{exportId}", s.withAuth(s.getProjectExport))
	mux.HandleFunc("POST /api/projects/{projectId}/exports/{exportId}/download-url", s.withAuth(s.createProjectExportDownloadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/reviews/run", s.withAuth(s.runProjectReview))
	mux.HandleFunc("GET /api/projects/{projectId}/reviews", s.withAuth(s.listReviewRuns))
	mux.HandleFunc("GET /api/projects/{projectId}/reviews/{reviewRunId}", s.withAuth(s.getReviewRun))
	mux.HandleFunc("GET /api/projects/{projectId}/review-items", s.withAuth(s.listReviewItems))
	mux.HandleFunc("GET /api/projects/{projectId}/review-items/{itemId}", s.withAuth(s.getReviewItem))
	mux.HandleFunc("POST /api/projects/{projectId}/review-items/{itemId}/resolve", s.withAuth(s.resolveReviewItem))
	mux.HandleFunc("POST /api/projects/{projectId}/review-items/{itemId}/ignore", s.withAuth(s.ignoreReviewItem))
	mux.HandleFunc("POST /api/projects/{projectId}/review-items/{itemId}/reopen", s.withAuth(s.reopenReviewItem))
	mux.HandleFunc("POST /api/projects/{projectId}/review-items/{itemId}/fixes/generate", s.withAuth(s.generateReviewFix))
	mux.HandleFunc("GET /api/projects/{projectId}/review-items/{itemId}/fixes", s.withAuth(s.listReviewFixes))
	mux.HandleFunc("GET /api/projects/{projectId}/review-fixes/{fixId}", s.withAuth(s.getReviewFix))
	mux.HandleFunc("POST /api/projects/{projectId}/review-fixes/{fixId}/apply", s.withAuth(s.applyReviewFix))
	mux.HandleFunc("POST /api/projects/{projectId}/review-fixes/{fixId}/dismiss", s.withAuth(s.dismissReviewFix))
	mux.HandleFunc("GET /api/projects/{projectId}/timelines", s.withAuth(s.listProjectTimelines))
	mux.HandleFunc("POST /api/projects/{projectId}/timelines", s.withAuth(s.createProjectTimeline))
	mux.HandleFunc("GET /api/projects/{projectId}/timelines/{timelineId}", s.withAuth(s.getProjectTimeline))
	mux.HandleFunc("PATCH /api/projects/{projectId}/timelines/{timelineId}", s.withAuth(s.updateProjectTimeline))
	mux.HandleFunc("DELETE /api/projects/{projectId}/timelines/{timelineId}", s.withAuth(s.deleteProjectTimeline))
	mux.HandleFunc("GET /api/projects/{projectId}/timelines/{timelineId}/detail", s.withAuth(s.getTimelineDetail))
	mux.HandleFunc("GET /api/projects/{projectId}/timelines/{timelineId}/clips", s.withAuth(s.listTimelineClips))
	mux.HandleFunc("POST /api/projects/{projectId}/timelines/{timelineId}/clips", s.withAuth(s.createTimelineClip))
	mux.HandleFunc("PATCH /api/projects/{projectId}/timelines/{timelineId}/clips/{clipId}", s.withAuth(s.updateTimelineClip))
	mux.HandleFunc("DELETE /api/projects/{projectId}/timelines/{timelineId}/clips/{clipId}", s.withAuth(s.deleteTimelineClip))
	mux.HandleFunc("POST /api/projects/{projectId}/timelines/{timelineId}/clips/reorder", s.withAuth(s.reorderTimelineClips))
	mux.HandleFunc("POST /api/projects/{projectId}/timelines/{timelineId}/compose", s.withAuth(s.composeTimeline))
	mux.HandleFunc("GET /api/projects/{projectId}/final-videos", s.withAuth(s.listFinalVideos))
	mux.HandleFunc("GET /api/projects/{projectId}/final-videos/{versionId}", s.withAuth(s.getFinalVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/final-videos/{versionId}/activate", s.withAuth(s.activateFinalVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/final-videos/{versionId}/download-url", s.withAuth(s.createFinalVideoDownloadURL))
	mux.HandleFunc("DELETE /api/projects/{projectId}/final-videos/{versionId}", s.withAuth(s.deleteFinalVideo))
	mux.HandleFunc("GET /api/projects/{projectId}/sources", s.withAuth(s.listProjectSources))
	mux.HandleFunc("POST /api/projects/{projectId}/sources", s.withAuth(s.createProjectSource))
	mux.HandleFunc("POST /api/projects/{projectId}/sources/import", s.withAuth(s.importProjectSourceFile))
	mux.HandleFunc("GET /api/projects/{projectId}/sources/{sourceId}", s.withAuth(s.getProjectSource))
	mux.HandleFunc("GET /api/projects/{projectId}/sources/{sourceId}/impact", s.withAuth(s.getProjectSourceImpact))
	mux.HandleFunc("PATCH /api/projects/{projectId}/sources/{sourceId}", s.withAuth(s.updateProjectSource))
	mux.HandleFunc("DELETE /api/projects/{projectId}/sources/{sourceId}", s.withAuth(s.deleteProjectSource))
	mux.HandleFunc("GET /api/projects/{projectId}/sources/{sourceId}/chapters", s.withAuth(s.listSourceChapters))
	mux.HandleFunc("GET /api/projects/{projectId}/sources/{sourceId}/chapters/{chapterId}", s.withAuth(s.getSourceChapter))
	mux.HandleFunc("DELETE /api/projects/{projectId}/sources/{sourceId}/chapters/{chapterId}", s.withAuth(s.deleteSourceChapter))
	mux.HandleFunc("POST /api/projects/{projectId}/sources/{sourceId}/extract-events", s.withAuth(s.extractNovelEvents))
	mux.HandleFunc("GET /api/projects/{projectId}/sources/{sourceId}/events", s.withAuth(s.listSourceNovelEvents))
	mux.HandleFunc("POST /api/projects/{projectId}/sources/{sourceId}/generate-adaptation-plan", s.withAuth(s.generateAdaptationPlan))
	mux.HandleFunc("GET /api/projects/{projectId}/novel-events/{eventId}", s.withAuth(s.getNovelEvent))
	mux.HandleFunc("PATCH /api/projects/{projectId}/novel-events/{eventId}", s.withAuth(s.updateNovelEvent))
	mux.HandleFunc("POST /api/projects/{projectId}/novel-events/{eventId}/review", s.withAuth(s.reviewNovelEvent))
	mux.HandleFunc("GET /api/projects/{projectId}/scripts", s.withAuth(s.listScripts))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts", s.withAuth(s.createScript))
	mux.HandleFunc("GET /api/projects/{projectId}/scripts/{scriptId}", s.withAuth(s.getScript))
	mux.HandleFunc("PATCH /api/projects/{projectId}/scripts/{scriptId}", s.withAuth(s.updateScript))
	mux.HandleFunc("GET /api/projects/{projectId}/scripts/{scriptId}/versions", s.withAuth(s.listScriptVersions))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts/{scriptId}/versions", s.withAuth(s.createScriptVersion))
	mux.HandleFunc("GET /api/projects/{projectId}/scripts/{scriptId}/versions/{versionId}/episodes", s.withAuth(s.listScriptEpisodes))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts/{scriptId}/activate-version", s.withAuth(s.activateScriptVersion))
	mux.HandleFunc("DELETE /api/projects/{projectId}/scripts/{scriptId}/versions/{versionId}", s.withAuth(s.deleteScriptVersion))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts/{scriptId}/versions/{versionId}/parse-scenes", s.withAuth(s.parseScriptScenes))
	mux.HandleFunc("GET /api/projects/{projectId}/scripts/{scriptId}/scenes", s.withAuth(s.listScriptScenes))
	mux.HandleFunc("PATCH /api/projects/{projectId}/script-episodes/{episodeId}", s.withAuth(s.updateScriptEpisode))
	mux.HandleFunc("GET /api/projects/{projectId}/script-scenes/{sceneId}", s.withAuth(s.getScriptScene))
	mux.HandleFunc("PATCH /api/projects/{projectId}/script-scenes/{sceneId}", s.withAuth(s.updateScriptScene))
	mux.HandleFunc("DELETE /api/projects/{projectId}/script-scenes/{sceneId}", s.withAuth(s.deleteScriptScene))
	mux.HandleFunc("POST /api/projects/{projectId}/script-scenes/{sceneId}/review", s.withAuth(s.reviewScriptScene))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts/{scriptId}/analyze-assets", s.withAuth(s.analyzeScriptAssets))
	mux.HandleFunc("POST /api/projects/{projectId}/scripts/{scriptId}/generate-storyboard", s.withAuth(s.generateScriptStoryboard))
	mux.HandleFunc("POST /api/projects/{projectId}/script-episodes/{episodeId}/timing/analyze", s.withAuth(s.analyzeScriptEpisodeTiming))
	mux.HandleFunc("GET /api/projects/{projectId}/script-episodes/{episodeId}/timing", s.withAuth(s.getScriptEpisodeTiming))
	mux.HandleFunc("GET /api/projects/{projectId}/script-episodes/{episodeId}/storyboard-plans", s.withAuth(s.listStoryboardPlans))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-plans/{planId}", s.withAuth(s.getStoryboardPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-plans/{planId}/activate", s.withAuth(s.activateStoryboardPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/split", s.withAuth(s.splitStoryboardShot))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/merge", s.withAuth(s.mergeStoryboardShots))
	mux.HandleFunc("PATCH /api/projects/{projectId}/storyboard-shots/{shotId}/timing", s.withAuth(s.updateStoryboardShotTiming))
	mux.HandleFunc("GET /api/projects/{projectId}/adaptation-plans", s.withAuth(s.listAdaptationPlans))
	mux.HandleFunc("POST /api/projects/{projectId}/adaptation-plans", s.withAuth(s.createAdaptationPlan))
	mux.HandleFunc("GET /api/projects/{projectId}/adaptation-plans/{planId}", s.withAuth(s.getAdaptationPlan))
	mux.HandleFunc("PATCH /api/projects/{projectId}/adaptation-plans/{planId}", s.withAuth(s.updateAdaptationPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/adaptation-plans/{planId}/review", s.withAuth(s.reviewAdaptationPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/adaptation-plans/{planId}/activate", s.withAuth(s.activateAdaptationPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/adaptation-plans/{planId}/generate-script", s.withAuth(s.generateScriptFromAdaptationPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/script-agent/sessions", s.withAuth(s.createScriptAgentSession))
	mux.HandleFunc("GET /api/projects/{projectId}/script-agent/sessions", s.withAuth(s.listScriptAgentSessions))
	mux.HandleFunc("GET /api/projects/{projectId}/script-agent/sessions/{sessionId}/messages", s.withAuth(s.listScriptAgentMessages))
	mux.HandleFunc("POST /api/projects/{projectId}/script-agent/sessions/{sessionId}/messages", s.withAuth(s.createScriptAgentMessage))
	mux.HandleFunc("POST /api/projects/{projectId}/script-agent/generate-script", s.withAuth(s.generateScriptFromAgent))
	mux.HandleFunc("POST /api/projects/{projectId}/script-agent/rewrite-script", s.withAuth(s.rewriteScriptFromAgent))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/tools", s.withAuth(s.listAgentTools))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/tasks", s.withAuth(s.createAgentTask))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/tasks", s.withAuth(s.listAgentTasks))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/tasks/{taskId}", s.withAuth(s.getAgentTask))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/tasks/{taskId}/cancel", s.withAuth(s.cancelAgentTask))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/tasks/{taskId}/steps/{stepId}/approve", s.withAuth(s.approveAgentStep))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/tasks/{taskId}/steps/{stepId}/reject", s.withAuth(s.rejectAgentStep))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/tasks/{taskId}/resume", s.withAuth(s.resumeAgentTask))
	mux.HandleFunc("GET /api/projects/{projectId}/canonical-assets", s.withAuth(s.listCanonicalAssets))
	mux.HandleFunc("POST /api/projects/{projectId}/asset-batches", s.withAuth(s.createAssetBatch))
	mux.HandleFunc("GET /api/projects/{projectId}/operations/{operationId}", s.withAuth(s.getRuntimeOperation))
	mux.HandleFunc("POST /api/projects/{projectId}/operations/{operationId}/reconcile", s.withAuth(s.reconcileRuntimeOperation))
	mux.HandleFunc("GET /api/projects/{projectId}/canonical-assets/{assetId}", s.withAuth(s.getCanonicalAsset))
	mux.HandleFunc("PATCH /api/projects/{projectId}/canonical-assets/{assetId}", s.withAuth(s.updateCanonicalAsset))
	mux.HandleFunc("DELETE /api/projects/{projectId}/canonical-assets/{assetId}", s.withAuth(s.deleteCanonicalAsset))
	mux.HandleFunc("GET /api/projects/{projectId}/canonical-assets/{assetId}/impact", s.withAuth(s.getCanonicalAssetImpact))
	mux.HandleFunc("POST /api/projects/{projectId}/canonical-assets/{assetId}/generate-card", s.withAuth(s.generateAssetCard))
	mux.HandleFunc("POST /api/projects/{projectId}/canonical-assets/{assetId}/generate-image", s.withAuth(s.generateCanonicalAssetImage))
	mux.HandleFunc("GET /api/projects/{projectId}/canonical-assets/{assetId}/references", s.withAuth(s.listAssetReferences))
	mux.HandleFunc("POST /api/projects/{projectId}/canonical-assets/{assetId}/references/upload-url", s.withAuth(s.createAssetReferenceUploadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/canonical-assets/{assetId}/references", s.withAuth(s.createAssetReference))
	mux.HandleFunc("POST /api/projects/{projectId}/canonical-assets/{assetId}/references/{referenceId}/set-primary", s.withAuth(s.setPrimaryAssetReference))
	mux.HandleFunc("DELETE /api/projects/{projectId}/canonical-assets/{assetId}/references/{referenceId}", s.withAuth(s.deleteAssetReference))
	mux.HandleFunc("GET /api/projects/{projectId}/shot-asset-requirements", s.withAuth(s.listShotAssetRequirements))
	mux.HandleFunc("PATCH /api/projects/{projectId}/shot-asset-requirements/{requirementId}", s.withAuth(s.updateShotAssetRequirement))
	mux.HandleFunc("POST /api/projects/{projectId}/shot-asset-requirements/{requirementId}/generate-image", s.withAuth(s.generateDerivedAssetImage))
	mux.HandleFunc("POST /api/projects/{projectId}/shot-asset-requirements/{requirementId}/review", s.withAuth(s.reviewShotAssetRequirement))
	mux.HandleFunc("POST /api/projects/{projectId}/shot-asset-requirements/{requirementId}/skip", s.withAuth(s.skipShotAssetRequirement))
	mux.HandleFunc("GET /api/projects/{projectId}/assets", s.withAuth(s.listAssets))
	mux.HandleFunc("POST /api/projects/{projectId}/assets", s.withAuth(s.createAsset))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/upload-url", s.withAuth(s.createAssetUploadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/{assetId}/generate-image", s.withAuth(s.generateCanonicalAssetImage))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/{assetId}/review", s.withAuth(s.reviewCanonicalAsset))
	mux.HandleFunc("GET /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.getAsset))
	mux.HandleFunc("PATCH /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.updateAsset))
	mux.HandleFunc("DELETE /api/projects/{projectId}/assets/{assetId}", s.withAuth(s.deleteAsset))
	mux.HandleFunc("POST /api/projects/{projectId}/assets/{assetId}/variants", s.withAuth(s.createAssetVariant))
	mux.HandleFunc("GET /api/projects/{projectId}/shot-production/status", s.withAuth(s.getShotProductionStatus))
	mux.HandleFunc("POST /api/projects/{projectId}/shot-production/actions", s.withAuth(s.runShotProductionAction))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots", s.withAuth(s.createStoryboardShot))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/reorder", s.withAuth(s.reorderStoryboardShots))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/detail", s.withAuth(s.getStoryboardShotDetail))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan", s.withAuth(s.getStoryboardShotRenderPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan", s.withAuth(s.createStoryboardShotRenderPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-verification", s.withAuth(s.verifyStoryboardShotRenderPlanAudio))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-review", s.withAuth(s.startNativeAudioReview))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-reviews", s.withAuth(s.listNativeAudioReviews))
	mux.HandleFunc("PATCH /api/projects/{projectId}/storyboard-shots/{shotId}", s.withAuth(s.updateStoryboardShot))
	mux.HandleFunc("DELETE /api/projects/{projectId}/storyboard-shots/{shotId}", s.withAuth(s.deleteStoryboardShot))
	mux.HandleFunc("GET /api/projects/{projectId}/character-voices", s.withAuth(s.listCharacterVoices))
	mux.HandleFunc("POST /api/projects/{projectId}/character-voices", s.withAuth(s.createCharacterVoice))
	mux.HandleFunc("PATCH /api/projects/{projectId}/character-voices/{voiceId}", s.withAuth(s.updateCharacterVoice))
	mux.HandleFunc("DELETE /api/projects/{projectId}/character-voices/{voiceId}", s.withAuth(s.deleteCharacterVoice))
	mux.HandleFunc("GET /api/projects/{projectId}/script-episodes/{episodeId}/audio", s.withAuth(s.getEpisodeAudio))
	mux.HandleFunc("POST /api/projects/{projectId}/script-episodes/{episodeId}/audio/produce", s.withAuth(s.produceEpisodeAudio))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/media/unlink", s.withAuth(s.unlinkStoryboardShotMedia))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/review", s.withAuth(s.reviewStoryboardShot))

	mux.HandleFunc("GET /api/provider-catalog", s.withAuth(s.listProviderCatalog))
	mux.HandleFunc("GET /api/provider-catalog/{providerKey}", s.withAuth(s.getProviderCatalogEntry))
	mux.HandleFunc("POST /api/provider-catalog/{providerKey}/install", s.withAuth(s.installProviderCatalogEntry))
	mux.HandleFunc("GET /api/providers/connectors", s.withAuth(s.listProviderConnectors))
	mux.HandleFunc("POST /api/providers/connectors/import", s.withAuth(s.importProviderConnector))
	mux.HandleFunc("GET /api/providers/accounts", s.withAuth(s.listProviderAccounts))
	mux.HandleFunc("POST /api/providers/accounts", s.withAuth(s.createProviderAccount))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}", s.withAuth(s.getProviderAccount))
	mux.HandleFunc("PATCH /api/providers/accounts/{accountId}", s.withAuth(s.updateProviderAccount))
	mux.HandleFunc("DELETE /api/providers/accounts/{accountId}", s.withAuth(s.deleteProviderAccount))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials/rotate", s.withAuth(s.rotateProviderCredential))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/discover-models", s.withAuth(s.discoverProviderModels))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}/models", s.withAuth(s.listProviderModels))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/models", s.withAuth(s.createProviderModel))
	mux.HandleFunc("PATCH /api/providers/models/{modelId}", s.withAuth(s.updateProviderModel))
	mux.HandleFunc("DELETE /api/providers/models/{modelId}", s.withAuth(s.deleteProviderModel))
	mux.HandleFunc("POST /api/providers/models/{modelId}/test", s.withAuth(s.testProviderModel))
	mux.HandleFunc("POST /api/providers/manifests/validate", s.withAuth(s.validateProviderManifest))
	mux.HandleFunc("POST /api/providers/manifests/test-run", s.withAuth(s.runProviderManifestTest))
	mux.HandleFunc("GET /api/model-profiles", s.withAuth(s.listModelProfiles))
	mux.HandleFunc("POST /api/model-profiles", s.withAuth(s.createModelProfile))
	mux.HandleFunc("PATCH /api/model-profiles/{profileId}", s.withAuth(s.updateModelProfile))
	mux.HandleFunc("POST /api/model-profiles/{profileId}/bindings", s.withAuth(s.createModelProfileBinding))
	mux.HandleFunc("PATCH /api/model-profiles/{profileId}/bindings/{bindingId}", s.withAuth(s.updateModelProfileBinding))
	mux.HandleFunc("DELETE /api/model-profiles/{profileId}/bindings/{bindingId}", s.withAuth(s.deleteModelProfileBinding))
	mux.HandleFunc("GET /api/provider-call-logs", s.withAuth(s.listProviderCallLogs))
	mux.HandleFunc("GET /api/provider-usage/summary", s.withAuth(s.getProviderUsageSummary))
	mux.HandleFunc("GET /api/provider-limit-policies", s.withAuth(s.listProviderLimitPolicies))
	mux.HandleFunc("POST /api/provider-limit-policies", s.withAuth(s.createProviderLimitPolicy))
	mux.HandleFunc("GET /api/provider-limit-policies/{policyId}", s.withAuth(s.getProviderLimitPolicy))
	mux.HandleFunc("PATCH /api/provider-limit-policies/{policyId}", s.withAuth(s.updateProviderLimitPolicy))
	mux.HandleFunc("DELETE /api/provider-limit-policies/{policyId}", s.withAuth(s.deleteProviderLimitPolicy))
	mux.HandleFunc("GET /api/provider-circuit-states", s.withAuth(s.listProviderCircuitStates))
	mux.HandleFunc("POST /api/provider-circuit-states/{stateId}/reset", s.withAuth(s.resetProviderCircuitState))
	mux.HandleFunc("GET /api/prompt-templates", s.withAuth(s.listPromptTemplates))
	mux.HandleFunc("POST /api/prompt-templates", s.withAuth(s.createPromptTemplate))
	mux.HandleFunc("GET /api/prompt-templates/{templateId}", s.withAuth(s.getPromptTemplate))
	mux.HandleFunc("PATCH /api/prompt-templates/{templateId}", s.withAuth(s.updatePromptTemplate))
	mux.HandleFunc("GET /api/prompt-templates/{templateId}/versions", s.withAuth(s.listPromptVersions))
	mux.HandleFunc("POST /api/prompt-templates/{templateId}/versions", s.withAuth(s.createPromptVersion))
	mux.HandleFunc("POST /api/prompt-versions/{versionId}/activate", s.withAuth(s.activatePromptVersion))
	mux.HandleFunc("GET /api/prompt-bindings", s.withAuth(s.listPromptBindings))
	mux.HandleFunc("POST /api/prompt-bindings", s.withAuth(s.createPromptBinding))
	mux.HandleFunc("DELETE /api/prompt-bindings/{bindingId}", s.withAuth(s.deletePromptBinding))
	mux.HandleFunc("POST /api/prompts/render-test", s.withAuth(s.renderPromptTest))
	mux.HandleFunc("POST /api/workflow-runs", s.withAuth(s.createWorkflowRun))
	mux.HandleFunc("GET /api/workflow-runs", s.withAuth(s.listWorkflowRuns))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}", s.withAuth(s.getWorkflowRun))
	mux.HandleFunc("POST /api/workflow-runs/{workflowRunId}/cancel", s.withAuth(s.cancelWorkflowRun))
	mux.HandleFunc("POST /api/workflow-runs/{workflowRunId}/retry-failed", s.withAuth(s.retryFailedWorkflowRun))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/nodes", s.withAuth(s.listWorkflowNodeRuns))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/shots", s.withAuth(s.listWorkflowRunShots))
	mux.HandleFunc("GET /api/artifacts", s.withAuth(s.listArtifacts))
	mux.HandleFunc("GET /api/artifacts/{artifactId}", s.withAuth(s.getArtifact))
	mux.HandleFunc("POST /api/artifacts/{artifactId}/preview-url", s.withAuth(s.createArtifactPreviewURL))
	mux.HandleFunc("GET /api/media-files/{mediaFileId}", s.withAuth(s.getMediaFile))
	mux.HandleFunc("POST /api/media-files/{mediaFileId}/download-url", s.withAuth(s.createMediaFileDownloadURL))

	return httpx.WithCORS(httpx.WithRequestID(mux))
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !publicRegistrationAllowed() {
		s.writeError(w, r, auth.ErrPublicRegistrationDisabled)
		return
	}
	var req auth.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Register(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req auth.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Login(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var req auth.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.Refresh(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var req auth.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	if err := s.auth.Logout(r.Context(), req); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"ok": true}, nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	user, err := s.auth.Me(r.Context(), principal)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	workspaceID, err := s.defaultWorkspaceID(r, principal.OrganizationID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":           user,
		"organizationId": principal.OrganizationID,
		"workspaceId":    workspaceID,
	}, nil)
}

func (s *Server) defaultWorkspaceID(r *http.Request, organizationID string) (string, error) {
	if strings.TrimSpace(organizationID) == "" {
		return "", nil
	}
	var workspaceID string
	err := s.db.QueryRow(r.Context(), `
		SELECT id
		FROM workspaces
		WHERE organization_id = $1
		ORDER BY created_at
		LIMIT 1
	`, organizationID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return workspaceID, err
}

func (s *Server) listOrganizations(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	rows, err := s.db.Query(r.Context(), `
		SELECT o.id, o.name, o.slug, o.created_at
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		WHERE om.user_id = $1 AND om.status = 'active'
		  AND EXISTS (
			SELECT 1
			FROM role_bindings rb
			JOIN role_permissions rp ON rp.role_id = rb.role_id
			WHERE rb.organization_id = o.id
			  AND rb.subject_type = 'user'
			  AND rb.subject_user_id = $1
			  AND rb.resource_type = 'organization'
			  AND rb.resource_organization_id = o.id
			  AND (rp.permission_key = 'organization.read' OR rp.permission_key = 'admin.manage')
		  )
		ORDER BY o.created_at
	`, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Organization, 0)
	for rows.Next() {
		var item Organization
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID, err := s.auth.CreateOrganization(r.Context(), principal.UserID, req.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	org, err := s.organization(r, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, org, nil)
}

func (s *Server) getOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := r.PathValue("organizationId")
	if !s.authorize(w, r, principal, authz.PermissionOrganizationRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	org, err := s.organization(r, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, org, nil)
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if orgID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANIZATION_REQUIRED", "organization context is required", nil, false)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkspaceRead, authz.Resource{OrganizationID: orgID}) {
		return
	}

	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, name, created_at
		FROM workspaces
		WHERE organization_id = $1
		ORDER BY created_at
	`, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Workspace, 0)
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		OrganizationID string `json:"organizationId"`
		Name           string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := req.OrganizationID
	if orgID == "" {
		orgID = organizationID(r, principal)
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "name is required", nil, false)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkspaceManage, authz.Resource{OrganizationID: orgID}) {
		return
	}

	var item Workspace
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, $2)
		RETURNING id, organization_id, name, created_at
	`, orgID, strings.TrimSpace(req.Name)).Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var item Workspace
	err := s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, name, created_at
		FROM workspaces
		WHERE id = $1
	`, r.PathValue("workspaceId")).Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkspaceRead, authz.Resource{WorkspaceID: item.ID}) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if orgID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANIZATION_REQUIRED", "organization context is required", nil, false)
		return
	}
	workspaceID := r.URL.Query().Get("filter[workspaceId]")
	if workspaceID != "" {
		if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{WorkspaceID: workspaceID}) {
			return
		}
	} else if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{OrganizationID: orgID}) {
		return
	}

	query := `
		SELECT id, organization_id, workspace_id, name, description, project_type, content_type, aspect_ratio,
		       video_ratio, art_style, director_manual, visual_manual,
		       image_model_profile_key, video_model_profile_key, script_model_profile_key,
		       tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement, audio_configuration_revision,
		       image_quality, production_mode, timeline_timebase, fps_numerator, fps_denominator,
		       active_final_video_version_id::text, active_audio_mix_version_id::text, settings, revision, created_at, updated_at
		FROM projects
		WHERE organization_id = $1
	`
	args := []any{orgID}
	if workspaceID != "" {
		query += " AND workspace_id = $2"
		args = append(args, workspaceID)
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]Project, 0)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		WorkspaceID                   string          `json:"workspaceId"`
		Name                          string          `json:"name"`
		Description                   *string         `json:"description"`
		ProjectType                   *string         `json:"projectType"`
		ContentType                   *string         `json:"contentType"`
		AspectRatio                   *string         `json:"aspectRatio"`
		VideoRatio                    *string         `json:"videoRatio"`
		ArtStyle                      *string         `json:"artStyle"`
		DirectorManual                *string         `json:"directorManual"`
		VisualManual                  *string         `json:"visualManual"`
		DirectorManualPromptVersionID *string         `json:"directorManualPromptVersionId"`
		VisualManualPromptVersionID   *string         `json:"visualManualPromptVersionId"`
		ImageModelProfileKey          *string         `json:"imageModelProfileKey"`
		VideoModelProfileKey          *string         `json:"videoModelProfileKey"`
		ScriptModelProfileKey         *string         `json:"scriptModelProfileKey"`
		TTSModelProfileKey            *string         `json:"ttsModelProfileKey"`
		ASRModelProfileKey            *string         `json:"asrModelProfileKey"`
		AudioStrategy                 *string         `json:"audioStrategy"`
		AudioRequirement              *string         `json:"audioRequirement"`
		ImageQuality                  *string         `json:"imageQuality"`
		ProductionMode                *string         `json:"productionMode"`
		TimelineTimebase              *int64          `json:"timelineTimebase"`
		FPSNumerator                  *int            `json:"fpsNumerator"`
		FPSDenominator                *int            `json:"fpsDenominator"`
		Settings                      json.RawMessage `json:"settings"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workspaceId and name are required", nil, false)
		return
	}
	settings := req.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}
	videoRatio := normalizedProjectString(req.VideoRatio, "16:9")
	aspectRatio := req.AspectRatio
	if aspectRatio == nil || strings.TrimSpace(*aspectRatio) == "" {
		aspectRatio = &videoRatio
	}
	artStyle := normalizedProjectString(req.ArtStyle, "")
	directorManual := normalizedProjectString(req.DirectorManual, "")
	visualManual := normalizedProjectString(req.VisualManual, "")
	directorManualPromptVersionID := normalizedProjectString(req.DirectorManualPromptVersionID, "")
	visualManualPromptVersionID := normalizedProjectString(req.VisualManualPromptVersionID, "")
	imageModelProfileKey := normalizedProjectString(req.ImageModelProfileKey, "image_generation_default")
	videoModelProfileKey := normalizedProjectString(req.VideoModelProfileKey, "video_generation_default")
	scriptModelProfileKey := normalizedProjectString(req.ScriptModelProfileKey, "script_agent_default")
	ttsModelProfileKey := normalizedProjectString(req.TTSModelProfileKey, "tts_generation_default")
	asrModelProfileKey := normalizedProjectString(req.ASRModelProfileKey, "audio_transcription_default")
	audioStrategy := normalizedProjectString(req.AudioStrategy, "native_av")
	audioRequirement := normalizedProjectString(req.AudioRequirement, "preferred")
	if !validProjectAudioSettings(audioStrategy, audioRequirement) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "audioStrategy or audioRequirement is invalid", nil, false)
		return
	}
	imageQuality := normalizedProjectString(req.ImageQuality, "standard")
	productionMode := normalizedProjectString(req.ProductionMode, "silent_video")
	timelineTimebase := int64(90_000)
	if req.TimelineTimebase != nil {
		timelineTimebase = *req.TimelineTimebase
	}
	fpsNumerator := 24
	if req.FPSNumerator != nil {
		fpsNumerator = *req.FPSNumerator
	}
	fpsDenominator := 1
	if req.FPSDenominator != nil {
		fpsDenominator = *req.FPSDenominator
	}
	if !validProjectTimebase(timelineTimebase, fpsNumerator, fpsDenominator) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "timelineTimebase and frame rate must be positive and exactly representable", nil, false)
		return
	}

	var orgID string
	err := s.db.QueryRow(r.Context(), `SELECT organization_id FROM workspaces WHERE id = $1`, req.WorkspaceID).Scan(&orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectWrite, authz.Resource{WorkspaceID: req.WorkspaceID}) {
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	var item Project
	err = tx.QueryRow(r.Context(), `
		INSERT INTO projects(
			organization_id, workspace_id, name, description, project_type, content_type, aspect_ratio,
			video_ratio, art_style, director_manual, visual_manual,
			image_model_profile_key, video_model_profile_key, script_model_profile_key,
			tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement,
			image_quality, production_mode, timeline_timebase, fps_numerator, fps_denominator, settings, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
		RETURNING id, organization_id, workspace_id, name, description, project_type, content_type, aspect_ratio,
		          video_ratio, art_style, director_manual, visual_manual,
		          image_model_profile_key, video_model_profile_key, script_model_profile_key,
		          tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement, audio_configuration_revision,
		          image_quality, production_mode, timeline_timebase, fps_numerator, fps_denominator,
		          active_final_video_version_id::text, active_audio_mix_version_id::text, settings, revision, created_at, updated_at
	`, orgID, req.WorkspaceID, strings.TrimSpace(req.Name), req.Description, req.ProjectType, req.ContentType, aspectRatio,
		videoRatio, artStyle, directorManual, visualManual, imageModelProfileKey, videoModelProfileKey, scriptModelProfileKey,
		ttsModelProfileKey, asrModelProfileKey, audioStrategy, audioRequirement,
		imageQuality, productionMode, timelineTimebase, fpsNumerator, fpsDenominator, settings, principal.UserID).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.ProjectType, &item.ContentType, &item.AspectRatio,
			&item.VideoRatio, &item.ArtStyle, &item.DirectorManual, &item.VisualManual,
			&item.ImageModelProfileKey, &item.VideoModelProfileKey, &item.ScriptModelProfileKey,
			&item.TTSModelProfileKey, &item.ASRModelProfileKey, &item.AudioStrategy, &item.AudioRequirement, &item.AudioConfigurationRevision,
			&item.ImageQuality, &item.ProductionMode, &item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
			&item.ActiveFinalVideoVersionID, &item.ActiveAudioMixVersionID, &item.Settings, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO project_members(project_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, item.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}

	var roleID string
	if err := tx.QueryRow(r.Context(), `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'project_owner' AND scope = 'project'
	`).Scan(&roleID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_project_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'project', $4, $3)
		ON CONFLICT DO NOTHING
	`, orgID, roleID, principal.UserID, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}

	if directorManual == "" {
		manual := ""
		if directorManualPromptVersionID != "" {
			_, manual, err = s.bindProjectManualTx(r.Context(), tx, orgID, item.ID, "director", directorManualPromptVersionID, principal.UserID)
		} else {
			manual, err = s.bindDefaultProjectManualTx(r.Context(), tx, orgID, item.ID, "director", principal.UserID)
		}
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.DirectorManual = manual
	}
	if visualManual == "" {
		manual := ""
		if visualManualPromptVersionID != "" {
			_, manual, err = s.bindProjectManualTx(r.Context(), tx, orgID, item.ID, "visual", visualManualPromptVersionID, principal.UserID)
		} else {
			manual, err = s.bindDefaultProjectManualTx(r.Context(), tx, orgID, item.ID, "visual", principal.UserID)
		}
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.VisualManual = manual
	}
	if err := tx.QueryRow(r.Context(), `SELECT revision, updated_at FROM projects WHERE id = $1`, item.ID).Scan(&item.Revision, &item.UpdatedAt); err != nil {
		s.writeError(w, r, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: item.ID}) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	item, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectWrite, authz.Resource{ProjectID: item.ID}) {
		return
	}

	var req struct {
		Name                          *string         `json:"name"`
		Description                   *string         `json:"description"`
		ProjectType                   *string         `json:"projectType"`
		ContentType                   *string         `json:"contentType"`
		AspectRatio                   *string         `json:"aspectRatio"`
		VideoRatio                    *string         `json:"videoRatio"`
		ArtStyle                      *string         `json:"artStyle"`
		DirectorManual                *string         `json:"directorManual"`
		VisualManual                  *string         `json:"visualManual"`
		DirectorManualPromptVersionID *string         `json:"directorManualPromptVersionId"`
		VisualManualPromptVersionID   *string         `json:"visualManualPromptVersionId"`
		ImageModelProfileKey          *string         `json:"imageModelProfileKey"`
		VideoModelProfileKey          *string         `json:"videoModelProfileKey"`
		ScriptModelProfileKey         *string         `json:"scriptModelProfileKey"`
		TTSModelProfileKey            *string         `json:"ttsModelProfileKey"`
		ASRModelProfileKey            *string         `json:"asrModelProfileKey"`
		AudioStrategy                 *string         `json:"audioStrategy"`
		AudioRequirement              *string         `json:"audioRequirement"`
		ImageQuality                  *string         `json:"imageQuality"`
		ProductionMode                *string         `json:"productionMode"`
		TimelineTimebase              *int64          `json:"timelineTimebase"`
		FPSNumerator                  *int            `json:"fpsNumerator"`
		FPSDenominator                *int            `json:"fpsDenominator"`
		Settings                      json.RawMessage `json:"settings"`
		ExpectedRevision              *int64          `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ExpectedRevision != nil && *req.ExpectedRevision <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedRevision must be positive", nil, false)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err = scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), projectID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.ExpectedRevision != nil && item.Revision != *req.ExpectedRevision {
		conflict := newAPIError(http.StatusConflict, "PROJECT_REVISION_CONFLICT", "project settings changed before the update was applied")
		conflict.Details = map[string]any{"expectedRevision": *req.ExpectedRevision, "currentRevision": item.Revision}
		s.writeError(w, r, conflict)
		return
	}
	baseRevision := item.Revision
	previousVideoRatio := strings.TrimSpace(item.VideoRatio)
	nextVideoRatio := previousVideoRatio
	if req.VideoRatio != nil {
		nextVideoRatio = strings.TrimSpace(*req.VideoRatio)
	}
	videoRatioChanged := nextVideoRatio != "" && nextVideoRatio != previousVideoRatio
	nextTimebase := item.TimelineTimebase
	if req.TimelineTimebase != nil {
		nextTimebase = *req.TimelineTimebase
	}
	nextFPSNumerator := item.FPSNumerator
	if req.FPSNumerator != nil {
		nextFPSNumerator = *req.FPSNumerator
	}
	nextFPSDenominator := item.FPSDenominator
	if req.FPSDenominator != nil {
		nextFPSDenominator = *req.FPSDenominator
	}
	if !validProjectTimebase(nextTimebase, nextFPSNumerator, nextFPSDenominator) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "timelineTimebase and frame rate must be positive and exactly representable", nil, false)
		return
	}
	frameRateChanged := nextTimebase != item.TimelineTimebase || nextFPSNumerator != item.FPSNumerator || nextFPSDenominator != item.FPSDenominator
	nextAudioStrategy := normalizedProjectString(req.AudioStrategy, item.AudioStrategy)
	nextAudioRequirement := normalizedProjectString(req.AudioRequirement, item.AudioRequirement)
	if !validProjectAudioSettings(nextAudioStrategy, nextAudioRequirement) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "audioStrategy or audioRequirement is invalid", nil, false)
		return
	}
	audioSettingsChanged := nextAudioStrategy != item.AudioStrategy || nextAudioRequirement != item.AudioRequirement ||
		(req.TTSModelProfileKey != nil && strings.TrimSpace(*req.TTSModelProfileKey) != item.TTSModelProfileKey) ||
		(req.ASRModelProfileKey != nil && strings.TrimSpace(*req.ASRModelProfileKey) != item.ASRModelProfileKey)
	directorManualPromptVersionID := normalizedProjectString(req.DirectorManualPromptVersionID, "")
	visualManualPromptVersionID := normalizedProjectString(req.VisualManualPromptVersionID, "")
	if req.DirectorManual != nil && directorManualPromptVersionID != "" {
		s.writeError(w, r, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "director manual text and prompt version cannot both be set"))
		return
	}
	if req.VisualManual != nil && visualManualPromptVersionID != "" {
		s.writeError(w, r, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "visual manual text and prompt version cannot both be set"))
		return
	}

	settings := item.Settings
	if len(req.Settings) > 0 {
		settings = req.Settings
	}

	err = tx.QueryRow(r.Context(), `
		UPDATE projects
		SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			project_type = COALESCE($4, project_type),
			content_type = COALESCE($5, content_type),
			aspect_ratio = COALESCE($6, aspect_ratio),
			video_ratio = COALESCE($7, video_ratio),
			art_style = COALESCE($8, art_style),
			director_manual = COALESCE($9, director_manual),
			visual_manual = COALESCE($10, visual_manual),
			image_model_profile_key = COALESCE($11, image_model_profile_key),
			video_model_profile_key = COALESCE($12, video_model_profile_key),
			script_model_profile_key = COALESCE($13, script_model_profile_key),
			tts_model_profile_key = COALESCE($14, tts_model_profile_key),
			asr_model_profile_key = COALESCE($15, asr_model_profile_key),
			audio_strategy = COALESCE($16, audio_strategy),
			audio_requirement = COALESCE($17, audio_requirement),
			image_quality = COALESCE($18, image_quality),
			production_mode = COALESCE($19, production_mode),
			timeline_timebase = COALESCE($20, timeline_timebase),
			fps_numerator = COALESCE($21, fps_numerator),
			fps_denominator = COALESCE($22, fps_denominator),
			settings = $23,
			revision = revision + 1,
			updated_at = now()
		WHERE id = $1
		  AND revision = $24
		RETURNING id, organization_id, workspace_id, name, description, project_type, content_type, aspect_ratio,
		          video_ratio, art_style, director_manual, visual_manual,
		          image_model_profile_key, video_model_profile_key, script_model_profile_key,
		          tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement, audio_configuration_revision,
		          image_quality, production_mode, timeline_timebase, fps_numerator, fps_denominator,
		          active_final_video_version_id::text, active_audio_mix_version_id::text, settings, revision, created_at, updated_at
	`, projectID, req.Name, req.Description, req.ProjectType, req.ContentType, req.AspectRatio,
		normalizedOptionalString(req.VideoRatio), normalizedOptionalString(req.ArtStyle), normalizedOptionalString(req.DirectorManual), normalizedOptionalString(req.VisualManual),
		normalizedOptionalString(req.ImageModelProfileKey), normalizedOptionalString(req.VideoModelProfileKey), normalizedOptionalString(req.ScriptModelProfileKey),
		normalizedOptionalString(req.TTSModelProfileKey), normalizedOptionalString(req.ASRModelProfileKey), normalizedOptionalString(req.AudioStrategy), normalizedOptionalString(req.AudioRequirement),
		normalizedOptionalString(req.ImageQuality), normalizedOptionalString(req.ProductionMode), req.TimelineTimebase, req.FPSNumerator, req.FPSDenominator, settings, baseRevision).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.ProjectType, &item.ContentType, &item.AspectRatio,
			&item.VideoRatio, &item.ArtStyle, &item.DirectorManual, &item.VisualManual,
			&item.ImageModelProfileKey, &item.VideoModelProfileKey, &item.ScriptModelProfileKey,
			&item.TTSModelProfileKey, &item.ASRModelProfileKey, &item.AudioStrategy, &item.AudioRequirement, &item.AudioConfigurationRevision,
			&item.ImageQuality, &item.ProductionMode, &item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
			&item.ActiveFinalVideoVersionID, &item.ActiveAudioMixVersionID, &item.Settings, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.DirectorManual != nil || req.VisualManual != nil {
		if err := disableManualBindingsForDirectEditTx(r.Context(), tx, item.ID, req.DirectorManual != nil, req.VisualManual != nil); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if directorManualPromptVersionID != "" {
		_, manual, err := s.bindProjectManualTx(r.Context(), tx, item.OrganizationID, item.ID, "director", directorManualPromptVersionID, principal.UserID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.DirectorManual = manual
	}
	if visualManualPromptVersionID != "" {
		_, manual, err := s.bindProjectManualTx(r.Context(), tx, item.OrganizationID, item.ID, "visual", visualManualPromptVersionID, principal.UserID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.VisualManual = manual
	}
	if err := tx.QueryRow(r.Context(), `SELECT revision, updated_at FROM projects WHERE id = $1`, item.ID).Scan(&item.Revision, &item.UpdatedAt); err != nil {
		s.writeError(w, r, err)
		return
	}
	if videoRatioChanged {
		if err := production.MarkProjectVideoRatioStale(r.Context(), tx, item.ID, item.VideoRatio); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := production.MarkFinalVideoStale(r.Context(), tx, item.ID, ""); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := insertAPIEvent(r.Context(), tx, item.OrganizationID, item.ID, "project.video_ratio.changed", "project", item.ID, mustRawJSON(map[string]any{
			"previousAspectRatio": previousVideoRatio,
			"aspectRatio":         item.VideoRatio,
		})); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if frameRateChanged {
		if err := production.MarkProjectFrameRateStale(r.Context(), tx, item.ID, item.TimelineTimebase, item.FPSNumerator, item.FPSDenominator); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := production.MarkFinalVideoStale(r.Context(), tx, item.ID, ""); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := insertAPIEvent(r.Context(), tx, item.OrganizationID, item.ID, "project.frame_rate.changed", "project", item.ID, mustRawJSON(map[string]any{
			"timelineTimebase": item.TimelineTimebase,
			"fpsNumerator":     item.FPSNumerator,
			"fpsDenominator":   item.FPSDenominator,
		})); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if audioSettingsChanged {
		invalidation, err := invalidateProjectAudioConfigurationTx(r.Context(), tx, item, "project_audio_settings_changed", principal.UserID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.AudioConfigurationRevision = invalidation.Revision
		item.ActiveAudioMixVersionID = nil
		if err := insertAPIEvent(r.Context(), tx, item.OrganizationID, item.ID, "project.audio_settings.changed", "project", item.ID, mustRawJSON(map[string]any{
			"audioStrategy": item.AudioStrategy, "audioRequirement": item.AudioRequirement,
			"ttsModelProfileKey": item.TTSModelProfileKey, "asrModelProfileKey": item.ASRModelProfileKey,
			"audioConfigurationRevision": item.AudioConfigurationRevision,
		})); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	projectID := r.PathValue("projectId")
	item, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectDelete, authz.Resource{ProjectID: item.ID}) {
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.auth.ParseBearer(r.Header.Get("Authorization"))
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		next(w, r, principal)
	}
}

func (s *Server) organization(r *http.Request, orgID string) (Organization, error) {
	var item Organization
	err := s.db.QueryRow(r.Context(), `
		SELECT id, name, slug, created_at
		FROM organizations
		WHERE id = $1
	`, orgID).Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt)
	return item, err
}

func (s *Server) project(r *http.Request, projectID string) (Project, error) {
	return scanProject(s.db.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1`), projectID))
}

func projectSelectSQL(where string) string {
	return `
		SELECT p.id, p.organization_id, p.workspace_id, p.name, p.description, p.project_type, p.content_type, p.aspect_ratio,
		       p.video_ratio, p.art_style,
		       COALESCE((
		         SELECT pv.content
		         FROM project_manual_bindings b
		         JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		         WHERE b.project_id = p.id
		           AND b.manual_kind = 'director'
		           AND b.status = 'active'
		           AND pv.status = 'active'
		         ORDER BY b.updated_at DESC
		         LIMIT 1
		       ), p.director_manual) AS director_manual,
		       COALESCE((
		         SELECT pv.content
		         FROM project_manual_bindings b
		         JOIN prompt_versions pv ON pv.id = b.prompt_version_id
		         WHERE b.project_id = p.id
		           AND b.manual_kind = 'visual'
		           AND b.status = 'active'
		           AND pv.status = 'active'
		         ORDER BY b.updated_at DESC
		         LIMIT 1
		       ), p.visual_manual) AS visual_manual,
		       p.image_model_profile_key, p.video_model_profile_key, p.script_model_profile_key,
		       p.tts_model_profile_key, p.asr_model_profile_key, p.audio_strategy, p.audio_requirement, p.audio_configuration_revision,
		       p.image_quality, p.production_mode, p.timeline_timebase, p.fps_numerator, p.fps_denominator,
		       p.active_final_video_version_id::text, p.active_audio_mix_version_id::text, p.settings, p.revision, p.created_at, p.updated_at
		FROM projects p
	` + where
}

func (s *Server) ensureOrganizationMember(r *http.Request, userID, orgID string) error {
	if orgID == "" {
		return auth.ErrUnauthorized
	}
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, orgID, userID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return auth.ErrForbidden
	}
	return nil
}

func (s *Server) ensureProjectMember(r *http.Request, userID, projectID string) error {
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1
			FROM project_members
			WHERE project_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, projectID, userID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return auth.ErrForbidden
	}
	return nil
}

func scanProject(row pgx.Row) (Project, error) {
	var item Project
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.WorkspaceID,
		&item.Name,
		&item.Description,
		&item.ProjectType,
		&item.ContentType,
		&item.AspectRatio,
		&item.VideoRatio,
		&item.ArtStyle,
		&item.DirectorManual,
		&item.VisualManual,
		&item.ImageModelProfileKey,
		&item.VideoModelProfileKey,
		&item.ScriptModelProfileKey,
		&item.TTSModelProfileKey,
		&item.ASRModelProfileKey,
		&item.AudioStrategy,
		&item.AudioRequirement,
		&item.AudioConfigurationRevision,
		&item.ImageQuality,
		&item.ProductionMode,
		&item.TimelineTimebase,
		&item.FPSNumerator,
		&item.FPSDenominator,
		&item.ActiveFinalVideoVersionID,
		&item.ActiveAudioMixVersionID,
		&item.Settings,
		&item.Revision,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func validProjectAudioSettings(strategy, requirement string) bool {
	validStrategy := strategy == "native_av" || strategy == "hybrid" || strategy == "tts_postdub"
	validRequirement := requirement == "preferred" || requirement == "required" || requirement == "disabled"
	return validStrategy && validRequirement
}

func validProjectTimebase(timebase int64, fpsNumerator, fpsDenominator int) bool {
	return timebase > 0 && fpsNumerator > 0 && fpsDenominator > 0 && timebase*int64(fpsDenominator)%int64(fpsNumerator) == 0
}

func normalizedProjectString(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func normalizedOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "request body is invalid", err.Error(), false)
		return false
	}
	return true
}

func organizationID(r *http.Request, principal auth.Principal) string {
	if header := strings.TrimSpace(r.Header.Get("X-Organization-Id")); header != "" {
		return header
	}
	return principal.OrganizationID
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var upstreamErr *provider.UpstreamError
	var accessErr authz.AccessError
	var catalogErr provider.CatalogError
	var appErr apiError
	standardErr, hasStandardErr := provider.StandardErrorFromError(err)
	switch {
	case errors.As(err, &appErr):
		httpx.WriteError(w, r, appErr.Status, appErr.Code, appErr.Message, appErr.Details, appErr.Retryable)
	case errors.As(err, &accessErr):
		httpx.WriteError(w, r, http.StatusForbidden, "ACCESS_DENIED", "missing permission "+accessErr.Permission, accessDeniedDetails(accessErr), false)
	case errors.As(err, &catalogErr):
		status := http.StatusUnprocessableEntity
		if catalogErr.Code == provider.CodeProviderPresetNotFound {
			status = http.StatusNotFound
		}
		httpx.WriteError(w, r, status, catalogErr.Code, catalogErr.Message, nil, false)
	case errors.Is(err, authz.ErrAccessDenied):
		httpx.WriteError(w, r, http.StatusForbidden, "ACCESS_DENIED", "access denied", nil, false)
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is invalid", nil, false)
	case errors.Is(err, auth.ErrEmailExists):
		httpx.WriteError(w, r, http.StatusConflict, "EMAIL_EXISTS", "email already exists", nil, false)
	case errors.Is(err, auth.ErrUnauthorized):
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required", nil, false)
	case errors.Is(err, auth.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "permission denied", nil, false)
	case errors.Is(err, auth.ErrSetupComplete):
		httpx.WriteError(w, r, http.StatusConflict, "SETUP_ALREADY_COMPLETED", "system setup has already been completed", nil, false)
	case errors.Is(err, auth.ErrPublicRegistrationDisabled):
		httpx.WriteError(w, r, http.StatusForbidden, "PUBLIC_REGISTRATION_DISABLED", "public registration is disabled", nil, false)
	case hasStandardErr:
		httpx.WriteError(w, r, provider.HTTPStatusForStandardError(standardErr), standardErr.Code, standardErr.Message, standardErr, standardErr.Retryable)
	case errors.Is(err, provider.ErrValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "request is invalid", fmt.Sprintf("%v", err), false)
	case errors.Is(err, provider.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "resource conflict", fmt.Sprintf("%v", err), false)
	case errors.Is(err, provider.ErrProviderGatewayRequired):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, provider.CodeProviderGatewayRequired, "provider gateway is required", fmt.Sprintf("%v", err), false)
	case errors.As(err, &upstreamErr):
		standard := provider.NormalizeUpstreamError(upstreamErr)
		httpx.WriteError(w, r, provider.HTTPStatusForStandardError(&standard), standard.Code, standard.Message, standard, standard.Retryable)
	case errors.Is(err, pgx.ErrNoRows):
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource was not found", nil, false)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", fmt.Sprintf("%v", err), false)
	}
}

func publicRegistrationAllowed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("CINEWEAVE_ALLOW_PUBLIC_REGISTRATION")), "true")
}

func accessDeniedDetails(err authz.AccessError) map[string]any {
	resourceID := err.Resource.OrganizationID
	if err.Resource.ProjectID != "" {
		resourceID = err.Resource.ProjectID
	} else if err.Resource.WorkspaceID != "" {
		resourceID = err.Resource.WorkspaceID
	}
	return map[string]any{
		"permission":   err.Permission,
		"resourceType": string(err.Resource.Type),
		"resourceId":   resourceID,
	}
}
