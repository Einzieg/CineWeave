import type {
  AgentMessage,
  AgentSession,
  AdaptationPlan,
  ApiEnvelope,
  Artifact,
  AuthResponse,
  CanonicalAsset,
  AssetReference,
  ComposeTimelineResponse,
  CreateProjectExportResponse,
  DownloadUrlResponse,
  FinalVideoVersion,
  GenerateAssetCardResponse,
  JsonRecord,
  ImportProjectSourceResponse,
  ListEnvelope,
  ModelProfile,
  NovelEvent,
  NovelEventLink,
  Organization,
  ParseScriptScenesResponse,
  Permission,
  Project,
  ProjectSource,
  ProductionActionResponse,
  ProjectExport,
  ProductionStatus,
  ProjectTimeline,
  ReviewItem,
  ReviewRun,
  RunProjectReviewResponse,
  RegenerateResponse,
  PromptTemplate,
  ProviderAccount,
  ReviewResponse,
  Role,
  Script,
  ScriptScene,
  ScriptVersion,
  ShotProductionActionResponse,
  ShotProductionStatus,
  SetupState,
  ShotAssetRequirement,
  StoryboardShot,
  StoryboardShotDetail,
  StudioSession,
  Team,
  TimelineClip,
  TimelineDetail,
  WorkflowNodeRun,
  WorkflowRun,
  Workspace,
} from "./types";

