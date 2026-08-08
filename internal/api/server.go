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
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/Einzieg/cineweave/internal/videoproduction"
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
	commerce                     *commercepkg.Service
	commerceCatalog              *commercepkg.CatalogService
	commerceDirect               *commercepkg.DirectVideoService
	commerceDerivations          *commercepkg.ScriptDerivationService
	storage                      *storage.Client
	temporal                     temporalClient
	editionRuntime               *editionpkg.Runtime
	projectControl               *projectControlExecutor
	projectControlMCP            *controlmcp.Handler
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
	ID                         string                          `json:"id"`
	OrganizationID             string                          `json:"organizationId"`
	WorkspaceID                string                          `json:"workspaceId"`
	Name                       string                          `json:"name"`
	Description                *string                         `json:"description,omitempty"`
	ProjectKind                commercepkg.ProjectKind         `json:"projectKind"`
	ProjectType                *string                         `json:"projectType,omitempty"`
	ContentType                *string                         `json:"contentType,omitempty"`
	AspectRatio                *string                         `json:"aspectRatio,omitempty"`
	VideoRatio                 string                          `json:"videoRatio"`
	ArtStyle                   string                          `json:"artStyle"`
	DirectorManual             string                          `json:"directorManual"`
	VisualManual               string                          `json:"visualManual"`
	ImageModelProfileKey       string                          `json:"imageModelProfileKey"`
	VideoModelProfileKey       string                          `json:"videoModelProfileKey"`
	ScriptModelProfileKey      string                          `json:"scriptModelProfileKey"`
	TTSModelProfileKey         string                          `json:"ttsModelProfileKey"`
	ASRModelProfileKey         string                          `json:"asrModelProfileKey"`
	AudioStrategy              string                          `json:"audioStrategy"`
	AudioRequirement           string                          `json:"audioRequirement"`
	AudioConfigurationRevision int                             `json:"audioConfigurationRevision"`
	Revision                   int64                           `json:"revision"`
	LifecycleStatus            string                          `json:"lifecycleStatus"`
	DeletionRevision           int64                           `json:"deletionRevision"`
	DeletionRequestedAt        *time.Time                      `json:"deletionRequestedAt,omitempty"`
	ImageQuality               string                          `json:"imageQuality"`
	TimelineTimebase           int64                           `json:"timelineTimebase"`
	FPSNumerator               int                             `json:"fpsNumerator"`
	FPSDenominator             int                             `json:"fpsDenominator"`
	ActiveScriptID             *string                         `json:"activeScriptId,omitempty"`
	ActiveFinalVideoVersionID  *string                         `json:"activeFinalVideoVersionId,omitempty"`
	ActiveAudioMixVersionID    *string                         `json:"activeAudioMixVersionId,omitempty"`
	VideoProductionBinding     *videoproduction.Binding        `json:"videoProductionBinding,omitempty"`
	ProductionGeneration       *videoproduction.Generation     `json:"productionGeneration,omitempty"`
	VideoProductionState       string                          `json:"videoProductionState"`
	VideoProductionLocked      bool                            `json:"videoProductionLocked"`
	Settings                   json.RawMessage                 `json:"settings"`
	CreatedAt                  time.Time                       `json:"createdAt"`
	UpdatedAt                  time.Time                       `json:"updatedAt"`
	SetupSessionID             *string                         `json:"setupSessionId,omitempty"`
	SetupState                 *string                         `json:"setupState,omitempty"`
	WorkflowTemplateVersionID  *string                         `json:"workflowTemplateVersionId,omitempty"`
	SetupConfigurationHash     *string                         `json:"setupConfigurationHash,omitempty"`
	ScriptUnitDefaults         *commercepkg.ScriptUnitDefaults `json:"scriptUnitDefaults,omitempty"`
}

func New(pool *pgxpool.Pool, authService *auth.Service, providerService *provider.Service, storageClient *storage.Client, temporalClient client.Client, authorizers ...*authz.Authorizer) *Server {
	authorizer := authz.New(pool)
	if len(authorizers) > 0 && authorizers[0] != nil {
		authorizer = authorizers[0]
	}
	server := &Server{
		db: pool, auth: authService, authorizer: authorizer, providers: providerService,
		commerce:        commercepkg.NewService(commercepkg.NewRepository()),
		commerceCatalog: commercepkg.NewCatalogService(commercepkg.NewRepository()),
		commerceDirect:  commercepkg.NewDirectVideoService(commercepkg.NewRepository()),
		commerceDerivations: commercepkg.NewScriptDerivationService(
			commercepkg.NewRepository(),
		),
		storage: storageClient, temporal: temporalClient,
		editionRuntime: editionpkg.MustCommunityRuntime(),
	}
	projectControl, err := newProjectControlExecutor(server)
	if err != nil {
		panic(fmt.Sprintf("initialize project control executor: %v", err))
	}
	server.projectControl = projectControl
	return server
}

