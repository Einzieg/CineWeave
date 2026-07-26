/**
 * Query key 工厂。所有 key 不含组织前缀,由 useApiQuery / useInvalidateKeys
 * 统一追加 ["org", organizationId] 前缀,避免切换组织后缓存串号。
 */

export type CanonicalAssetListQueryShape = {
  status?: "active" | "archived" | "all";
  assetType?: string;
  includePreviewUrl?: boolean;
  previewExpiresSeconds?: number;
};

function canonicalAssetListShape(shape: CanonicalAssetListQueryShape = {}) {
  return [
    shape.status ?? "active",
    shape.assetType?.trim() || "all",
    shape.includePreviewUrl === true ? "preview" : "metadata",
    shape.includePreviewUrl === true ? shape.previewExpiresSeconds ?? 900 : 0,
  ] as const;
}

export const qk = {
  // 全局
  setupState: () => ["setup-state"] as const,
  organizations: () => ["organizations"] as const,
  systemOrganizationsRoot: () => ["system-organizations"] as const,
  systemOrganizations: (search = "", page = 1) => ["system-organizations", search, page] as const,
  systemOrganizationMembers: (organizationId: string, search = "", status = "", page = 1) =>
    ["system-organization-members", organizationId, search, status, page] as const,
  organizationMembers: (search = "", status = "", page = 1) => ["organization-members", search, status, page] as const,
  organizationMember: (userId: string) => ["organization-member", userId] as const,
  organizationInvitations: () => ["organization-invitations"] as const,
  organizationAuditLogs: (action = "", resourceType = "", page = 1) => ["organization-audit-logs", action, resourceType, page] as const,
  workspaces: () => ["workspaces"] as const,
  teams: () => ["teams"] as const,
  teamMembers: (teamId: string) => ["team-members", teamId] as const,
  teamImpact: (teamId: string) => ["team-impact", teamId] as const,
  roles: () => ["roles"] as const,
  role: (roleId: string) => ["role", roleId] as const,
  roleImpact: (roleId: string) => ["role-impact", roleId] as const,
  roleBindings: (filters?: Record<string, string>) => ["role-bindings", filters ?? {}] as const,
  permissions: () => ["permissions"] as const,
  projects: () => ["projects"] as const,
  commerceProjectOptions: (workspaceId: string) => ["workspace", workspaceId, "commerce-project-options"] as const,
  videoProductionProfiles: () => ["video-production-profiles"] as const,
  providerAccounts: () => ["provider-accounts"] as const,
  providerCredentials: (accountId: string) => ["provider-credentials", accountId] as const,
  providerCatalog: () => ["provider-catalog"] as const,
  providerModels: (accountId: string) => ["provider-models", accountId] as const,
  providerModelsAll: (accountIds: string) => ["provider-models", "all", accountIds] as const,
  providerModelVideoCapabilities: (modelId: string) => ["provider-model", modelId, "video-capabilities"] as const,
  modelProfiles: () => ["model-profiles"] as const,
  promptTemplates: () => ["prompt-templates"] as const,
  artifacts: (projectId?: string) => ["artifacts", projectId ?? "all"] as const,
  workflowRuns: (projectId?: string) => ["workflow-runs", projectId ?? "all"] as const,
  workflowNodes: (workflowRunId: string) => ["workflow-nodes", workflowRunId] as const,
  workflowVideoProduction: (workflowRunId: string) => ["workflow-video-production", workflowRunId] as const,
  workflowDerivedAssetBatch: (workflowRunId: string) => ["workflow-derived-asset-batch", workflowRunId] as const,

  // 项目域
  project: (projectId: string) => ["project", projectId] as const,
  projectDeletionImpact: (projectId: string) => ["project", projectId, "deletion-impact"] as const,
  projectDeletionRequest: (projectId: string, requestId: string) =>
    ["project-deletion-request", projectId, requestId] as const,
  commerceSetupSession: (projectId: string, setupSessionId: string) => ["project", projectId, "commerce-setup", setupSessionId] as const,
  commerceSetupRun: (projectId: string, setupRunId: string) => ["project", projectId, "commerce-setup-run", setupRunId] as const,
  commerceProduct: (projectId: string) => ["project", projectId, "commerce-product"] as const,
  commerceProductVersions: (projectId: string) => ["project", projectId, "commerce-product-versions"] as const,
  commerceProductReferences: (projectId: string, status = "active") => ["project", projectId, "commerce-product-references", status] as const,
  commerceProductReferencesRoot: (projectId: string) => ["project", projectId, "commerce-product-references"] as const,
  commerceProductReferencePacks: (projectId: string, status = "active") => ["project", projectId, "commerce-product-reference-packs", status] as const,
  commerceProductReferencePacksRoot: (projectId: string) => ["project", projectId, "commerce-product-reference-packs"] as const,
  commerceProductReferencePack: (projectId: string, packId: string) => ["project", projectId, "commerce-product-reference-pack", packId] as const,
  commerceScriptUnits: (projectId: string, status = "active", cursor = "") => ["project", projectId, "commerce-script-units", status, cursor] as const,
  commerceScriptUnitsRoot: (projectId: string) => ["project", projectId, "commerce-script-units"] as const,
  commerceScriptUnit: (projectId: string, scriptUnitId: string) => ["project", projectId, "commerce-script-unit", scriptUnitId] as const,
  commerceScriptVersions: (projectId: string, scriptUnitId: string) => ["project", projectId, "commerce-script-versions", scriptUnitId] as const,
  commerceDirectVideoOptions: (projectId: string) => ["project", projectId, "commerce-direct-video-options"] as const,
  commerceScriptReferences: (projectId: string, scriptUnitId: string, status = "active") =>
    ["project", projectId, "commerce-script-references", scriptUnitId, status] as const,
  commerceScriptReferencesRoot: (projectId: string, scriptUnitId = "all") =>
    ["project", projectId, "commerce-script-references", scriptUnitId] as const,
  commerceDirectVideos: (projectId: string, scriptUnitId = "all") =>
    ["project", projectId, "commerce-direct-videos", scriptUnitId] as const,
  commerceDirectVideosRoot: (projectId: string) => ["project", projectId, "commerce-direct-videos"] as const,
  commerceDirectVideo: (projectId: string, jobId: string) =>
    ["project", projectId, "commerce-direct-video", jobId] as const,
  commerceScriptUnitRebuild: (projectId: string, scriptUnitId: string) => ["project", projectId, "commerce-script-unit-rebuild", scriptUnitId] as const,
  commerceLanguageResolution: (projectId: string, scriptUnitId: string) => ["project", projectId, "commerce-language-resolution", scriptUnitId] as const,
  commerceLocalizations: (projectId: string, scriptUnitId: string) => ["project", projectId, "commerce-localizations", scriptUnitId] as const,
  commerceStoryboardPlans: (projectId: string, scriptUnitId: string, status = "active") =>
    ["project", projectId, "commerce-storyboard-plans", scriptUnitId, status] as const,
  commerceStoryboardPlansRoot: (projectId: string, scriptUnitId: string) =>
    ["project", projectId, "commerce-storyboard-plans", scriptUnitId] as const,
  commerceStoryboardPlanningPreview: (projectId: string, scriptUnitId: string, unitGenerationId: string) =>
    ["project", projectId, "commerce-storyboard-planning-preview", scriptUnitId, unitGenerationId] as const,
  commerceStoryboardPlan: (projectId: string, scriptUnitId: string, planId: string) =>
    ["project", projectId, "commerce-storyboard-plan", scriptUnitId, planId] as const,
  commerceProductionRuns: (projectId: string, scriptUnitId = "all", runType = "all") =>
    ["project", projectId, "commerce-production-runs", scriptUnitId, runType] as const,
  commerceProductionRunsRoot: (projectId: string) => ["project", projectId, "commerce-production-runs"] as const,
  commerceProductionRun: (projectId: string, runId: string) =>
    ["project", projectId, "commerce-production-run", runId] as const,
  commerceScriptUnitBatches: (projectId: string) => ["project", projectId, "commerce-script-unit-batches"] as const,
  commerceScriptUnitBatch: (projectId: string, coordinatorId: string) =>
    ["project", projectId, "commerce-script-unit-batch", coordinatorId] as const,
  commerceProjectProductionStatus: (projectId: string) => ["project", projectId, "commerce-production-status"] as const,
  commerceUnitProductionStatus: (projectId: string, scriptUnitId: string) =>
    ["project", projectId, "commerce-unit-production-status", scriptUnitId] as const,
  commerceTimelines: (projectId: string, scriptUnitId: string) =>
    ["project", projectId, "commerce-timelines", scriptUnitId] as const,
  commerceTimeline: (projectId: string, scriptUnitId: string, timelineId: string) =>
    ["project", projectId, "commerce-timeline", scriptUnitId, timelineId] as const,
  commerceFinalVideos: (projectId: string, scriptUnitId: string) =>
    ["project", projectId, "commerce-final-videos", scriptUnitId] as const,
  commerceFinalVideo: (projectId: string, scriptUnitId: string, finalVideoVersionId: string) =>
    ["project", projectId, "commerce-final-video", scriptUnitId, finalVideoVersionId] as const,
  projectVideoProductionProfile: (projectId: string) => ["project", projectId, "video-production-profile"] as const,
  currentProjectVideoProductionRebuild: (projectId: string) => ["project", projectId, "video-production-rebuild", "current"] as const,
  projectVideoProductionRebuild: (projectId: string, rebuildId: string) => ["project", projectId, "video-production-rebuild", rebuildId] as const,
  projectVideoProductionRebuildItems: (projectId: string, rebuildId: string) => ["project", projectId, "video-production-rebuild", rebuildId, "items"] as const,
  projectManualTemplates: (kind?: string) => ["project-manual-templates", kind ?? "all"] as const,
  projectManualBindings: (projectId: string) => ["project", projectId, "manual-bindings"] as const,
  productionStatus: (projectId: string) => ["project", projectId, "production-status"] as const,
  sources: (projectId: string) => ["project", projectId, "sources"] as const,
  sourceImpact: (projectId: string, sourceId: string) => ["project", projectId, "source-impact", sourceId] as const,
  sourceChapters: (projectId: string, sourceId: string) => ["project", projectId, "source-chapters", sourceId] as const,
  sourceChapter: (projectId: string, sourceId: string, chapterId: string) => ["project", projectId, "source-chapter", sourceId, chapterId] as const,
  sourceEvents: (projectId: string, sourceId: string, chapterId?: string) => ["project", projectId, "source-events", sourceId, chapterId ?? "all"] as const,
  adaptationPlans: (projectId: string, sourceId?: string) => ["project", projectId, "adaptation-plans", sourceId ?? "all"] as const,
  adaptationPlan: (projectId: string, planId: string) => ["project", projectId, "adaptation-plan", planId] as const,
  scripts: (projectId: string) => ["project", projectId, "scripts"] as const,
  scriptDetailsPrefix: (projectId: string) => ["project", projectId, "script"] as const,
  scriptVersionsPrefix: (projectId: string) => ["project", projectId, "script-versions"] as const,
  scriptEpisodesPrefix: (projectId: string) => ["project", projectId, "script-episodes"] as const,
  scriptEpisodesForScriptPrefix: (projectId: string, scriptId: string) => ["project", projectId, "script-episodes", scriptId] as const,
  scriptScenesPrefix: (projectId: string) => ["project", projectId, "script-scenes"] as const,
  script: (projectId: string, scriptId: string) => ["project", projectId, "script", scriptId] as const,
  scriptVersions: (projectId: string, scriptId: string) => ["project", projectId, "script-versions", scriptId] as const,
  scriptEpisodes: (projectId: string, scriptId: string, versionId?: string) => ["project", projectId, "script-episodes", scriptId, versionId ?? "all"] as const,
  scriptScenes: (projectId: string, scriptId: string, versionId?: string) => ["project", projectId, "script-scenes", scriptId, versionId ?? "all"] as const,
  agentSessions: (projectId: string) => ["project", projectId, "agent-sessions"] as const,
  agentMessages: (projectId: string, sessionId: string) => ["project", projectId, "agent-messages", sessionId] as const,
  agentTools: (projectId: string) => ["project", projectId, "agent-tools"] as const,
  agentTasks: (projectId: string, sessionId?: string | null) => ["project", projectId, "agent-tasks", sessionId || "all"] as const,
  agentTask: (projectId: string, taskId: string) => ["project", projectId, "agent-task", taskId] as const,
  assetsRoot: (projectId: string) => ["project", projectId, "assets"] as const,
  assets: (projectId: string, shape: CanonicalAssetListQueryShape = {}) => ["project", projectId, "assets", "list", ...canonicalAssetListShape(shape)] as const,
  assetPreviews: (projectId: string, shape: CanonicalAssetListQueryShape = {}) =>
    ["project", projectId, "assets", "previews", ...canonicalAssetListShape({ ...shape, includePreviewUrl: true })] as const,
  asset: (projectId: string, assetId: string, includePreviewUrl = false, previewExpiresSeconds = 900) =>
    ["project", projectId, "assets", "detail", assetId, includePreviewUrl ? "preview" : "metadata", includePreviewUrl ? previewExpiresSeconds : 0] as const,
  assetImpact: (projectId: string, assetId: string) => ["project", projectId, "asset-impact", assetId] as const,
  assetReferencesRoot: (projectId: string, assetId: string) => ["project", projectId, "asset-references", assetId] as const,
  assetReferences: (projectId: string, assetId: string, includePreviewUrl = false, previewExpiresSeconds = 900) =>
    ["project", projectId, "asset-references", assetId, includePreviewUrl ? "preview" : "metadata", includePreviewUrl ? previewExpiresSeconds : 0] as const,
  requirements: (projectId: string) => ["project", projectId, "shot-asset-requirements"] as const,
  shotProductionPrefix: (projectId: string) => ["project", projectId, "shot-production"] as const,
  shotProduction: (projectId: string, scriptEpisodeId?: string, storyboardPlanId?: string) => ["project", projectId, "shot-production", scriptEpisodeId ?? "all", storyboardPlanId ?? "active"] as const,
  scriptEpisodeTiming: (projectId: string, episodeId: string) => ["project", projectId, "script-episode-timing", episodeId] as const,
  episodeAudio: (projectId: string, episodeId: string) => ["project", projectId, "episode-audio", episodeId] as const,
  characterVoices: (projectId: string) => ["project", projectId, "character-voices"] as const,
  storyboardPlans: (projectId: string, episodeId: string) => ["project", projectId, "storyboard-plans", episodeId] as const,
  storyboardPlan: (projectId: string, planId: string) => ["project", projectId, "storyboard-plan", planId] as const,
  shotDetail: (projectId: string, shotId: string) => ["project", projectId, "shot-detail", shotId] as const,
  shotState: (projectId: string, shotId: string) => ["project", projectId, "shot-state", shotId] as const,
  shotTransition: (projectId: string, shotId: string) => ["project", projectId, "shot-transition", shotId] as const,
  shotAnchors: (projectId: string, shotId: string) => ["project", projectId, "shot-anchors", shotId] as const,
  shotReferencePack: (projectId: string, shotId: string, purpose: "anchor" | "video" = "anchor") => ["project", projectId, "shot-reference-pack", shotId, purpose] as const,
  shotStoryboardSheet: (projectId: string, shotId: string) => ["project", projectId, "shot-storyboard-sheet", shotId] as const,
  shotVideoPromptPlan: (projectId: string, shotId: string) => ["project", projectId, "shot-video-prompt-plan", shotId] as const,
  shotRenderPlan: (projectId: string, shotId: string) => ["project", projectId, "shot-render-plan", shotId] as const,
  nativeAudioReviews: (projectId: string, shotId: string) => ["project", projectId, "native-audio-reviews", shotId] as const,
  timelines: (projectId: string) => ["project", projectId, "timelines"] as const,
  timelineDetail: (projectId: string, timelineId: string) => ["project", projectId, "timeline-detail", timelineId] as const,
  finalVideos: (projectId: string) => ["project", projectId, "final-videos"] as const,
  exports: (projectId: string) => ["project", projectId, "exports"] as const,
  reviewRuns: (projectId: string) => ["project", projectId, "review-runs"] as const,
  reviewItems: (projectId: string, filter?: Record<string, string>) => ["project", projectId, "review-items", filter ?? {}] as const,
  reviewItemsPrefix: (projectId: string) => ["project", projectId, "review-items"] as const,
  reviewFixes: (projectId: string, itemId: string) => ["project", projectId, "review-fixes", itemId] as const,
};