const apiBase = trimTrailingSlash(process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080");

type ApiRequestOptions = {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  session?: StudioSession;
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
};

export class StudioApiError extends Error {
  code: string;
  status: number;
  retryable: boolean;

  constructor(message: string, code: string, status: number, retryable = false) {
    super(message);
    this.name = "StudioApiError";
    this.code = code;
    this.status = status;
    this.retryable = retryable;
  }
}

export async function apiRequest<TData>(path: string, options: ApiRequestOptions = {}): Promise<TData> {
  const url = new URL(`${apiBase}${path}`);
  for (const [key, value] of Object.entries(options.query ?? {})) {
    if (value !== undefined && value !== null && String(value).trim() !== "") {
      url.searchParams.set(key, String(value));
    }
  }
  const headers = new Headers({ Accept: "application/json" });
  const isFormData = typeof FormData !== "undefined" && options.body instanceof FormData;
  if (options.body !== undefined && !isFormData) {
    headers.set("Content-Type", "application/json");
  }
  const token = options.session?.accessToken.trim();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  const organizationId = options.session?.organizationId.trim();
  if (organizationId) {
    headers.set("X-Organization-Id", organizationId);
  }
  const requestBody = options.body === undefined ? undefined : isFormData ? (options.body as BodyInit) : JSON.stringify(options.body);
  const response = await fetch(url, {
    method: options.method ?? (options.body === undefined ? "GET" : "POST"),
    headers,
    body: requestBody,
  });
  const envelope = (await response.json().catch(() => ({}))) as ApiEnvelope<TData>;
  if (!response.ok || envelope.error || envelope.data === undefined) {
    throw new StudioApiError(
      envelope.error?.message ?? `请求失败：HTTP ${response.status}`,
      envelope.error?.code ?? "HTTP_ERROR",
      response.status,
      envelope.error?.retryable ?? false,
    );
  }
  return envelope.data;
}

export const studioApi = {
  getSetupState: () => apiRequest<SetupState>("/api/system/setup-state"),
  setupSystem: (body: JsonRecord) => apiRequest<AuthResponse>("/api/system/setup", { method: "POST", body }),
  login: (body: JsonRecord) => apiRequest<AuthResponse>("/api/auth/login", { method: "POST", body }),
  refreshAuth: (refreshToken: string) => apiRequest<AuthResponse>("/api/auth/refresh", { method: "POST", body: { refreshToken } }),
  logout: (refreshToken: string) => apiRequest<{ ok: boolean }>("/api/auth/logout", { method: "POST", body: { refreshToken } }),
  me: (session: StudioSession) => apiRequest<{ user: AuthResponse["user"]; organizationId: string; workspaceId?: string }>("/api/auth/me", { session }),

  listOrganizations: (session: StudioSession) => apiRequest<ListEnvelope<Organization>>("/api/organizations", { session }),
  listWorkspaces: (session: StudioSession) => apiRequest<ListEnvelope<Workspace>>("/api/workspaces", { session }),
  listTeams: (session: StudioSession) => apiRequest<ListEnvelope<Team>>("/api/teams", { session }),
  createTeam: (session: StudioSession, body: JsonRecord) => apiRequest<Team>("/api/teams", { method: "POST", session, body }),
  listRoles: (session: StudioSession) => apiRequest<ListEnvelope<Role>>("/api/roles", { session }),
  listPermissions: (session: StudioSession) => apiRequest<ListEnvelope<Permission>>("/api/permissions", { session }),

  listProjects: (session: StudioSession) => apiRequest<ListEnvelope<Project>>("/api/projects", { session }),
  getProject: (session: StudioSession, projectId: string) => apiRequest<Project>(`/api/projects/${projectId}`, { session }),
  createProject: (session: StudioSession, body: JsonRecord) => apiRequest<Project>("/api/projects", { method: "POST", session, body }),
  updateProject: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<Project>(`/api/projects/${projectId}`, { method: "PATCH", session, body }),
  getProductionStatus: (session: StudioSession, projectId: string) =>
    apiRequest<ProductionStatus>(`/api/projects/${projectId}/production/status`, { session }),
  runProductionAction: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ProductionActionResponse>(`/api/projects/${projectId}/production/actions`, { method: "POST", session, body }),
  listProjectExports: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ProjectExport>>(`/api/projects/${projectId}/exports`, { session }),
  createProjectExport: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<CreateProjectExportResponse>(`/api/projects/${projectId}/exports`, { method: "POST", session, body }),
  getProjectExport: (session: StudioSession, projectId: string, exportId: string) =>
    apiRequest<ProjectExport>(`/api/projects/${projectId}/exports/${exportId}`, { session }),
  createProjectExportDownloadUrl: (session: StudioSession, projectId: string, exportId: string, body: JsonRecord) =>
    apiRequest<DownloadUrlResponse>(`/api/projects/${projectId}/exports/${exportId}/download-url`, { method: "POST", session, body }),
  runProjectReview: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<RunProjectReviewResponse>(`/api/projects/${projectId}/reviews/run`, { method: "POST", session, body }),
  listReviewRuns: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ReviewRun>>(`/api/projects/${projectId}/reviews`, { session }),
  getReviewRun: (session: StudioSession, projectId: string, reviewRunId: string) =>
    apiRequest<ReviewRun>(`/api/projects/${projectId}/reviews/${reviewRunId}`, { session }),
  listReviewItems: (session: StudioSession, projectId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ListEnvelope<ReviewItem>>(`/api/projects/${projectId}/review-items`, { session, query }),
  getReviewItem: (session: StudioSession, projectId: string, itemId: string) =>
    apiRequest<ReviewItem>(`/api/projects/${projectId}/review-items/${itemId}`, { session }),
  resolveReviewItem: (session: StudioSession, projectId: string, itemId: string, body: JsonRecord) =>
    apiRequest<ReviewItem>(`/api/projects/${projectId}/review-items/${itemId}/resolve`, { method: "POST", session, body }),
  ignoreReviewItem: (session: StudioSession, projectId: string, itemId: string, body: JsonRecord) =>
    apiRequest<ReviewItem>(`/api/projects/${projectId}/review-items/${itemId}/ignore`, { method: "POST", session, body }),
  reopenReviewItem: (session: StudioSession, projectId: string, itemId: string, body: JsonRecord) =>
    apiRequest<ReviewItem>(`/api/projects/${projectId}/review-items/${itemId}/reopen`, { method: "POST", session, body }),
  getShotProductionStatus: (session: StudioSession, projectId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ShotProductionStatus>(`/api/projects/${projectId}/shot-production/status`, { session, query }),
  runShotProductionAction: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ShotProductionActionResponse>(`/api/projects/${projectId}/shot-production/actions`, { method: "POST", session, body }),
  regenerate: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<RegenerateResponse>(`/api/projects/${projectId}/regenerate`, { method: "POST", session, body }),
  listTimelines: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ProjectTimeline>>(`/api/projects/${projectId}/timelines`, { session }),
  createTimeline: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ProjectTimeline>(`/api/projects/${projectId}/timelines`, { method: "POST", session, body }),
  getTimelineDetail: (session: StudioSession, projectId: string, timelineId: string) =>
    apiRequest<TimelineDetail>(`/api/projects/${projectId}/timelines/${timelineId}/detail`, { session, query: { previewExpiresSeconds: 900 } }),
  updateTimeline: (session: StudioSession, projectId: string, timelineId: string, body: JsonRecord) =>
    apiRequest<ProjectTimeline>(`/api/projects/${projectId}/timelines/${timelineId}`, { method: "PATCH", session, body }),
  deleteTimeline: (session: StudioSession, projectId: string, timelineId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/projects/${projectId}/timelines/${timelineId}`, { method: "DELETE", session }),
  createTimelineClip: (session: StudioSession, projectId: string, timelineId: string, body: JsonRecord) =>
    apiRequest<TimelineClip>(`/api/projects/${projectId}/timelines/${timelineId}/clips`, { method: "POST", session, body }),
  updateTimelineClip: (session: StudioSession, projectId: string, timelineId: string, clipId: string, body: JsonRecord) =>
    apiRequest<TimelineClip>(`/api/projects/${projectId}/timelines/${timelineId}/clips/${clipId}`, { method: "PATCH", session, body }),
  deleteTimelineClip: (session: StudioSession, projectId: string, timelineId: string, clipId: string) =>
    apiRequest<{ deleted: boolean; clipId: string }>(`/api/projects/${projectId}/timelines/${timelineId}/clips/${clipId}`, { method: "DELETE", session }),
  reorderTimelineClips: (session: StudioSession, projectId: string, timelineId: string, body: JsonRecord) =>
    apiRequest<{ items: { clipId: string; clipIndex: number }[] }>(`/api/projects/${projectId}/timelines/${timelineId}/clips/reorder`, { method: "POST", session, body }),
  composeTimeline: (session: StudioSession, projectId: string, timelineId: string, body: JsonRecord) =>
    apiRequest<ComposeTimelineResponse>(`/api/projects/${projectId}/timelines/${timelineId}/compose`, { method: "POST", session, body }),
  listFinalVideos: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<FinalVideoVersion>>(`/api/projects/${projectId}/final-videos`, { session }),
  getFinalVideo: (session: StudioSession, projectId: string, versionId: string) =>
    apiRequest<FinalVideoVersion>(`/api/projects/${projectId}/final-videos/${versionId}`, { session }),
  activateFinalVideo: (session: StudioSession, projectId: string, versionId: string) =>
    apiRequest<FinalVideoVersion>(`/api/projects/${projectId}/final-videos/${versionId}/activate`, { method: "POST", session, body: {} }),
  createFinalVideoDownloadUrl: (session: StudioSession, projectId: string, versionId: string, body: JsonRecord) =>
    apiRequest<DownloadUrlResponse>(`/api/projects/${projectId}/final-videos/${versionId}/download-url`, { method: "POST", session, body }),
  deleteFinalVideo: (session: StudioSession, projectId: string, versionId: string) =>
    apiRequest<{ deleted: boolean; versionId: string }>(`/api/projects/${projectId}/final-videos/${versionId}`, { method: "DELETE", session }),

  listSources: (session: StudioSession, projectId: string) => apiRequest<ListEnvelope<ProjectSource>>(`/api/projects/${projectId}/sources`, { session }),
  getSource: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources/${sourceId}`, { session }),
  createSource: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ImportProjectSourceResponse>(`/api/projects/${projectId}/sources`, { method: "POST", session, body }),
  importSourceFile: (session: StudioSession, projectId: string, body: FormData) =>
    apiRequest<ImportProjectSourceResponse>(`/api/projects/${projectId}/sources/import`, { method: "POST", session, body }),
  updateSource: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "PATCH", session, body }),
  deleteSource: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "DELETE", session }),
  extractNovelEvents: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/sources/${sourceId}/extract-events`, { method: "POST", session, body }),
  listSourceNovelEvents: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<{ items: NovelEvent[]; links: NovelEventLink[] }>(`/api/projects/${projectId}/sources/${sourceId}/events`, { session }),
  updateNovelEvent: (session: StudioSession, projectId: string, eventId: string, body: JsonRecord) =>
    apiRequest<NovelEvent>(`/api/projects/${projectId}/novel-events/${eventId}`, { method: "PATCH", session, body }),
  reviewNovelEvent: (session: StudioSession, projectId: string, eventId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/novel-events/${eventId}/review`, { method: "POST", session, body }),
  listAdaptationPlans: (session: StudioSession, projectId: string, sourceId?: string) =>
    apiRequest<ListEnvelope<AdaptationPlan>>(`/api/projects/${projectId}/adaptation-plans`, { session, query: sourceId ? { sourceId } : undefined }),
  generateAdaptationPlan: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<AdaptationPlan>(`/api/projects/${projectId}/sources/${sourceId}/generate-adaptation-plan`, { method: "POST", session, body }),
  updateAdaptationPlan: (session: StudioSession, projectId: string, planId: string, body: JsonRecord) =>
    apiRequest<AdaptationPlan>(`/api/projects/${projectId}/adaptation-plans/${planId}`, { method: "PATCH", session, body }),
  reviewAdaptationPlan: (session: StudioSession, projectId: string, planId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/adaptation-plans/${planId}/review`, { method: "POST", session, body }),
  activateAdaptationPlan: (session: StudioSession, projectId: string, planId: string) =>
    apiRequest<AdaptationPlan>(`/api/projects/${projectId}/adaptation-plans/${planId}/activate`, { method: "POST", session, body: {} }),
  generateScriptFromAdaptationPlan: (session: StudioSession, projectId: string, planId: string, body: JsonRecord) =>
    apiRequest<{ scriptId: string; versionId: string; adaptationPlanId: string; content: string; providerCallId?: string; modelId?: string }>(
      `/api/projects/${projectId}/adaptation-plans/${planId}/generate-script`,
      { method: "POST", session, body },
    ),

  listScripts: (session: StudioSession, projectId: string) => apiRequest<ListEnvelope<Script>>(`/api/projects/${projectId}/scripts`, { session }),
  getScript: (session: StudioSession, projectId: string, scriptId: string) => apiRequest<Script>(`/api/projects/${projectId}/scripts/${scriptId}`, { session }),
  createScript: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<Script>(`/api/projects/${projectId}/scripts`, { method: "POST", session, body }),
  listScriptVersions: (session: StudioSession, projectId: string, scriptId: string) =>
    apiRequest<ListEnvelope<ScriptVersion>>(`/api/projects/${projectId}/scripts/${scriptId}/versions`, { session }),
  createScriptVersion: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<ScriptVersion>(`/api/projects/${projectId}/scripts/${scriptId}/versions`, { method: "POST", session, body }),
  activateScriptVersion: (session: StudioSession, projectId: string, scriptId: string, versionId: string) =>
    apiRequest<Script>(`/api/projects/${projectId}/scripts/${scriptId}/activate-version`, { method: "POST", session, body: { versionId } }),
  parseScriptScenes: (session: StudioSession, projectId: string, scriptId: string, versionId: string, body: JsonRecord) =>
    apiRequest<ParseScriptScenesResponse>(`/api/projects/${projectId}/scripts/${scriptId}/versions/${versionId}/parse-scenes`, { method: "POST", session, body }),
  listScriptScenes: (session: StudioSession, projectId: string, scriptId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ListEnvelope<ScriptScene>>(`/api/projects/${projectId}/scripts/${scriptId}/scenes`, { session, query }),
  updateScriptScene: (session: StudioSession, projectId: string, sceneId: string, body: JsonRecord) =>
    apiRequest<ScriptScene>(`/api/projects/${projectId}/script-scenes/${sceneId}`, { method: "PATCH", session, body }),
  reviewScriptScene: (session: StudioSession, projectId: string, sceneId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/script-scenes/${sceneId}/review`, { method: "POST", session, body }),

  listAgentSessions: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<AgentSession>>(`/api/projects/${projectId}/script-agent/sessions`, { session }),
  createAgentSession: (session: StudioSession, projectId: string, title: string) =>
    apiRequest<AgentSession>(`/api/projects/${projectId}/script-agent/sessions`, { method: "POST", session, body: { title } }),
  listAgentMessages: (session: StudioSession, projectId: string, sessionId: string) =>
    apiRequest<ListEnvelope<AgentMessage>>(`/api/projects/${projectId}/script-agent/sessions/${sessionId}/messages`, { session }),
  createAgentMessage: (session: StudioSession, projectId: string, sessionId: string, content: string) =>
    apiRequest<AgentMessage>(`/api/projects/${projectId}/script-agent/sessions/${sessionId}/messages`, {
      method: "POST",
      session,
      body: { role: "user", content },
    }),
  generateScript: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<{ scriptId: string; versionId: string; content: string; agentRunId: string }>(`/api/projects/${projectId}/script-agent/generate-script`, {
      method: "POST",
      session,
      body,
    }),
  rewriteScript: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<{ scriptId: string; versionId: string; content: string; agentRunId: string }>(`/api/projects/${projectId}/script-agent/rewrite-script`, {
      method: "POST",
      session,
      body,
    }),

  listCanonicalAssets: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<CanonicalAsset>>(`/api/projects/${projectId}/canonical-assets`, { session }),
  getCanonicalAsset: (session: StudioSession, projectId: string, assetId: string, includePreviewUrl = false) =>
    apiRequest<CanonicalAsset>(`/api/projects/${projectId}/canonical-assets/${assetId}`, { session, query: includePreviewUrl ? { includePreviewUrl: "true" } : undefined }),
  updateCanonicalAsset: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<CanonicalAsset>(`/api/projects/${projectId}/canonical-assets/${assetId}`, { method: "PATCH", session, body }),
  generateAssetCard: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<GenerateAssetCardResponse>(`/api/projects/${projectId}/canonical-assets/${assetId}/generate-card`, { method: "POST", session, body }),
  listAssetReferences: (session: StudioSession, projectId: string, assetId: string, includePreviewUrl = false) =>
    apiRequest<ListEnvelope<AssetReference>>(`/api/projects/${projectId}/canonical-assets/${assetId}/references`, { session, query: includePreviewUrl ? { includePreviewUrl: "true" } : undefined }),
  createAssetReferenceUploadUrl: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<{ storageKey: string; uploadUrl: string; method: string; headers: Record<string, string | string[]>; expiresAt: string }>(`/api/projects/${projectId}/canonical-assets/${assetId}/references/upload-url`, { method: "POST", session, body }),
  createAssetReference: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<AssetReference>(`/api/projects/${projectId}/canonical-assets/${assetId}/references`, { method: "POST", session, body }),
  setPrimaryAssetReference: (session: StudioSession, projectId: string, assetId: string, referenceId: string) =>
    apiRequest<{ assetId: string; reference: AssetReference }>(`/api/projects/${projectId}/canonical-assets/${assetId}/references/${referenceId}/set-primary`, { method: "POST", session, body: {} }),
  analyzeScriptAssets: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/scripts/${scriptId}/analyze-assets`, { method: "POST", session, body }),
  generateAssetImage: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord = {}) =>
    apiRequest<{ asset: CanonicalAsset; providerCallId: string }>(`/api/projects/${projectId}/canonical-assets/${assetId}/generate-image`, { method: "POST", session, body }),
  reviewAsset: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/assets/${assetId}/review`, { method: "POST", session, body }),
  listShotAssetRequirements: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ShotAssetRequirement>>(`/api/projects/${projectId}/shot-asset-requirements`, { session }),
  generateDerivedAssetImage: (session: StudioSession, projectId: string, requirementId: string) =>
    apiRequest<{ requirement: ShotAssetRequirement; providerCallId: string }>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/generate-image`, { method: "POST", session, body: {} }),
  reviewShotAssetRequirement: (session: StudioSession, projectId: string, requirementId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/review`, { method: "POST", session, body }),
  updateShotAssetRequirement: (session: StudioSession, projectId: string, requirementId: string, body: JsonRecord) =>
    apiRequest<ShotAssetRequirement>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}`, { method: "PATCH", session, body }),

  generateStoryboard: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/scripts/${scriptId}/generate-storyboard`, { method: "POST", session, body }),
  createWorkflowRun: (session: StudioSession, body: JsonRecord) => apiRequest<WorkflowRun>("/api/workflow-runs", { method: "POST", session, body }),
  listWorkflowRuns: (session: StudioSession, projectId?: string) =>
    apiRequest<ListEnvelope<WorkflowRun>>("/api/workflow-runs", { session, query: projectId ? { "filter[projectId]": projectId } : undefined }),
  cancelWorkflowRun: (session: StudioSession, workflowRunId: string, reason: string) =>
    apiRequest<WorkflowRun>(`/api/workflow-runs/${workflowRunId}/cancel`, { method: "POST", session, body: { reason } }),
  listWorkflowNodes: (session: StudioSession, workflowRunId: string) =>
    apiRequest<ListEnvelope<WorkflowNodeRun>>(`/api/workflow-runs/${workflowRunId}/nodes`, { session }),
  listWorkflowShots: (session: StudioSession, workflowRunId: string) =>
    apiRequest<ListEnvelope<StoryboardShot>>(`/api/workflow-runs/${workflowRunId}/shots`, {
      session,
      query: { includePreviewUrl: true, previewExpiresSeconds: 900 },
    }),
  createStoryboardShot: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<StoryboardShot>(`/api/projects/${projectId}/storyboard-shots`, { method: "POST", session, body }),
  deleteStoryboardShot: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<{ deleted: boolean; shotId: string }>(`/api/projects/${projectId}/storyboard-shots/${shotId}`, { method: "DELETE", session }),
  reorderStoryboardShots: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<{ items: { shotId: string; shotIndex: number; shotNo: number }[] }>(`/api/projects/${projectId}/storyboard-shots/reorder`, { method: "POST", session, body }),
  getStoryboardShotDetail: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<StoryboardShotDetail>(`/api/projects/${projectId}/storyboard-shots/${shotId}/detail`, {
      session,
      query: { previewExpiresSeconds: 900 },
    }),
  reviewStoryboardShot: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/review`, { method: "POST", session, body }),
  updateStoryboardShot: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord) =>
    apiRequest<StoryboardShot>(`/api/projects/${projectId}/storyboard-shots/${shotId}`, { method: "PATCH", session, body }),

  listArtifacts: (session: StudioSession, projectId?: string) =>
    apiRequest<ListEnvelope<Artifact>>("/api/artifacts", {
      session,
      query: { "filter[projectId]": projectId, includePreviewUrl: true, previewExpiresSeconds: 900 },
    }),

  listProviderAccounts: (session: StudioSession) => apiRequest<ListEnvelope<ProviderAccount>>("/api/providers/accounts", { session }),
  createProviderAccount: (session: StudioSession, body: JsonRecord) => apiRequest<ProviderAccount>("/api/providers/accounts", { method: "POST", session, body }),
  listModelProfiles: (session: StudioSession) => apiRequest<ListEnvelope<ModelProfile>>("/api/model-profiles", { session }),
  createModelProfile: (session: StudioSession, body: JsonRecord) => apiRequest<ModelProfile>("/api/model-profiles", { method: "POST", session, body }),
  listPromptTemplates: (session: StudioSession) => apiRequest<ListEnvelope<PromptTemplate>>("/api/prompt-templates", { session }),
  createPromptTemplate: (session: StudioSession, body: JsonRecord) => apiRequest<PromptTemplate>("/api/prompt-templates", { method: "POST", session, body }),
  createPromptVersion: (session: StudioSession, templateId: string, body: JsonRecord) =>
    apiRequest<{ id: string }>(`/api/prompt-templates/${templateId}/versions`, { method: "POST", session, body }),
};

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}