func (s *Server) Handler() http.Handler {
	if s.editionRuntime == nil {
		s.editionRuntime = editionpkg.MustCommunityRuntime()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.HealthHandler("api"))
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /api/system/status", s.systemStatus)
	mux.HandleFunc("GET /api/system/edition", s.systemEdition)
	mux.HandleFunc("GET /api/system/project-control-diagnostics", s.withAuth(s.getSystemProjectControlDiagnostics))
	mux.HandleFunc("GET /api/system/setup-state", s.systemSetupState)
	mux.HandleFunc("POST /api/system/setup", s.systemSetup)
	mux.HandleFunc("GET /api/system/organizations", s.withAuth(s.listSystemOrganizations))
	mux.HandleFunc("POST /api/system/organizations", s.withAuth(s.createSystemOrganization))
	mux.HandleFunc("GET /api/system/organizations/{organizationId}/members", s.withAuth(s.listSystemOrganizationMembers))
	mux.HandleFunc("POST /api/system/organizations/{organizationId}/members", s.withAuth(s.createSystemOrganizationMember))
	mux.HandleFunc("PATCH /api/system/organizations/{organizationId}/members/{userId}", s.withAuth(s.updateSystemOrganizationMember))

	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/register-with-invitation", s.registerWithOrganizationInvitation)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/select-organization", s.selectOrganization)
	mux.HandleFunc("POST /api/auth/switch-organization", s.withAuth(s.switchOrganization))
	mux.HandleFunc("POST /api/auth/refresh", s.refresh)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/password-reset/complete", s.completePasswordReset)
	mux.HandleFunc("GET /api/auth/me", s.withAuth(s.me))
	mux.HandleFunc("GET /api/me/entitlements", s.withAuth(s.meEntitlements))
	mux.HandleFunc("GET /api/me/codex-control-key", s.withAuth(s.getCodexControlKey))
	mux.HandleFunc("POST /api/me/codex-control-key", s.withAuth(s.createCodexControlKey))
	mux.HandleFunc("POST /api/me/codex-control-key/rotate", s.withAuth(s.rotateCodexControlKey))
	mux.HandleFunc("DELETE /api/me/codex-control-key", s.withAuth(s.revokeCodexControlKey))
	mux.HandleFunc("GET /api/project-control/commands", s.withAuth(s.listProjectControlCommands))
	mux.HandleFunc("GET /api/project-control/commands/{commandId}", s.withAuth(s.getProjectControlCommand))
	mux.HandleFunc("GET /api/project-control/commands/{commandId}/events", s.withAuth(s.listProjectControlCommandEvents))
	mux.HandleFunc("POST /api/project-control/commands/{commandId}/wait", s.withAuth(s.waitProjectControlCommand))
	mux.HandleFunc("POST /api/project-control/commands/{commandId}/cancel", s.withAuth(s.cancelProjectControlCommand))
	mux.HandleFunc("POST /api/project-control/commands/{commandId}/retry", s.withAuth(s.retryProjectControlCommand))
	mux.HandleFunc("POST /api/project-control/commands/{commandId}/resolve", s.withAuth(s.resolveProjectControlCommand))
	mux.HandleFunc("PATCH /api/auth/me", s.withAuth(s.updateProfile))
	mux.HandleFunc("POST /api/auth/me/username", s.withAuth(s.setInitialUsername))
	mux.HandleFunc("POST /api/organization-invitations/resolve", s.resolveOrganizationInvitation)
	mux.HandleFunc("POST /api/organization-invitations/accept", s.withAuth(s.acceptOrganizationInvitation))
	mux.HandleFunc("POST /api/provider-webhooks/{providerAccountId}/{webhookSecret}", s.providerWebhook)
	mcpHandler, err := controlmcp.NewHandler(s.auth, s.projectControl, controlmcp.Options{
		Version:        strings.TrimSpace(os.Getenv("CINEWEAVE_RELEASE_ID")),
		AllowedOrigins: projectControlMCPOrigins(),
	})
	if err != nil {
		panic(fmt.Sprintf("initialize project control MCP: %v", err))
	}
	s.projectControlMCP = mcpHandler
	mux.Handle("/mcp", mcpHandler)

	mux.HandleFunc("GET /api/organizations", s.withAuth(s.listOrganizations))
	mux.HandleFunc("GET /api/organizations/{organizationId}", s.withAuth(s.getOrganization))
	mux.HandleFunc("PATCH /api/organizations/{organizationId}", s.withAuth(s.updateOrganization))
	mux.HandleFunc("POST /api/organizations/{organizationId}/leave", s.withAuth(s.leaveOrganization))
	mux.HandleFunc("GET /api/organizations/{organizationId}/audit-logs", s.withAuth(s.listOrganizationAuditLogs))
	mux.HandleFunc("GET /api/organizations/{organizationId}/members", s.withAuth(s.listOrganizationMembers))
	mux.HandleFunc("GET /api/organizations/{organizationId}/members/{userId}", s.withAuth(s.getOrganizationMember))
	mux.HandleFunc("PATCH /api/organizations/{organizationId}/members/{userId}", s.withAuth(s.updateOrganizationMember))
	mux.HandleFunc("DELETE /api/organizations/{organizationId}/members/{userId}", s.withAuth(s.removeOrganizationMember))
	mux.HandleFunc("PATCH /api/organizations/{organizationId}/members/{userId}/profile", s.withAuth(s.updateOrganizationMemberProfile))
	mux.HandleFunc("POST /api/organizations/{organizationId}/members/{userId}/password-reset", s.withAuth(s.issueOrganizationMemberPasswordReset))
	mux.HandleFunc("GET /api/organizations/{organizationId}/invitations", s.withAuth(s.listOrganizationInvitations))
	mux.HandleFunc("POST /api/organizations/{organizationId}/invitations", s.withAuth(s.createOrganizationInvitation))
	mux.HandleFunc("DELETE /api/organizations/{organizationId}/invitations/{invitationId}", s.withAuth(s.revokeOrganizationInvitation))

	mux.HandleFunc("GET /api/workspaces", s.withAuth(s.listWorkspaces))
	mux.HandleFunc("POST /api/workspaces", s.withAuth(s.createWorkspace))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}", s.withAuth(s.getWorkspace))
	mux.HandleFunc("GET /api/teams", s.withAuth(s.listTeams))
	mux.HandleFunc("POST /api/teams", s.withAuth(s.createTeam))
	mux.HandleFunc("GET /api/teams/{teamId}", s.withAuth(s.getTeam))
	mux.HandleFunc("GET /api/teams/{teamId}/impact", s.withAuth(s.getTeamImpact))
	mux.HandleFunc("PATCH /api/teams/{teamId}", s.withAuth(s.updateTeam))
	mux.HandleFunc("DELETE /api/teams/{teamId}", s.withAuth(s.deleteTeam))
	mux.HandleFunc("GET /api/teams/{teamId}/members", s.withAuth(s.listTeamMembers))
	mux.HandleFunc("POST /api/teams/{teamId}/members", s.withAuth(s.addTeamMember))
	mux.HandleFunc("DELETE /api/teams/{teamId}/members/{userId}", s.withAuth(s.removeTeamMember))
	mux.HandleFunc("GET /api/roles", s.withAuth(s.listRoles))
	mux.HandleFunc("POST /api/roles", s.withAuth(s.createCustomRole))
	mux.HandleFunc("GET /api/roles/{roleId}", s.withAuth(s.getRole))
	mux.HandleFunc("PATCH /api/roles/{roleId}", s.withAuth(s.updateCustomRole))
	mux.HandleFunc("DELETE /api/roles/{roleId}", s.withAuth(s.deleteCustomRole))
	mux.HandleFunc("GET /api/roles/{roleId}/impact", s.withAuth(s.getRoleImpact))
	mux.HandleFunc("GET /api/permissions", s.withAuth(s.listPermissions))
	mux.HandleFunc("GET /api/role-bindings", s.withAuth(s.listRoleBindings))
	mux.HandleFunc("POST /api/role-bindings", s.withAuth(s.createRoleBinding))
	mux.HandleFunc("DELETE /api/role-bindings/{roleBindingId}", s.withAuth(s.deleteRoleBinding))

	mux.HandleFunc("GET /api/projects", s.withAuth(s.listProjects))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/commerce/project-options", s.withAuth(s.getCommerceProjectOptions))
	mux.HandleFunc("GET /api/video-production-profiles", s.withAuth(s.listVideoProductionProfiles))
	mux.HandleFunc("POST /api/projects", s.withAuth(s.createProject))
	mux.HandleFunc("GET /api/projects/{projectId}", s.withAuth(s.getProject))
	mux.HandleFunc("GET /api/projects/{projectId}/video-production-profile", s.withAuth(s.getProjectVideoProductionProfile))
	mux.HandleFunc("GET /api/projects/{projectId}/video-production-profile/compatibility", s.withAuth(s.getProjectVideoProductionCompatibility))
	mux.HandleFunc("POST /api/projects/{projectId}/video-production/rebuild-impact", s.withAuth(s.getProjectVideoProductionRebuildImpact))
	mux.HandleFunc("POST /api/projects/{projectId}/video-production/rebuilds", s.withAuth(s.createProjectVideoProductionRebuild))
	mux.HandleFunc("GET /api/projects/{projectId}/video-production/rebuilds/current", s.withAuth(s.getCurrentProjectVideoProductionRebuild))
	mux.HandleFunc("GET /api/projects/{projectId}/video-production/rebuilds/{rebuildId}", s.withAuth(s.getProjectVideoProductionRebuild))
	mux.HandleFunc("GET /api/projects/{projectId}/video-production/rebuilds/{rebuildId}/items", s.withAuth(s.listProjectVideoProductionRebuildItems))
	mux.HandleFunc("POST /api/projects/{projectId}/video-production/rebuilds/{rebuildId}/retry-failed", s.withAuth(s.retryFailedProjectVideoProductionRebuildItems))
	mux.HandleFunc("PATCH /api/projects/{projectId}", s.withAuth(s.updateProject))
	mux.HandleFunc("GET /api/projects/{projectId}/deletion-impact", s.withAuth(s.getProjectDeletionImpact))
	mux.HandleFunc("POST /api/projects/{projectId}/deletion-requests", s.withAuth(s.createProjectDeletionRequest))
	mux.HandleFunc("GET /api/projects/{projectId}/deletion-requests/{requestId}", s.withAuth(s.getProjectDeletionRequest))
	mux.HandleFunc("POST /api/projects/{projectId}/deletion-requests/{requestId}/retry", s.withAuth(s.retryProjectDeletionRequest))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/setup-sessions/{setupSessionId}", s.withAuth(s.getCommerceSetupSession))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/setup-sessions/{setupSessionId}/complete", s.withAuth(s.completeCommerceSetupSession))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/setup-sessions/{setupSessionId}/restart", s.withAuth(s.restartCommerceSetupSession))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/setup-sessions/{setupSessionId}/language-confirmation", s.withAuth(s.confirmCommerceSetupLanguage))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/setup-sessions/{setupSessionId}/abandon", s.withAuth(s.abandonCommerceSetupSession))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product", s.withAuth(s.getCommerceProduct))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product", s.withAuth(s.createCommerceProductVersion))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/product", s.withAuth(s.updateCommerceProduct))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product/versions", s.withAuth(s.listCommerceProductVersions))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product/versions/{versionId}", s.withAuth(s.getCommerceProductVersion))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product/versions", s.withAuth(s.createCommerceProductVersion))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product/references", s.withAuth(s.listCommerceProductReferences))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product/references/upload-url", s.withAuth(s.createCommerceProductReferenceUploadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product/references", s.withAuth(s.completeCommerceProductReferenceUpload))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/product/references/{referenceId}", s.withAuth(s.updateCommerceProductReference))
	mux.HandleFunc("DELETE /api/projects/{projectId}/commerce/product/references/{referenceId}", s.withAuth(s.archiveCommerceProductReference))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product/reference-packs", s.withAuth(s.listCommerceProductReferencePacks))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/product/reference-packs/{packId}", s.withAuth(s.getCommerceProductReferencePack))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product/rebuild-impact", s.withAuth(s.getCommerceProductRebuildImpact))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/product/rebuilds", s.withAuth(s.createCommerceProductRebuild))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units", s.withAuth(s.listCommerceScriptUnits))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/production-status", s.withAuth(s.getCommerceProjectProductionStatus))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/script-unit-defaults", s.withAuth(s.updateCommerceScriptUnitDefaults))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units", s.withAuth(s.createCommerceScriptUnit))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/reorder", s.withAuth(s.reorderCommerceScriptUnits))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}", s.withAuth(s.getCommerceScriptUnit))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/production-status", s.withAuth(s.getCommerceUnitProductionStatus))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/script-units/{scriptUnitId}", s.withAuth(s.updateCommerceScriptUnit))
	mux.HandleFunc("DELETE /api/projects/{projectId}/commerce/script-units/{scriptUnitId}", s.withAuth(s.archiveCommerceScriptUnit))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/duplicate", s.withAuth(s.duplicateCommerceScriptUnit))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/language-variants", s.withAuth(s.createCommerceScriptLanguageVariant))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/versions", s.withAuth(s.listCommerceScriptVersions))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/versions/{versionId}", s.withAuth(s.getCommerceScriptVersion))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/versions", s.withAuth(s.createCommerceScriptVersion))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/versions/{versionId}/activate", s.withAuth(s.activateCommerceScriptVersion))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/video-options", s.withAuth(s.getCommerceDirectVideoOptions))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/references", s.withAuth(s.listCommerceScriptReferences))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/references/upload-url", s.withAuth(s.createCommerceScriptReferenceUploadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/references/complete", s.withAuth(s.completeCommerceScriptReferenceUpload))
	mux.HandleFunc("DELETE /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/references/{referenceId}", s.withAuth(s.archiveCommerceScriptReference))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/direct-videos", s.withAuth(s.listCommerceDirectVideos))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/direct-videos", s.withAuth(s.createCommerceDirectVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/derivations", s.withAuth(s.createCommerceScriptDerivation))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-derivations", s.withAuth(s.listCommerceScriptDerivations))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-derivations/{batchId}", s.withAuth(s.getCommerceScriptDerivation))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-derivations/{batchId}/retry-failed", s.withAuth(s.retryCommerceScriptDerivation))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-derivations/{batchId}/cancel", s.withAuth(s.cancelCommerceScriptDerivation))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/direct-videos/{jobId}", s.withAuth(s.getCommerceDirectVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/direct-videos/{jobId}/cancel", s.withAuth(s.cancelCommerceDirectVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/language-resolution", s.withAuth(s.resolveCommerceScriptLanguage))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/language-resolution", s.withAuth(s.getCommerceScriptLanguageResolution))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/language-confirmation", s.withAuth(s.confirmCommerceScriptLanguage))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/prepare", s.withAuth(s.prepareCommerceScriptUnit))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/organize", s.withAuth(s.organizeCommerceScriptUnit))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/rebuild-impact", s.withAuth(s.getCommerceScriptUnitRebuildImpact))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/rebuilds", s.withAuth(s.createCommerceScriptUnitRebuild))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/localizations", s.withAuth(s.listCommerceScriptLocalizations))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/localizations/{localizationId}", s.withAuth(s.getCommerceScriptLocalization))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/localizations", s.withAuth(s.createCommerceScriptLocalization))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/localizations/{localizationId}/activate", s.withAuth(s.activateCommerceScriptLocalization))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/storyboard-plans", s.withAuth(s.listCommerceStoryboardPlans))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/generations/{scriptUnitGenerationId}/storyboard-planning-preview", s.withAuth(s.previewCommerceStoryboardPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/generations/{scriptUnitGenerationId}/storyboard-plans", s.withAuth(s.createCommerceStoryboardPlan))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/storyboard-plans/{planId}", s.withAuth(s.getCommerceStoryboardPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/storyboard-plans/{planId}/activate", s.withAuth(s.activateCommerceStoryboardPlan))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/storyboard-plans/{planId}/shots", s.withAuth(s.listCommerceStoryboardShots))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/shots/{shotId}", s.withAuth(s.updateCommerceStoryboardShot))
	mux.HandleFunc("DELETE /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/shots/{shotId}", s.withAuth(s.deleteCommerceStoryboardShot))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/shots/reorder", s.withAuth(s.reorderCommerceStoryboardShots))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/reference-images/generate-batch", s.withAuth(s.generateCommerceReferenceImageBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/video-prompts/generate-batch", s.withAuth(s.generateCommerceVideoPromptBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/shot-videos/generate-batch", s.withAuth(s.generateCommerceShotVideoBatch))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/timelines", s.withAuth(s.listCommerceTimelines))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/timelines/prepare", s.withAuth(s.prepareCommerceTimeline))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/timelines/{timelineId}", s.withAuth(s.getCommerceTimeline))
	mux.HandleFunc("PATCH /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/timelines/{timelineId}", s.withAuth(s.updateCommerceTimeline))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/final-videos/compose", s.withAuth(s.composeCommerceFinalVideo))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/final-videos", s.withAuth(s.listCommerceFinalVideos))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/final-videos/{finalVideoVersionId}", s.withAuth(s.getCommerceFinalVideo))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/final-videos/{finalVideoVersionId}/activate", s.withAuth(s.activateCommerceFinalVideo))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/production-runs", s.withAuth(s.listCommerceProductionRuns))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/production-runs/{runId}", s.withAuth(s.getCommerceProductionRun))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/production-runs/{runId}/retry-failed", s.withAuth(s.retryFailedCommerceProductionRun))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/production-runs/{runId}/cancel", s.withAuth(s.cancelCommerceProductionRun))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-unit-batches", s.withAuth(s.listCommerceScriptUnitBatches))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-unit-batches", s.withAuth(s.createCommerceScriptUnitBatch))
	mux.HandleFunc("GET /api/projects/{projectId}/commerce/script-unit-batches/{coordinatorId}", s.withAuth(s.getCommerceScriptUnitBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-unit-batches/{coordinatorId}/retry-failed", s.withAuth(s.retryCommerceScriptUnitBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/commerce/script-unit-batches/{coordinatorId}/cancel", s.withAuth(s.cancelCommerceScriptUnitBatch))
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
	mux.HandleFunc("DELETE /api/projects/{projectId}/scripts/{scriptId}", s.withAuth(s.deleteScript))
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
	mux.HandleFunc("POST /api/projects/{projectId}/agent/sessions", s.withAuth(s.createProjectAgentSession))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/sessions", s.withAuth(s.listProjectAgentSessions))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/sessions/{sessionId}/messages", s.withAuth(s.listProjectAgentMessages))
	mux.HandleFunc("GET /api/projects/{projectId}/agent/tools", s.withAuth(s.listAgentTools))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/image-attachments/upload-url", s.withAuth(s.createAgentImageAttachmentUploadURL))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/image-attachments/{attachmentId}/complete", s.withAuth(s.completeAgentImageAttachment))
	mux.HandleFunc("POST /api/projects/{projectId}/agent/image-attachments/{attachmentId}/assign", s.withAuth(s.assignAgentImageAttachment))
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
	mux.HandleFunc("POST /api/projects/{projectId}/shot-asset-requirements/review-batch", s.withAuth(s.batchReviewShotAssetRequirements))
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
	mux.HandleFunc("POST /api/projects/{projectId}/video-prompts/generate-batch", s.withAuth(s.generateVideoPromptsBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/shot-videos/generate-batch", s.withAuth(s.generateShotVideosBatch))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots", s.withAuth(s.createStoryboardShot))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/reorder", s.withAuth(s.reorderStoryboardShots))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/detail", s.withAuth(s.getStoryboardShotDetail))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/state", s.withAuth(s.getStoryboardShotState))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/state/replan", s.withAuth(s.replanStoryboardShotState))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/transition", s.withAuth(s.getStoryboardShotTransition))
	mux.HandleFunc("PATCH /api/projects/{projectId}/storyboard-shots/{shotId}/transition", s.withAuth(s.updateStoryboardShotTransition))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/anchors", s.withAuth(s.listStoryboardShotAnchors))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/generate", s.withAuth(s.generateStoryboardShotAnchor))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/{anchorId}/approve", s.withAuth(s.approveStoryboardShotAnchor))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/{anchorId}/reject", s.withAuth(s.rejectStoryboardShotAnchor))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/reference-pack", s.withAuth(s.getStoryboardShotReferencePack))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/storyboard-sheet", s.withAuth(s.getStoryboardShotStoryboardSheet))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/video-prompt-plan", s.withAuth(s.getStoryboardShotVideoPromptPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/video-prompt-plan/revisions", s.withAuth(s.createManualVideoPromptPlanRevision))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/video-render-plan", s.withAuth(s.getStoryboardShotRenderPlan))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan", s.withAuth(s.getStoryboardShotRenderPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan", s.withAuth(s.createStoryboardShotRenderPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-verification", s.withAuth(s.verifyStoryboardShotRenderPlanAudio))
	mux.HandleFunc("POST /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-review", s.withAuth(s.startNativeAudioReview))
	mux.HandleFunc("GET /api/projects/{projectId}/storyboard-shots/{shotId}/render-plan/audio-reviews", s.withAuth(s.listNativeAudioReviews))
	mux.HandleFunc("POST /api/projects/{projectId}/video-prompts/{promptPlanId}/approve", s.withAuth(s.approveVideoPromptPlan))
	mux.HandleFunc("POST /api/projects/{projectId}/video-prompts/{promptPlanId}/reject", s.withAuth(s.rejectVideoPromptPlan))
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

	mux.HandleFunc("GET /api/provider-models/available", s.withAuth(s.listAvailableProviderModels))
	mux.HandleFunc("PATCH /api/provider-models/{modelId}", s.withAuth(s.withProviderConfigurationWriteGate(s.updateAvailableProviderModel)))
	mux.HandleFunc("GET /api/provider-catalog", s.withAuth(s.withProviderAdministration(s.listProviderCatalog)))
	mux.HandleFunc("GET /api/provider-catalog/{providerKey}", s.withAuth(s.withProviderAdministration(s.getProviderCatalogEntry)))
	mux.HandleFunc("POST /api/provider-catalog/{providerKey}/install", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.installProviderCatalogEntry))))
	mux.HandleFunc("GET /api/providers/connectors", s.withAuth(s.withProviderAdministration(s.listProviderConnectors)))
	mux.HandleFunc("POST /api/providers/connectors/import", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.importProviderConnector))))
	mux.HandleFunc("GET /api/providers/accounts", s.withAuth(s.withProviderAdministration(s.listProviderAccounts)))
	mux.HandleFunc("POST /api/providers/accounts", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.createProviderAccount))))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}", s.withAuth(s.withProviderAdministration(s.getProviderAccount)))
	mux.HandleFunc("PATCH /api/providers/accounts/{accountId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.updateProviderAccount))))
	mux.HandleFunc("DELETE /api/providers/accounts/{accountId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.deleteProviderAccount))))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}/credentials", s.withAuth(s.withProviderAdministration(s.listProviderCredentials)))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.createProviderCredential))))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials/rotate", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.rotateProviderCredential))))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials/{credentialId}/rotate", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.rotateProviderCredentialByID))))
	mux.HandleFunc("DELETE /api/providers/accounts/{accountId}/credentials/{credentialId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.revokeProviderCredential))))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/credentials/{credentialId}/discover-models", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.discoverProviderCredentialModels))))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/discover-models", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.discoverProviderModels))))
	mux.HandleFunc("GET /api/providers/accounts/{accountId}/models", s.withAuth(s.withProviderAdministration(s.listProviderModels)))
	mux.HandleFunc("POST /api/providers/accounts/{accountId}/models", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.createProviderModel))))
	mux.HandleFunc("PATCH /api/providers/models/{modelId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.updateProviderModel))))
	mux.HandleFunc("DELETE /api/providers/models/{modelId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.deleteProviderModel))))
	mux.HandleFunc("POST /api/providers/models/{modelId}/test", s.withAuth(s.withProviderAdministration(s.testProviderModel)))
	mux.HandleFunc("GET /api/providers/models/{modelId}/video-capability-attestations", s.withAuth(s.withProviderAdministration(s.listProviderModelVideoCapabilityAttestations)))
	mux.HandleFunc("POST /api/providers/models/{modelId}/video-capability-attestations", s.withAuth(s.withProviderAdministration(s.createProviderModelVideoCapabilityAttestation)))
	mux.HandleFunc("POST /api/providers/models/{modelId}/video-capability-attestations/{attestationId}/revoke", s.withAuth(s.withProviderAdministration(s.revokeProviderModelVideoCapabilityAttestation)))
	mux.HandleFunc("POST /api/providers/models/{modelId}/video-capabilities/verify", s.withAuth(s.withProviderAdministration(s.verifyProviderModelVideoCapabilities)))
	mux.HandleFunc("POST /api/providers/manifests/validate", s.withAuth(s.withProviderAdministration(s.validateProviderManifest)))
	mux.HandleFunc("POST /api/providers/manifests/test-run", s.withAuth(s.withProviderAdministration(s.runProviderManifestTest)))
	mux.HandleFunc("GET /api/model-profiles", s.withAuth(s.listModelProfiles))
	mux.HandleFunc("POST /api/model-profiles", s.withAuth(s.withProviderConfigurationWriteGate(s.createModelProfile)))
	mux.HandleFunc("PATCH /api/model-profiles/{profileId}", s.withAuth(s.withProviderConfigurationWriteGate(s.updateModelProfile)))
	mux.HandleFunc("POST /api/model-profiles/{profileId}/bindings", s.withAuth(s.withProviderConfigurationWriteGate(s.createModelProfileBinding)))
	mux.HandleFunc("PATCH /api/model-profiles/{profileId}/bindings/{bindingId}", s.withAuth(s.withProviderConfigurationWriteGate(s.updateModelProfileBinding)))
	mux.HandleFunc("DELETE /api/model-profiles/{profileId}/bindings/{bindingId}", s.withAuth(s.withProviderConfigurationWriteGate(s.deleteModelProfileBinding)))
	mux.HandleFunc("GET /api/provider-call-logs", s.withAuth(s.listProviderCallLogs))
	mux.HandleFunc("GET /api/provider-usage/summary", s.withAuth(s.getProviderUsageSummary))
	mux.HandleFunc("GET /api/provider-limit-policies", s.withAuth(s.withProviderAdministration(s.listProviderLimitPolicies)))
	mux.HandleFunc("POST /api/provider-limit-policies", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.createProviderLimitPolicy))))
	mux.HandleFunc("GET /api/provider-limit-policies/{policyId}", s.withAuth(s.withProviderAdministration(s.getProviderLimitPolicy)))
	mux.HandleFunc("PATCH /api/provider-limit-policies/{policyId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.updateProviderLimitPolicy))))
	mux.HandleFunc("DELETE /api/provider-limit-policies/{policyId}", s.withAuth(s.withProviderAdministration(s.withProviderConfigurationWriteGate(s.deleteProviderLimitPolicy))))
	mux.HandleFunc("GET /api/provider-circuit-states", s.withAuth(s.withProviderAdministration(s.listProviderCircuitStates)))
	mux.HandleFunc("POST /api/provider-circuit-states/{stateId}/reset", s.withAuth(s.withProviderAdministration(s.resetProviderCircuitState)))
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
	mux.HandleFunc("POST /api/projects/{projectId}/workflow-activity/clear-completed", s.withAuth(s.clearCompletedWorkflowActivity))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}", s.withAuth(s.getWorkflowRun))
	mux.HandleFunc("POST /api/workflow-runs/{workflowRunId}/cancel", s.withAuth(s.cancelWorkflowRun))
	mux.HandleFunc("POST /api/workflow-runs/{workflowRunId}/retry-failed", s.withAuth(s.retryFailedWorkflowRun))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/derived-asset-batch", s.withAuth(s.getDerivedAssetBatchActivity))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/nodes", s.withAuth(s.listWorkflowNodeRuns))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/video-production", s.withAuth(s.getWorkflowVideoProductionActivity))
	mux.HandleFunc("GET /api/workflow-runs/{workflowRunId}/shots", s.withAuth(s.listWorkflowRunShots))
	mux.HandleFunc("GET /api/artifacts", s.withAuth(s.listArtifacts))
	mux.HandleFunc("GET /api/artifacts/{artifactId}", s.withAuth(s.getArtifact))
	mux.HandleFunc("POST /api/artifacts/{artifactId}/preview-url", s.withAuth(s.createArtifactPreviewURL))
	mux.HandleFunc("GET /api/media-files/{mediaFileId}", s.withAuth(s.getMediaFile))
	mux.HandleFunc("POST /api/media-files/{mediaFileId}/download-url", s.withAuth(s.createMediaFileDownloadURL))

	s.registerEditionAPIModules(mux)
	return httpx.WithCORS(httpx.WithRequestID(httpx.WithRecovery(mux)))
}

func projectControlMCPOrigins() []string {
	value := strings.TrimSpace(os.Getenv("CINEWEAVE_MCP_ALLOWED_ORIGINS"))
	if value == "" {
		return []string{
			"https://cineweave.einzieg.site",
			"http://localhost:19285",
			"http://127.0.0.1:19285",
		}
	}
	items := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
	s.notifyOrganizationCreated(
		r.Context(),
		resp.OrganizationID,
		resp.User.ID,
		req.OrganizationName,
	)
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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

func (s *Server) selectOrganization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.SelectOrganizationRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.SelectOrganization(r.Context(), req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) switchOrganization(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	w.Header().Set("Cache-Control", "no-store")
	var req auth.SwitchOrganizationRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.auth.SwitchOrganization(r.Context(), principal, req, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, resp, nil)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
	w.Header().Set("Cache-Control", "no-store")
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
	w.Header().Set("Cache-Control", "no-store")
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
	organizations, err := s.auth.Organizations(r.Context(), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	membership, err := s.auth.GetOrganizationMember(r.Context(), principal.OrganizationID, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	permissions, err := s.currentOrganizationPermissions(r.Context(), principal)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"user":           user,
		"organizationId": principal.OrganizationID,
		"workspaceId":    workspaceID,
		"organizations":  organizations,
		"membership":     membership,
		"permissions":    permissions,
	}, nil)
}

func (s *Server) setInitialUsername(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &req) {
		return
	}
	user, err := s.auth.SetInitialUsername(r.Context(), principal, req.Username)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, user, nil)
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
	page, err := s.listAccessibleOrganizations(r.Context(), principal, 100, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": page.Items, "nextCursor": page.NextCursor}, nil)
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
	page, err := s.listAccessibleWorkspaces(r.Context(), principal, orgID, 100, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": page.Items, "nextCursor": page.NextCursor}, nil)
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
	page, err := s.listAccessibleProjects(r.Context(), principal, orgID, workspaceID, 100, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": page.Items, "nextCursor": page.NextCursor}, nil)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req createProjectRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workspaceId and name are required", nil, false)
		return
	}
	classification, err := commercepkg.ResolveProjectClassification(req.ProjectKind, req.ProjectType, req.ContentType)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROJECT_KIND_CONFIGURATION_INVALID", "项目类型配置无效", map[string]any{"reason": err.Error()}, false)
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
	directorManualPromptVersionID := normalizedProjectString(req.DirectorManualPromptVersionID, "")
	visualManualPromptVersionID := normalizedProjectString(req.VisualManualPromptVersionID, "")
	imageModelProfileKey := normalizedProjectString(req.ImageModelProfileKey, "image_generation_default")
	videoModelProfileKey := normalizedProjectString(req.VideoModelProfileKey, "video_generation_default")
	scriptModelProfileKey := normalizedProjectString(req.ScriptModelProfileKey, "script_agent_default")
	ttsModelProfileKey := normalizedProjectString(req.TTSModelProfileKey, "tts_generation_default")
	asrModelProfileKey := normalizedProjectString(req.ASRModelProfileKey, "audio_transcription_default")
	audioStrategy := normalizedProjectString(req.AudioStrategy, "native_av")
	audioRequirement := normalizedProjectString(req.AudioRequirement, "preferred")
	if classification.Kind.IsCommerce() && audioStrategy == "external_audio" {
		// Commerce exposes a provider-neutral external audio choice while the
		// shared production schema stores the existing post-dub strategy.
		audioStrategy = "tts_postdub"
	}
	if !validProjectAudioSettings(audioStrategy, audioRequirement) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "audioStrategy or audioRequirement is invalid", nil, false)
		return
	}
	imageQuality := normalizedProjectString(req.ImageQuality, "standard")
	profileKey := normalizedProjectString(req.VideoProductionProfileKey, videoproduction.ProfileSingleFrameI2V)
	compatibilityPolicy := normalizedProjectString(req.CompatibilityPolicy, videoproduction.CompatibilityStrict)
	if !videoproduction.FeatureEnabled() && profileKey != videoproduction.ProfileSingleFrameI2V {
		s.writeVideoProductionError(w, r, videoproduction.NewError(
			videoproduction.CodeProfileUnavailable,
			"视频生产方案功能尚未启用，当前只能创建图生视频项目",
			false,
		))
		return
	}
	if req.VideoProductionProfileVersion != nil && *req.VideoProductionProfileVersion <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "videoProductionProfileVersion must be positive", nil, false)
		return
	}
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
	err = s.db.QueryRow(r.Context(), `SELECT organization_id FROM workspaces WHERE id = $1`, req.WorkspaceID).Scan(&orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectWrite, authz.Resource{WorkspaceID: req.WorkspaceID}) {
		return
	}
	if classification.Kind.IsCommerce() {
		s.createCommerceProjectDraft(w, r, principal, orgID, req, projectCreateOptions{
			Settings:         settings,
			VideoRatio:       videoRatio,
			AspectRatio:      aspectRatio,
			ImageQuality:     imageQuality,
			AudioStrategy:    audioStrategy,
			AudioRequirement: audioRequirement,
			TimelineTimebase: timelineTimebase,
			FPSNumerator:     fpsNumerator,
			FPSDenominator:   fpsDenominator,
		})
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	profileVersion, err := videoproduction.ResolveProfileVersion(
		r.Context(),
		tx,
		profileKey,
		req.VideoProductionProfileVersion,
		true,
	)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	identity := videoproduction.NewIdentity()

	var item Project
	err = tx.QueryRow(r.Context(), `
		INSERT INTO projects(
			id, organization_id, workspace_id, name, description, project_kind, project_type, content_type, aspect_ratio,
			video_ratio, art_style, director_manual, visual_manual,
			image_model_profile_key, video_model_profile_key, script_model_profile_key,
			tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement,
			image_quality, timeline_timebase, fps_numerator, fps_denominator, settings, created_by,
			active_video_production_generation_id, video_production_generation_no,
			video_production_state, video_production_locked
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, 1, 'storyboard_required', false)
		RETURNING id, organization_id, workspace_id, name, description, project_kind, project_type, content_type, aspect_ratio,
		          video_ratio, art_style, director_manual, visual_manual,
		          image_model_profile_key, video_model_profile_key, script_model_profile_key,
		          tts_model_profile_key, asr_model_profile_key, audio_strategy, audio_requirement, audio_configuration_revision,
		          image_quality, timeline_timebase, fps_numerator, fps_denominator,
			active_script_id::text, active_final_video_version_id::text, active_audio_mix_version_id::text,
			settings, revision, created_at, updated_at
	`, identity.ProjectID, orgID, req.WorkspaceID, strings.TrimSpace(req.Name), req.Description, classification.Kind, classification.ProjectType, classification.ContentType, aspectRatio,
		videoRatio, artStyle, "", "", imageModelProfileKey, videoModelProfileKey, scriptModelProfileKey,
		ttsModelProfileKey, asrModelProfileKey, audioStrategy, audioRequirement,
		imageQuality, timelineTimebase, fpsNumerator, fpsDenominator, settings, principal.UserID,
		identity.GenerationID).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.Description, &item.ProjectKind, &item.ProjectType, &item.ContentType, &item.AspectRatio,
			&item.VideoRatio, &item.ArtStyle, &item.DirectorManual, &item.VisualManual,
			&item.ImageModelProfileKey, &item.VideoModelProfileKey, &item.ScriptModelProfileKey,
			&item.TTSModelProfileKey, &item.ASRModelProfileKey, &item.AudioStrategy, &item.AudioRequirement, &item.AudioConfigurationRevision,
			&item.ImageQuality, &item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
			&item.ActiveScriptID, &item.ActiveFinalVideoVersionID, &item.ActiveAudioMixVersionID,
			&item.Settings, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	if err := createProjectAccessTx(r.Context(), tx, orgID, item.ID, principal.UserID); err != nil {
		s.writeError(w, r, err)
		return
	}

	directorManual := ""
	if directorManualPromptVersionID != "" {
		_, directorManual, err = s.bindProjectManualTx(r.Context(), tx, orgID, item.ID, "director", directorManualPromptVersionID, principal.UserID)
	} else {
		directorManual, err = s.bindDefaultProjectManualTx(r.Context(), tx, orgID, item.ID, "director", principal.UserID)
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item.DirectorManual = directorManual
	visualManual := ""
	if visualManualPromptVersionID != "" {
		_, visualManual, err = s.bindProjectManualTx(r.Context(), tx, orgID, item.ID, "visual", visualManualPromptVersionID, principal.UserID)
	} else {
		visualManual, err = s.bindDefaultProjectManualTx(r.Context(), tx, orgID, item.ID, "visual", principal.UserID)
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item.VisualManual = visualManual
	productionConfiguration, err := videoproduction.LoadProductionConfiguration(r.Context(), tx, item.ID)
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	binding, generation, err := videoproduction.CreateInitialBindingAndGeneration(r.Context(), tx, videoproduction.InitialBindingParams{
		Identity:            identity,
		OrganizationID:      orgID,
		CreatedBy:           principal.UserID,
		ProfileVersion:      profileVersion,
		CompatibilityPolicy: compatibilityPolicy,
		Configuration:       productionConfiguration,
	})
	if err != nil {
		s.writeVideoProductionError(w, r, err)
		return
	}
	productionIdentityPayload := mustRawJSON(map[string]any{
		"bindingId":              binding.ID,
		"bindingRevision":        binding.Revision,
		"productionGenerationId": generation.ID,
		"profileKey":             binding.ProfileKey,
		"profileVersion":         binding.ProfileVersion,
	})
	if err := events.AppendTx(r.Context(), tx, orgID, item.ID, "video.production.binding.created", "video_production_binding", binding.ID, productionIdentityPayload); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := events.AppendTx(r.Context(), tx, orgID, item.ID, "video.production.generation.activated", "production_generation", generation.ID, productionIdentityPayload); err != nil {
		s.writeError(w, r, err)
		return
	}
	item.VideoProductionBinding = &binding
	item.ProductionGeneration = &generation
	item.VideoProductionState = "storyboard_required"
	item.VideoProductionLocked = false
	if err := tx.QueryRow(r.Context(), `SELECT revision, updated_at FROM projects WHERE id = $1`, item.ID).Scan(&item.Revision, &item.UpdatedAt); err != nil {
		s.writeError(w, r, err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.notifyProjectCreated(
		r.Context(),
		orgID,
		item.ID,
		principal.UserID,
	)
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
	project, err := s.project(r, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectWrite, authz.Resource{ProjectID: project.ID}) {
		return
	}
	var req projectUpdateActionInput
	if !decode(w, r, &req) {
		return
	}
	raw := mustRawJSON(req)
	if _, err := decodeProjectUpdateActionInput(raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "project.update", raw, idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, ok := result.Data["project"]
	if !ok {
		s.writeError(w, r, newAPIError(http.StatusInternalServerError, "PROJECT_CONTROL_RESULT_INVALID", "项目更新结果缺少项目数据"))
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func projectRevisionConflict(item Project, expectedRevision int64) apiError {
	description := ""
	if item.Description != nil {
		description = *item.Description
	}
	conflict := newAPIError(http.StatusConflict, "PROJECT_REVISION_CONFLICT", "项目设置已被其他操作修改")
	conflict.Details = map[string]any{
		"expectedRevision": expectedRevision,
		"currentRevision":  item.Revision,
		"currentSnapshot": map[string]any{
			"name":        item.Name,
			"description": description,
			"revision":    item.Revision,
		},
	}
	return conflict
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.auth.ParseBearer(r.Header.Get("Authorization"))
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := s.auth.ValidatePrincipalActive(r.Context(), principal); err != nil {
			s.writeError(w, r, err)
			return
		}
		r = r.WithContext(withAPIProviderIdentity(
			r.Context(),
			principal,
			httpx.RequestIDFromContext(r.Context()),
		))
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
	item, err := s.projectIncludingDeleting(r.Context(), projectID)
	if err != nil {
		return Project{}, err
	}
	if item.LifecycleStatus == "deleting" {
		conflict := newAPIError(http.StatusConflict, "PROJECT_DELETION_IN_PROGRESS", "项目正在删除，不能继续操作")
		conflict.Retryable = false
		conflict.Details = map[string]any{
			"projectId":        item.ID,
			"deletionRevision": item.DeletionRevision,
		}
		return Project{}, conflict
	}
	return item, nil
}

func (s *Server) projectIncludingDeleting(ctx context.Context, projectID string) (Project, error) {
	item, err := scanProject(s.db.QueryRow(ctx, projectSelectSQL(`WHERE p.id = $1`), projectID))
	if err != nil {
		return Project{}, err
	}
	if err := s.attachCommerceSetupContext(ctx, s.db, &item); err != nil {
		return Project{}, err
	}
	if err := s.attachVideoProductionContext(ctx, s.db, &item); err != nil {
		return Project{}, err
	}
	return item, nil
}

func projectSelectSQL(where string) string {
	return `
		SELECT p.id, p.organization_id, p.workspace_id, p.name, p.description, p.project_kind, p.project_type, p.content_type, p.aspect_ratio,
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
		       p.image_quality, p.timeline_timebase, p.fps_numerator, p.fps_denominator,
		       p.active_script_id::text, p.active_final_video_version_id::text, p.active_audio_mix_version_id::text,
		       p.settings, p.revision, p.lifecycle_status, p.deletion_revision, p.deletion_requested_at,
		       p.created_at, p.updated_at
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
		&item.ProjectKind,
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
		&item.TimelineTimebase,
		&item.FPSNumerator,
		&item.FPSDenominator,
		&item.ActiveScriptID,
		&item.ActiveFinalVideoVersionID,
		&item.ActiveAudioMixVersionID,
		&item.Settings,
		&item.Revision,
		&item.LifecycleStatus,
		&item.DeletionRevision,
		&item.DeletionRequestedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err == nil && item.ProjectKind.IsCommerce() {
		defaults, defaultsErr := commerceScriptUnitDefaultsFromSettings(item.Settings)
		if defaultsErr != nil {
			return Project{}, defaultsErr
		}
		item.ScriptUnitDefaults = &defaults
	}
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
	var videoProductionErr videoproduction.Error
	var commerceErr commercepkg.Error
	var editionErr editionpkg.AuthorizationError
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
	case errors.As(err, &videoProductionErr):
		httpx.WriteError(w, r, videoProductionErrorStatus(videoProductionErr.Code), videoProductionErr.Code, videoProductionErr.Message, nil, videoProductionErr.Retryable)
	case errors.As(err, &commerceErr):
		httpx.WriteError(w, r, commerceErrorStatus(commerceErr.Code), commerceErr.Code, commerceErr.Message, commerceErr.Details, commerceErr.Retryable)
	case errors.As(err, &editionErr):
		httpx.WriteError(w, r, http.StatusForbidden, string(editionErr.Code), editionErr.Message, editionErr.Details, editionErr.Retryable)
	case errors.Is(err, authz.ErrAccessDenied):
		httpx.WriteError(w, r, http.StatusForbidden, "ACCESS_DENIED", "access denied", nil, false)
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "identifier or password is invalid", nil, false)
	case errors.Is(err, auth.ErrRegistrationUnavailable):
		httpx.WriteError(w, r, http.StatusConflict, "REGISTRATION_UNAVAILABLE", "registration cannot be completed with these details", nil, false)
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, r, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "too many authentication attempts", nil, true)
	case errors.Is(err, auth.ErrEmailExists):
		httpx.WriteError(w, r, http.StatusConflict, "EMAIL_EXISTS", "email already exists", nil, false)
	case errors.Is(err, auth.ErrUsernameExists):
		httpx.WriteError(w, r, http.StatusConflict, "USERNAME_EXISTS", "username already exists", nil, false)
	case errors.Is(err, auth.ErrInvalidUsername):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INVALID_USERNAME", "username is invalid", nil, false)
	case errors.Is(err, auth.ErrProfileValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROFILE_VALIDATION_FAILED", "profile request is invalid", nil, false)
	case errors.Is(err, auth.ErrSystemAdministratorRequired):
		httpx.WriteError(w, r, http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED", "system administrator access is required", nil, false)
	case errors.Is(err, auth.ErrSystemOrganizationValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SYSTEM_ORGANIZATION_VALIDATION_FAILED", "system organization request is invalid", nil, false)
	case errors.Is(err, auth.ErrSystemOwnerNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "SYSTEM_OWNER_NOT_FOUND", "initial organization owner was not found", nil, false)
	case errors.Is(err, auth.ErrSystemMemberValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SYSTEM_MEMBER_VALIDATION_FAILED", "system member request is invalid", nil, false)
	case errors.Is(err, auth.ErrSystemMemberConflict):
		httpx.WriteError(w, r, http.StatusConflict, "SYSTEM_MEMBER_CONFLICT", "member already belongs to the organization", nil, false)
	case errors.Is(err, auth.ErrSystemMemberNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "SYSTEM_MEMBER_NOT_FOUND", "member account was not found", nil, false)
	case errors.Is(err, auth.ErrControlKeyConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CODEX_CONTROL_KEY_EXISTS", "an active Codex control key already exists", nil, false)
	case errors.Is(err, auth.ErrControlKeyNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "CODEX_CONTROL_KEY_NOT_FOUND", "Codex control key was not found", nil, false)
	case errors.Is(err, auth.ErrControlKeyInvalid):
		httpx.WriteError(w, r, http.StatusUnauthorized, "CODEX_CONTROL_KEY_INVALID", "Codex control key is invalid", nil, false)
	case errors.Is(err, auth.ErrNoActiveOrganization):
		httpx.WriteError(w, r, http.StatusForbidden, "NO_ACTIVE_ORGANIZATION", "no active organization is available", nil, false)
	case errors.Is(err, auth.ErrOrganizationSelection):
		httpx.WriteError(w, r, http.StatusUnauthorized, "ORGANIZATION_SELECTION_INVALID", "organization selection is invalid or expired", nil, false)
	case errors.Is(err, auth.ErrInvitationInvalid):
		httpx.WriteError(w, r, http.StatusGone, "INVITATION_INVALID_OR_EXPIRED", "invitation is invalid or expired", nil, false)
	case errors.Is(err, auth.ErrInvitationValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INVITATION_VALIDATION_FAILED", "invitation request is invalid", nil, false)
	case errors.Is(err, auth.ErrInvitationConflict):
		httpx.WriteError(w, r, http.StatusConflict, "INVITATION_CONFLICT", "invitation conflicts with current membership", nil, false)
	case errors.Is(err, auth.ErrLastOwner):
		httpx.WriteError(w, r, http.StatusConflict, "LAST_OWNER_REQUIRED", "organization must keep an active direct owner", nil, false)
	case errors.Is(err, auth.ErrMemberLifecycle):
		httpx.WriteError(w, r, http.StatusConflict, "MEMBER_LIFECYCLE_INVALID", "member lifecycle transition is invalid", nil, false)
	case errors.Is(err, auth.ErrSharedAccountManagement):
		httpx.WriteError(w, r, http.StatusConflict, "ACCOUNT_SHARED_ACROSS_ORGANIZATIONS", "account belongs to multiple organizations", nil, false)
	case errors.Is(err, auth.ErrMemberAccountProtected):
		httpx.WriteError(w, r, http.StatusForbidden, "MEMBER_ACCOUNT_PROTECTED", "member account is protected", nil, false)
	case errors.Is(err, auth.ErrMemberProfileValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "MEMBER_PROFILE_VALIDATION_FAILED", "member profile request is invalid", nil, false)
	case errors.Is(err, auth.ErrPasswordResetInvalid):
		httpx.WriteError(w, r, http.StatusGone, "PASSWORD_RESET_INVALID_OR_EXPIRED", "password reset is invalid or expired", nil, false)
	case errors.Is(err, auth.ErrPasswordResetValidation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PASSWORD_RESET_VALIDATION_FAILED", "password reset request is invalid", nil, false)
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
	case errors.Is(err, provider.ErrModelAlreadyExists):
		httpx.WriteError(w, r, http.StatusConflict, "PROVIDER_MODEL_ALREADY_EXISTS", "provider model already exists", nil, false)
	case errors.Is(err, provider.ErrModelInUse):
		httpx.WriteError(w, r, http.StatusConflict, "PROVIDER_MODEL_IN_USE", "provider model has active asynchronous tasks or leases", nil, false)
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

func commerceErrorStatus(code string) int {
	switch code {
	case commercepkg.CodeProjectKindMismatch:
		return http.StatusConflict
	case commercepkg.CodeBindingMismatch, commercepkg.CodeGenerationMismatch,
		commercepkg.CodeRevisionConflict, commercepkg.CodeProjectLocked,
		commercepkg.CodeProjectRebuildBlocked, commercepkg.CodeIdempotencyKeyReused,
		commercepkg.CodeRunStateConflict, commercepkg.CodeSetupRevisionConflict,
		commercepkg.CodeSetupAbandoned, commercepkg.CodeProductVersionStale,
		commercepkg.CodeProductReconfigure, commercepkg.CodeProductPrimaryImage,
		commercepkg.CodeScriptUnitArchived, commercepkg.CodeScriptUnitRevision,
		commercepkg.CodeScriptVersionStale, commercepkg.CodeScriptRebuildRequired,
		commercepkg.CodeScriptRebuildStale, commercepkg.CodeScriptRebuildBlocked,
		commercepkg.CodeScriptOrganization, commercepkg.CodeScriptOrganizationBusy,
		commercepkg.CodeScriptOrganizationNeed, commercepkg.CodeLanguageConfirmation,
		commercepkg.CodeStoryboardPlanStale, commercepkg.CodeStoryboardRevision,
		commercepkg.CodeStoryboardPreviewStale, commercepkg.CodeImagePromptRequired:
		return http.StatusConflict
	case commercepkg.CodeStoryboardPlanRequired, commercepkg.CodeStoryboardShotRequired:
		return http.StatusNotFound
	case commercepkg.CodeProjectNotConfigured:
		return http.StatusPreconditionFailed
	default:
		return http.StatusUnprocessableEntity
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
