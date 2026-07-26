import type {
  AgentMessage,
  AgentApproval,
  AgentSession,
  AgentTask,
  AgentToolDescriptor,
  AdaptationPlan,
  ApiEnvelope,
  Artifact,
  AuthResponse,
  LoginResponse,
  OrganizationInvitation,
  OrganizationInvitationList,
  OrganizationAuditLogList,
  OrganizationMember,
  OrganizationMemberList,
  MemberPasswordReset,
  CanonicalAsset,
  CreateAssetBatchRequest,
  CreatedSystemOrganization,
  CreateSystemOrganizationMemberRequest,
  CreateSystemOrganizationRequest,
  CharacterVoiceProfile,
  AssetReference,
  ComposeTimelineResponse,
  CommerceLanguageResolution,
  CommerceLanguageConfirmationAccepted,
  CommerceDirectVideoJob,
  CommerceDirectVideoOptions,
  CommerceScriptReferenceImage,
  CreateCommerceDirectVideoRequest,
  CommerceProduct,
  CommerceProductMutationResult,
  CommerceProductRebuildImpact,
  CommerceProductRebuildResult,
  CommerceProductReference,
  CommerceProductReferencePack,
  CommerceProductReferenceUpload,
  CommerceProductVersion,
  CommerceProjectProductionStatus,
  CommerceProductionRun,
  CommerceProductionRunDetail,
  CommerceProductionRunType,
  CommerceScriptUnitBatch,
  CommerceScriptUnitBatchAdvanceItem,
  CommerceScriptUnitBatchStage,
  CommerceProjectOptions,
  CommerceScriptLocalization,
  CommerceStoryboardPlan,
  CommerceStoryboardPlanDetail,
  CommerceStoryboardPlanningPreview,
  CommerceVideoBatchRequest,
  CommerceScriptUnit,
  CommerceScriptUnitList,
  CommerceScriptVersion,
  CommerceScriptVersionMutation,
  CommerceScriptUnitRebuildImpact,
  CommerceTimeline,
  CommerceTimelineDetail,
  CommerceTimelineOverlay,
  CommerceFinalVideoVersion,
  CommerceUnitProductionStatus,
  CommerceSetupCompletionResult,
  CommerceSetupLanguageConfirmationResult,
  CommerceSetupSession,
  CompleteCommerceSetupSessionRequest,
  ConfirmCommerceSetupLanguageRequest,
  CreateProjectRequest,
  CreateProjectExportResponse,
  DownloadUrlResponse,
  DerivedAssetBatchCommandResult,
  DerivedAssetBatchProjection,
  FinalVideoVersion,
  EpisodeAudio,
  GenerateAssetCardResponse,
  JsonRecord,
  JsonValue,
  ImportProjectSourceResponse,
  ListEnvelope,
  ModelProfile,
  NovelChapter,
  NovelChapterSummary,
  NovelEvent,
  NovelEventLink,
  NativeAudioReview,
  Organization,
  OrganizationChoice,
  OutputImpact,
  ParseScriptScenesResponse,
  Permission,
  Project,
  ProjectDeletionImpact,
  ProjectDeletionRequest,
  CreateProjectDeletionRequest,
  ProjectManualBinding,
  ProjectSource,
  ProductionActionResponse,
  ProjectExport,
  ProductionStatus,
  ProjectTimeline,
  ApplyReviewFixResponse,
  DismissReviewFixResponse,
  ReviewFix,
  ReviewItem,
  ReviewRun,
  RunProjectReviewResponse,
  RegenerateResponse,
  PromptTemplate,
  ProviderAccount,
  ProviderCredential,
  ProviderCallLog,
  ProviderCatalogEntry,
  ProviderCatalogInstallResponse,
  ProviderCircuitState,
  ProviderConnector,
  ProviderLimitPolicy,
  ProviderManifestTestRunResult,
  ProviderManifestValidationResult,
  ProviderModel,
  ProviderModelDiscoveryResult,
  VideoCapabilityAttestation,
  VideoCapabilityAttestationList,
  ProviderTestResult,
  ProviderUsageSummary,
  ReviewResponse,
  RetryFailedWorkflowRequest,
  Role,
  RoleBinding,
  RoleBindingList,
  RoleImpact,
  RuntimeOperation,
  Script,
  ScriptEpisode,
  ScriptScene,
  ScriptVersion,
  ShotProductionActionResponse,
  ShotProductionBatchRequest,
  ShotProductionStatus,
  ShotReferencePackResponse,
  ShotVisualAnchor,
  ShotVisualAnchorResponse,
  SetupState,
  ShotAssetRequirement,
  BatchReviewShotAssetRequirementsResponse,
  StoryboardShot,
  StoryboardShotDetail,
  StoryboardShotStateResponse,
  StoryboardShotTransition,
  StoryboardShotTransitionResponse,
  StoryboardSheetResponse,
  StoryboardPlan,
  StoryboardPlanEditResponse,
  ScriptTimingAnalysis,
  StudioSession,
  SystemOrganizationList,
  Team,
  TeamImpact,
  TeamMember,
  TimelineClip,
  TimelineDetail,
  UpdateProjectRequest,
  UpdateSystemOrganizationMemberRequest,
  UpdateCommerceScriptUnitDefaultsRequest,
  UpdateModelProfileBindingRequest,
  CreateModelProfileBindingRequest,
  UpdateStoryboardShotRequest,
  VideoRenderPlan,
  VideoPromptPlan,
  VideoPromptPlanResponse,
  VideoProductionCompatibility,
  VideoProductionConfigurationInput,
  VideoProductionProfileKey,
  VideoProductionProfileVersion,
  VideoProductionRebuild,
  VideoProductionRebuildImpact,
  VideoProductionRebuildItem,
  WorkflowNodeRun,
  WorkflowRun,
  WorkflowVideoProductionActivity,
  Workspace,
} from "./types";
import { localizePlatformError } from "./error-localization";
import { normalizeIdempotencyKey } from "./idempotency-key";
import { wrapBrowserCachedMediaUrls } from "./media-cache";

const configuredApiBase = trimTrailingSlash(process.env.NEXT_PUBLIC_API_BASE_URL ?? "");

type ApiRequestOptions = {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  session?: StudioSession;
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
  idempotencyKey?: string;
  headers?: Record<string, string>;
};

type ProviderListStatus = "active" | "disabled" | "all";
type ProviderCredentialStatus = "active" | "rotated" | "revoked" | "expired" | "all";
type ArchiveListStatus = "active" | "archived" | "all";
type CanonicalAssetListOptions = {
  status?: ArchiveListStatus;
  assetType?: string;
  includePreviewUrl?: boolean;
  previewExpiresSeconds?: number;
};

export class StudioApiError extends Error {
  code: string;
  status: number;
  retryable: boolean;
  details?: JsonValue;

  constructor(message: string, code: string, status: number, retryable = false, details?: JsonValue) {
    super(message);
    this.name = "StudioApiError";
    this.code = code;
    this.status = status;
    this.retryable = retryable;
    this.details = details;
  }
}

export async function apiRequest<TData>(path: string, options: ApiRequestOptions = {}): Promise<TData> {
  const url = resolveApiUrl(path);
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
  if (options.idempotencyKey?.trim()) {
    try {
      headers.set("Idempotency-Key", await normalizeIdempotencyKey(options.idempotencyKey));
    } catch {
      throw new StudioApiError(
        "浏览器无法生成请求标识，请刷新页面后重试",
        "IDEMPOTENCY_KEY_INVALID",
        0,
        false,
      );
    }
  }
  for (const [name, value] of Object.entries(options.headers ?? {})) {
    if (value.trim()) headers.set(name, value.trim());
  }
  const requestBody = options.body === undefined ? undefined : isFormData ? (options.body as BodyInit) : JSON.stringify(options.body);
  let response: Response;
  try {
    response = await fetch(url, {
      method: options.method ?? (options.body === undefined ? "GET" : "POST"),
      headers,
      body: requestBody,
    });
  } catch {
    throw new StudioApiError(
      "无法连接 CineWeave 服务，请检查网络后重试",
      "NETWORK_ERROR",
      0,
      true,
    );
  }
  const envelope = (await response.json().catch(() => ({}))) as ApiEnvelope<TData>;
  if (!response.ok || envelope.error || envelope.data === undefined) {
    const errorCode = envelope.error?.code ?? "HTTP_ERROR";
    const errorMessage = localizePlatformError(
      envelope.error?.message,
      errorCode,
      `请求失败：HTTP ${response.status}`,
    );
    throw new StudioApiError(
      errorMessage,
      errorCode,
      response.status,
      envelope.error?.retryable ?? false,
      envelope.error?.details,
    );
  }
  return wrapBrowserCachedMediaUrls(envelope.data, options.session);
}

export const studioApi = {
  getSetupState: () => apiRequest<SetupState>("/api/system/setup-state"),
  setupSystem: (body: JsonRecord) => apiRequest<AuthResponse>("/api/system/setup", { method: "POST", body }),
  login: (body: JsonRecord) => apiRequest<LoginResponse>("/api/auth/login", { method: "POST", body }),
  selectOrganization: (organizationSelectionToken: string, organizationId: string) =>
    apiRequest<AuthResponse>("/api/auth/select-organization", {
      method: "POST",
      body: { organizationSelectionToken, organizationId },
    }),
  switchOrganization: (session: StudioSession, organizationId: string) =>
    apiRequest<AuthResponse>("/api/auth/switch-organization", {
      method: "POST",
      session,
      body: { refreshToken: session.refreshToken, organizationId },
    }),
  refreshAuth: (refreshToken: string) => apiRequest<AuthResponse>("/api/auth/refresh", { method: "POST", body: { refreshToken } }),
  logout: (refreshToken: string) => apiRequest<{ ok: boolean }>("/api/auth/logout", { method: "POST", body: { refreshToken } }),
  completePasswordReset: (resetToken: string, password: string) =>
    apiRequest<{ completed: boolean }>("/api/auth/password-reset/complete", {
      method: "POST",
      body: { resetToken, password },
    }),
  me: (session: StudioSession) => apiRequest<{
    user: AuthResponse["user"];
    organizationId: string;
    workspaceId?: string;
    organizations: OrganizationChoice[];
    membership: OrganizationMember;
    permissions: string[];
  }>("/api/auth/me", { session }),
  updateProfile: (session: StudioSession, body: { displayName?: string; avatarUrl?: string }) =>
    apiRequest<AuthResponse["user"]>("/api/auth/me", { method: "PATCH", session, body }),
  setInitialUsername: (session: StudioSession, username: string) =>
    apiRequest<AuthResponse["user"]>("/api/auth/me/username", { method: "POST", session, body: { username } }),
  registerWithInvitation: (body: JsonRecord) =>
    apiRequest<AuthResponse>("/api/auth/register-with-invitation", { method: "POST", body }),
  resolveOrganizationInvitation: (invitationToken: string) =>
    apiRequest<OrganizationInvitation>("/api/organization-invitations/resolve", {
      method: "POST",
      body: { invitationToken },
    }),
  acceptOrganizationInvitation: (session: StudioSession, invitationToken: string) =>
    apiRequest<AuthResponse>("/api/organization-invitations/accept", {
      method: "POST",
      session,
      body: { invitationToken },
    }),

  listOrganizations: (session: StudioSession) => apiRequest<ListEnvelope<Organization>>("/api/organizations", { session }),
  listSystemOrganizations: (session: StudioSession, query?: { search?: string; page?: number; pageSize?: number }) =>
    apiRequest<SystemOrganizationList>("/api/system/organizations", { session, query }),
  createSystemOrganization: (session: StudioSession, body: CreateSystemOrganizationRequest) =>
    apiRequest<CreatedSystemOrganization>("/api/system/organizations", { method: "POST", session, body }),
  listSystemOrganizationMembers: (
    session: StudioSession,
    organizationId: string,
    query?: { search?: string; status?: string; page?: number; pageSize?: number },
  ) => apiRequest<OrganizationMemberList>(`/api/system/organizations/${organizationId}/members`, { session, query }),
  createSystemOrganizationMember: (
    session: StudioSession,
    organizationId: string,
    body: CreateSystemOrganizationMemberRequest,
  ) => apiRequest<OrganizationMember>(`/api/system/organizations/${organizationId}/members`, {
    method: "POST",
    session,
    body,
  }),
  updateSystemOrganizationMember: (
    session: StudioSession,
    organizationId: string,
    userId: string,
    body: UpdateSystemOrganizationMemberRequest,
  ) => apiRequest<OrganizationMember>(`/api/system/organizations/${organizationId}/members/${userId}`, {
    method: "PATCH",
    session,
    body,
  }),
  updateOrganization: (session: StudioSession, organizationId: string, name: string) =>
    apiRequest<Organization>(`/api/organizations/${organizationId}`, { method: "PATCH", session, body: { name } }),
  leaveOrganization: (session: StudioSession, organizationId: string) =>
    apiRequest<{ left: boolean }>(`/api/organizations/${organizationId}/leave`, { method: "POST", session }),
  listOrganizationAuditLogs: (
    session: StudioSession,
    organizationId: string,
    query?: { action?: string; resourceType?: string; actorUserId?: string; page?: number; pageSize?: number },
  ) => apiRequest<OrganizationAuditLogList>(`/api/organizations/${organizationId}/audit-logs`, { session, query }),
  listOrganizationMembers: (
    session: StudioSession,
    organizationId: string,
    query?: { search?: string; status?: string; page?: number; pageSize?: number },
  ) => apiRequest<OrganizationMemberList>(`/api/organizations/${organizationId}/members`, { session, query }),
  getOrganizationMember: (session: StudioSession, organizationId: string, userId: string) =>
    apiRequest<OrganizationMember>(`/api/organizations/${organizationId}/members/${userId}`, { session }),
  updateOrganizationMemberStatus: (
    session: StudioSession,
    organizationId: string,
    userId: string,
    status: "active" | "disabled",
  ) => apiRequest<OrganizationMember>(`/api/organizations/${organizationId}/members/${userId}`, {
    method: "PATCH",
    session,
    body: { status },
  }),
  updateOrganizationMemberProfile: (
    session: StudioSession,
    organizationId: string,
    userId: string,
    body: { displayName?: string; avatarUrl?: string },
  ) => apiRequest<OrganizationMember>(`/api/organizations/${organizationId}/members/${userId}/profile`, {
    method: "PATCH",
    session,
    body,
  }),
  issueOrganizationMemberPasswordReset: (session: StudioSession, organizationId: string, userId: string) =>
    apiRequest<MemberPasswordReset>(`/api/organizations/${organizationId}/members/${userId}/password-reset`, {
      method: "POST",
      session,
    }),
  removeOrganizationMember: (session: StudioSession, organizationId: string, userId: string) =>
    apiRequest<{ removed: boolean }>(`/api/organizations/${organizationId}/members/${userId}`, {
      method: "DELETE",
      session,
    }),
  listOrganizationInvitations: (session: StudioSession, organizationId: string, page = 1, pageSize = 25) =>
    apiRequest<OrganizationInvitationList>(`/api/organizations/${organizationId}/invitations`, { session, query: { page, pageSize } }),
  createOrganizationInvitation: (session: StudioSession, organizationId: string, body: JsonRecord) =>
    apiRequest<OrganizationInvitation>(`/api/organizations/${organizationId}/invitations`, {
      method: "POST",
      session,
      body,
    }),
  revokeOrganizationInvitation: (session: StudioSession, organizationId: string, invitationId: string) =>
    apiRequest<{ revoked: boolean }>(`/api/organizations/${organizationId}/invitations/${invitationId}`, {
      method: "DELETE",
      session,
    }),
  listWorkspaces: (session: StudioSession) => apiRequest<ListEnvelope<Workspace>>("/api/workspaces", { session }),
  listTeams: (session: StudioSession) => apiRequest<ListEnvelope<Team>>("/api/teams", { session }),
  createTeam: (session: StudioSession, body: JsonRecord) => apiRequest<Team>("/api/teams", { method: "POST", session, body }),
  getTeam: (session: StudioSession, teamId: string) => apiRequest<Team>(`/api/teams/${teamId}`, { session }),
  updateTeam: (session: StudioSession, teamId: string, body: JsonRecord) => apiRequest<Team>(`/api/teams/${teamId}`, { method: "PATCH", session, body }),
  getTeamImpact: (session: StudioSession, teamId: string) => apiRequest<TeamImpact>(`/api/teams/${teamId}/impact`, { session }),
  listTeamMembers: (session: StudioSession, teamId: string) => apiRequest<ListEnvelope<TeamMember>>(`/api/teams/${teamId}/members`, { session }),
  addTeamMember: (session: StudioSession, teamId: string, userId: string) => apiRequest<TeamMember>(`/api/teams/${teamId}/members`, { method: "POST", session, body: { userId } }),
  removeTeamMember: (session: StudioSession, teamId: string, userId: string) => apiRequest<{ deleted: boolean }>(`/api/teams/${teamId}/members/${userId}`, { method: "DELETE", session }),
  listRoles: (session: StudioSession) => apiRequest<ListEnvelope<Role>>("/api/roles", { session }),
  createCustomRole: (session: StudioSession, body: JsonRecord) => apiRequest<Role>("/api/roles", { method: "POST", session, body }),
  getRole: (session: StudioSession, roleId: string) => apiRequest<Role>(`/api/roles/${roleId}`, { session }),
  updateCustomRole: (session: StudioSession, roleId: string, body: JsonRecord) => apiRequest<Role>(`/api/roles/${roleId}`, { method: "PATCH", session, body }),
  deleteCustomRole: (session: StudioSession, roleId: string) => apiRequest<{ deleted: boolean }>(`/api/roles/${roleId}`, { method: "DELETE", session }),
  getRoleImpact: (session: StudioSession, roleId: string) => apiRequest<RoleImpact>(`/api/roles/${roleId}/impact`, { session }),
  listPermissions: (session: StudioSession) => apiRequest<ListEnvelope<Permission>>("/api/permissions", { session }),
  listRoleBindings: (session: StudioSession, query?: { subjectType?: string; subjectId?: string; resourceType?: string; resourceId?: string; roleId?: string; page?: string; pageSize?: string }) =>
	apiRequest<RoleBindingList>("/api/role-bindings", { session, query }),
  createRoleBinding: (session: StudioSession, body: JsonRecord) => apiRequest<RoleBinding>("/api/role-bindings", { method: "POST", session, body }),
  deleteRoleBinding: (session: StudioSession, roleBindingId: string) => apiRequest<{ deleted: boolean }>(`/api/role-bindings/${roleBindingId}`, { method: "DELETE", session }),

  listProjects: (session: StudioSession) => apiRequest<ListEnvelope<Project>>("/api/projects", { session }),
  getProject: (session: StudioSession, projectId: string) => apiRequest<Project>(`/api/projects/${projectId}`, { session }),
  createProject: (session: StudioSession, body: CreateProjectRequest, idempotencyKey?: string) =>
    apiRequest<Project>("/api/projects", { method: "POST", session, body, idempotencyKey }),
  getProjectDeletionImpact: (session: StudioSession, projectId: string) =>
    apiRequest<ProjectDeletionImpact>(`/api/projects/${projectId}/deletion-impact`, { session }),
  createProjectDeletionRequest: (
    session: StudioSession,
    projectId: string,
    body: CreateProjectDeletionRequest,
    idempotencyKey: string,
  ) => apiRequest<ProjectDeletionRequest>(`/api/projects/${projectId}/deletion-requests`, {
    method: "POST",
    session,
    body,
    idempotencyKey,
  }),
  getProjectDeletionRequest: (session: StudioSession, projectId: string, requestId: string) =>
    apiRequest<ProjectDeletionRequest>(`/api/projects/${projectId}/deletion-requests/${requestId}`, { session }),
  retryProjectDeletionRequest: (session: StudioSession, projectId: string, requestId: string) =>
    apiRequest<ProjectDeletionRequest>(`/api/projects/${projectId}/deletion-requests/${requestId}/retry`, {
      method: "POST",
      session,
    }),
  getCommerceProjectOptions: (session: StudioSession, workspaceId: string) =>
    apiRequest<CommerceProjectOptions>(`/api/workspaces/${workspaceId}/commerce/project-options`, { session }),
  getCommerceSetupSession: (session: StudioSession, projectId: string, setupSessionId: string) =>
    apiRequest<CommerceSetupSession>(`/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}`, { session }),
  completeCommerceSetupSession: (
    session: StudioSession,
    projectId: string,
    setupSessionId: string,
    body: CompleteCommerceSetupSessionRequest,
    idempotencyKey: string,
  ) => apiRequest<CommerceSetupCompletionResult>(`/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}/complete`, {
    method: "POST", session, body, idempotencyKey,
  }),
  restartCommerceSetupSession: (
    session: StudioSession,
    projectId: string,
    setupSessionId: string,
    expectedRevision: number,
  ) => apiRequest<CommerceSetupSession>(`/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}/restart`, {
    method: "POST", session, body: { expectedRevision },
  }),
  confirmCommerceSetupLanguage: (
    session: StudioSession,
    projectId: string,
    setupSessionId: string,
    body: ConfirmCommerceSetupLanguageRequest,
  ) => apiRequest<CommerceSetupLanguageConfirmationResult>(`/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}/language-confirmation`, {
    method: "POST", session, body,
  }),
  abandonCommerceSetupSession: (session: StudioSession, projectId: string, setupSessionId: string, expectedRevision: number) =>
    apiRequest<CommerceSetupSession>(`/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}/abandon`, {
      method: "POST", session, body: { expectedRevision },
    }),
  getCommerceProduct: (session: StudioSession, projectId: string) =>
    apiRequest<CommerceProduct>(`/api/projects/${projectId}/commerce/product`, { session }),
  createCommerceProductVersion: (
    session: StudioSession,
    projectId: string,
    body: {
      expectedRevision?: number;
      name: string;
      brand?: string;
      sellingPoints?: string[];
      immutableFeatures?: JsonRecord;
      prohibitedClaims?: string[];
      metadata?: JsonRecord;
    },
  ) => apiRequest<CommerceProductMutationResult>(`/api/projects/${projectId}/commerce/product/versions`, { method: "POST", session, body }),
  listCommerceProductVersions: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<CommerceProductVersion>>(`/api/projects/${projectId}/commerce/product/versions`, { session }),
  getCommerceProductVersion: (session: StudioSession, projectId: string, versionId: string) =>
    apiRequest<CommerceProductVersion>(`/api/projects/${projectId}/commerce/product/versions/${versionId}`, { session }),
  listCommerceProductReferences: (session: StudioSession, projectId: string, status: ArchiveListStatus = "active") =>
    apiRequest<ListEnvelope<CommerceProductReference>>(`/api/projects/${projectId}/commerce/product/references`, {
      session, query: { "filter[status]": status },
    }),
  listCommerceProductReferencePacks: (session: StudioSession, projectId: string, status: "active" | "stale" | "archived" | "all" = "active") =>
    apiRequest<ListEnvelope<CommerceProductReferencePack>>(`/api/projects/${projectId}/commerce/product/reference-packs`, {
      session, query: { "filter[status]": status },
    }),
  getCommerceProductReferencePack: (session: StudioSession, projectId: string, packId: string) =>
    apiRequest<CommerceProductReferencePack>(`/api/projects/${projectId}/commerce/product/reference-packs/${packId}`, { session }),
  createCommerceProductReferenceUpload: (
    session: StudioSession,
    projectId: string,
    body: { setupSessionId?: string; fileName: string; mimeType: string; expiresSeconds?: number },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductReferenceUpload>(`/api/projects/${projectId}/commerce/product/references/upload-url`, {
    method: "POST", session, body, idempotencyKey,
  }),
  uploadCommerceProductReferenceFile: async (upload: CommerceProductReferenceUpload, file: File) => {
    let response: Response;
    try {
      response = await fetch(upload.uploadUrl, { method: upload.method || "PUT", headers: upload.headers, body: file });
    } catch {
      throw new StudioApiError(
        "无法连接对象存储，请检查网络或存储域名配置后重试",
        "UPLOAD_NETWORK_ERROR",
        0,
        true,
      );
    }
    if (!response.ok) throw new StudioApiError(`商品图片上传失败：HTTP ${response.status}`, "UPLOAD_FAILED", response.status, true);
  },
  completeCommerceProductReferenceUpload: (
    session: StudioSession,
    projectId: string,
    body: { uploadId: string; referenceRole?: string; setPrimary?: boolean },
  ) => apiRequest<CommerceProductReference>(`/api/projects/${projectId}/commerce/product/references`, { method: "POST", session, body }),
  updateCommerceProductReference: (
    session: StudioSession,
    projectId: string,
    referenceId: string,
    body: { expectedRevision: number; referenceRole?: string; ordinal?: number; setPrimary?: boolean },
  ) => apiRequest<CommerceProductReference>(`/api/projects/${projectId}/commerce/product/references/${referenceId}`, {
    method: "PATCH", session, body,
  }),
  archiveCommerceProductReference: (session: StudioSession, projectId: string, referenceId: string, expectedRevision: number) =>
    apiRequest<CommerceProductReference>(`/api/projects/${projectId}/commerce/product/references/${referenceId}`, {
      method: "DELETE", session, body: { expectedRevision },
    }),
  getCommerceProductRebuildImpact: (
    session: StudioSession,
    projectId: string,
    body: { targetProductVersionId: string; targetReferenceIds: string[]; expectedProductRevision: number },
  ) => apiRequest<CommerceProductRebuildImpact>(`/api/projects/${projectId}/commerce/product/rebuild-impact`, {
    method: "POST", session, body,
  }),
  createCommerceProductRebuild: (
    session: StudioSession,
    projectId: string,
    body: { impactToken: string; expectedProductRevision: number },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductRebuildResult>(`/api/projects/${projectId}/commerce/product/rebuilds`, {
    method: "POST", session, body, idempotencyKey,
  }),
  listCommerceScriptUnits: (
    session: StudioSession,
    projectId: string,
    options: { status?: ArchiveListStatus; cursor?: string; limit?: number; includeProductionSummary?: boolean } = {},
  ) => apiRequest<CommerceScriptUnitList>(`/api/projects/${projectId}/commerce/script-units`, {
    session,
    query: {
      "filter[status]": options.status ?? "active",
      cursor: options.cursor,
      limit: options.limit,
      include: options.includeProductionSummary ? "productionSummary" : undefined,
    },
  }),
  getCommerceProjectProductionStatus: (session: StudioSession, projectId: string) =>
    apiRequest<CommerceProjectProductionStatus>(`/api/projects/${projectId}/commerce/production-status`, { session }),
  updateCommerceScriptUnitDefaults: (
    session: StudioSession,
    projectId: string,
    body: UpdateCommerceScriptUnitDefaultsRequest,
  ) => apiRequest<Project>(`/api/projects/${projectId}/commerce/script-unit-defaults`, { method: "PATCH", session, body }),
  getCommerceUnitProductionStatus: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<CommerceUnitProductionStatus>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/production-status`, { session }),
  getCommerceScriptUnit: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}`, { session }),
  createCommerceScriptUnit: (
    session: StudioSession,
    projectId: string,
    body: {
      expectedScriptUnitsRevision: number;
      title: string;
      content: string;
      languageMode: "auto" | "explicit";
      explicitTargetLanguage?: string;
      targetDurationSeconds: number;
      targetPlatform?: string;
      sourceLanguageHint?: string;
    },
  ) => apiRequest<CommerceScriptVersionMutation>(`/api/projects/${projectId}/commerce/script-units`, { method: "POST", session, body }),
  updateCommerceScriptUnit: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      expectedRevision: number;
      title?: string;
      draftContent?: string;
      languageMode?: "auto" | "explicit";
      explicitTargetLanguage?: string | null;
      targetDurationSeconds?: number;
      targetPlatform?: string;
    },
  ) => apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}`, { method: "PATCH", session, body }),
  archiveCommerceScriptUnit: (session: StudioSession, projectId: string, scriptUnitId: string, expectedRevision: number) =>
    apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}`, {
      method: "DELETE", session, body: { expectedRevision },
    }),
  reorderCommerceScriptUnits: (
    session: StudioSession,
    projectId: string,
    body: { expectedScriptUnitsRevision: number; items: Array<{ scriptUnitId: string; sortOrder: number }> },
  ) => apiRequest<{ scriptUnitsRevision: number }>(`/api/projects/${projectId}/commerce/script-units/reorder`, { method: "POST", session, body }),
  duplicateCommerceScriptUnit: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    expectedScriptUnitsRevision: number,
  ) => apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/duplicate`, {
    method: "POST", session, body: { expectedScriptUnitsRevision },
  }),
  createCommerceScriptLanguageVariant: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { expectedScriptUnitsRevision: number; targetLanguage: string },
  ) => apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/language-variants`, {
    method: "POST", session, body,
  }),
  prepareCommerceScriptUnit: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    expectedRevision: number,
  ) => apiRequest<WorkflowRun>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/prepare`, {
    method: "POST", session, body: { expectedRevision },
  }),
  organizeCommerceScriptUnit: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    expectedUnitGenerationId: string,
    idempotencyKey: string,
  ) => apiRequest<WorkflowRun>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/organize`, {
    method: "POST", session, body: { expectedUnitGenerationId }, idempotencyKey,
  }),
  getCommerceScriptUnitRebuildImpact: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      expectedRevision: number;
      targetSourceScriptVersionId: string;
      targetLanguageMode: "explicit" | "auto";
      targetLanguage?: string;
      targetDurationSeconds: number;
      targetPlatform: string;
      targetStoryboardStrategy: "smart" | "single_take";
    },
  ) => apiRequest<CommerceScriptUnitRebuildImpact>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/rebuild-impact`, {
    method: "POST", session, body,
  }),
  createCommerceScriptUnitRebuild: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { impactToken: string; expectedRevision: number },
    idempotencyKey: string,
  ) => apiRequest<WorkflowRun>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/rebuilds`, {
    method: "POST", session, body, idempotencyKey,
  }),
  listCommerceScriptVersions: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<ListEnvelope<CommerceScriptVersion>>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/versions`, { session }),
  getCommerceScriptVersion: (session: StudioSession, projectId: string, scriptUnitId: string, versionId: string) =>
    apiRequest<CommerceScriptVersion>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/versions/${versionId}`, { session }),
  createCommerceScriptVersion: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { expectedRevision: number; content: string; sourceLanguageHint?: string; activate?: boolean },
  ) => apiRequest<CommerceScriptVersionMutation>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/versions`, {
    method: "POST", session, body,
  }),
  getCommerceDirectVideoOptions: (session: StudioSession, projectId: string) =>
    apiRequest<CommerceDirectVideoOptions>(`/api/projects/${projectId}/commerce/video-options`, { session }),
  listCommerceScriptReferences: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    status: ArchiveListStatus = "active",
  ) => apiRequest<ListEnvelope<CommerceScriptReferenceImage>>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/references`,
    { session, query: { "filter[status]": status } },
  ),
  createCommerceScriptReferenceUpload: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { fileName: string; mimeType: string; expiresSeconds?: number },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductReferenceUpload>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/references/upload-url`,
    { method: "POST", session, body, idempotencyKey },
  ),
  uploadCommerceScriptReferenceFile: async (upload: CommerceProductReferenceUpload, file: File) => {
    let response: Response;
    try {
      response = await fetch(upload.uploadUrl, {
        method: upload.method || "PUT",
        headers: upload.headers,
        body: file,
      });
    } catch {
      throw new StudioApiError(
        "无法连接对象存储，请检查网络或存储域名配置后重试",
        "UPLOAD_NETWORK_ERROR",
        0,
        true,
      );
    }
    if (!response.ok) {
      throw new StudioApiError(`自定义参考图上传失败：HTTP ${response.status}`, "UPLOAD_FAILED", response.status, true);
    }
  },
  completeCommerceScriptReferenceUpload: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    uploadId: string,
  ) => apiRequest<CommerceScriptReferenceImage>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/references/complete`,
    { method: "POST", session, body: { uploadId } },
  ),
  archiveCommerceScriptReference: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    referenceId: string,
    expectedRevision: number,
  ) => apiRequest<CommerceScriptReferenceImage>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/references/${referenceId}`,
    { method: "DELETE", session, body: { expectedRevision } },
  ),
  listCommerceDirectVideos: (
    session: StudioSession,
    projectId: string,
    scriptUnitId?: string,
  ) => apiRequest<ListEnvelope<CommerceDirectVideoJob>>(
    `/api/projects/${projectId}/commerce/direct-videos`,
    { session, query: scriptUnitId ? { "filter[scriptUnitId]": scriptUnitId } : undefined },
  ),
  getCommerceDirectVideo: (session: StudioSession, projectId: string, jobId: string) =>
    apiRequest<CommerceDirectVideoJob>(`/api/projects/${projectId}/commerce/direct-videos/${jobId}`, { session }),
  createCommerceDirectVideo: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: CreateCommerceDirectVideoRequest,
    idempotencyKey: string,
  ) => apiRequest<CommerceDirectVideoJob>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/direct-videos`,
    { method: "POST", session, body, idempotencyKey },
  ),
  activateCommerceScriptVersion: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    versionId: string,
    expectedRevision: number,
  ) => apiRequest<CommerceScriptUnit>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/versions/${versionId}/activate`, {
    method: "POST", session, body: { expectedRevision },
  }),
  resolveCommerceScriptLanguage: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<CommerceLanguageResolution>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/language-resolution`, {
      method: "POST", session, body: {},
    }),
  getCommerceScriptLanguageResolution: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<CommerceLanguageResolution>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/language-resolution`, { session }),
  confirmCommerceScriptLanguage: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { languageResolutionId: string; targetLanguage: string },
  ) => apiRequest<CommerceLanguageResolution | CommerceLanguageConfirmationAccepted>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/language-confirmation`, {
    method: "POST", session, body,
  }),
  listCommerceScriptLocalizations: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<ListEnvelope<CommerceScriptLocalization>>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/localizations`, { session }),
  getCommerceScriptLocalization: (session: StudioSession, projectId: string, scriptUnitId: string, localizationId: string) =>
    apiRequest<CommerceScriptLocalization>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/localizations/${localizationId}`, { session }),
  createCommerceScriptLocalization: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      sourceScriptVersionId: string;
      languageResolutionId: string;
      sourceLanguage: string;
      targetLanguage: string;
      localizedContent: string;
      structuredContract?: JsonRecord;
      reviewerOutput?: JsonRecord;
      approve?: boolean;
    },
  ) => apiRequest<CommerceScriptLocalization>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/localizations`, {
    method: "POST", session, body,
  }),
  activateCommerceScriptLocalization: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    localizationId: string,
    expectedRevision: number,
  ) => apiRequest<CommerceScriptLocalization>(`/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/localizations/${localizationId}/activate`, {
    method: "POST", session, body: { expectedRevision },
  }),
  listCommerceStoryboardPlans: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    status: "active" | "archived" | "all" = "active",
  ) => apiRequest<ListEnvelope<CommerceStoryboardPlan>>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/storyboard-plans`,
    { session, query: { "filter[status]": status } },
  ),
  createCommerceStoryboardPlan: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    scriptUnitGenerationId: string,
    body: {
      expectedScriptUnitRevision: number;
      expectedProjectProductionGenerationId: string;
      previewHash: string;
      videoExecutionEnvelopeHash: string;
      clientRequestId: string;
    },
    idempotencyKey: string,
  ) => apiRequest<WorkflowRun>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/generations/${scriptUnitGenerationId}/storyboard-plans`,
    { method: "POST", session, body, idempotencyKey },
  ),
  previewCommerceStoryboardPlan: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    scriptUnitGenerationId: string,
    body: {
      expectedScriptUnitRevision: number;
      expectedProjectProductionGenerationId: string;
      clientRequestId: string;
    },
  ) => apiRequest<CommerceStoryboardPlanningPreview>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/generations/${scriptUnitGenerationId}/storyboard-planning-preview`,
    { method: "POST", session, body },
  ),
  getCommerceStoryboardPlan: (session: StudioSession, projectId: string, scriptUnitId: string, planId: string) =>
    apiRequest<CommerceStoryboardPlanDetail>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/storyboard-plans/${planId}`,
      { session },
    ),
  activateCommerceStoryboardPlan: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    planId: string,
    expectedRevision: number,
  ) => apiRequest<CommerceStoryboardPlanDetail>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/storyboard-plans/${planId}/activate`,
    { method: "POST", session, body: { expectedRevision } },
  ),
  updateCommerceStoryboardShot: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    shotId: string,
    body: {
      expectedPlanRevision: number;
      expectedShotRevision: number;
      visualAction?: string;
      shotPurpose?: string;
      composition?: string;
      camera?: JsonRecord;
      voiceoverText?: string;
      onscreenText?: string;
      durationSeconds?: number;
      productReferenceIds?: string[];
    },
  ) => apiRequest<CommerceStoryboardPlanDetail>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/shots/${shotId}`,
    { method: "PATCH", session, body },
  ),
  archiveCommerceStoryboardShot: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    shotId: string,
    planId: string,
    expectedPlanRevision: number,
  ) => apiRequest<CommerceStoryboardPlanDetail>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/shots/${shotId}`,
    {
      method: "DELETE", session,
      headers: { "If-Match": `W/"commerce-storyboard:${planId}:${expectedPlanRevision}"` },
    },
  ),
  reorderCommerceStoryboardShots: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: { planId: string; expectedPlanRevision: number; items: Array<{ shotId: string; durationSeconds?: number }> },
  ) => apiRequest<CommerceStoryboardPlanDetail>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/shots/reorder`,
    { method: "POST", session, body },
  ),
  generateCommerceReferenceImageBatch: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      operation: "generate_prompts" | "generate_images";
      planId: string;
      expectedPlanRevision: number;
      expectedUnitGenerationId: string;
      shotIds: string[];
      force?: boolean;
      concurrency?: number;
    },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductionRun>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/reference-images/generate-batch`,
    { method: "POST", session, body, idempotencyKey },
  ),
  generateCommerceVideoPrompts: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: CommerceVideoBatchRequest,
    idempotencyKey: string,
  ) => apiRequest<CommerceProductionRun>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/video-prompts/generate-batch`,
    { method: "POST", session, body, idempotencyKey },
  ),
  generateCommerceShotVideos: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: CommerceVideoBatchRequest,
    idempotencyKey: string,
  ) => apiRequest<CommerceProductionRun>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/shot-videos/generate-batch`,
    { method: "POST", session, body, idempotencyKey },
  ),
  listCommerceTimelines: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<{ items: CommerceTimeline[]; unitGenerationId: string }>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/timelines`,
      { session },
    ),
  prepareCommerceTimeline: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      storyboardPlanId: string;
      expectedPlanRevision: number;
      expectedUnitGenerationId: string;
      title?: string;
      resolution?: string;
    },
    idempotencyKey: string,
  ) => apiRequest<CommerceTimeline>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/timelines/prepare`,
    { method: "POST", session, body, idempotencyKey },
  ),
  getCommerceTimeline: (session: StudioSession, projectId: string, scriptUnitId: string, timelineId: string) =>
    apiRequest<CommerceTimelineDetail>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/timelines/${timelineId}`,
      { session },
    ),
  updateCommerceTimeline: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    timelineId: string,
    body: {
      expectedRevision: number;
      title?: string;
      overlays?: Array<Omit<CommerceTimelineOverlay, "id" | "timelineId" | "contentHash">>;
    },
  ) => apiRequest<CommerceTimeline>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/timelines/${timelineId}`,
    { method: "PATCH", session, body },
  ),
  composeCommerceFinalVideo: (
    session: StudioSession,
    projectId: string,
    scriptUnitId: string,
    body: {
      timelineId: string;
      expectedTimelineRevision: number;
      expectedUnitGenerationId: string;
      title?: string;
      resolution?: string;
    },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductionRun>(
    `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/final-videos/compose`,
    { method: "POST", session, body, idempotencyKey },
  ),
  listCommerceFinalVideos: (session: StudioSession, projectId: string, scriptUnitId: string) =>
    apiRequest<{ items: CommerceFinalVideoVersion[]; unitGenerationId: string }>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/final-videos`,
      { session },
    ),
  getCommerceFinalVideo: (session: StudioSession, projectId: string, scriptUnitId: string, finalVideoVersionId: string) =>
    apiRequest<CommerceFinalVideoVersion>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/final-videos/${finalVideoVersionId}`,
      { session },
    ),
  activateCommerceFinalVideo: (session: StudioSession, projectId: string, scriptUnitId: string, finalVideoVersionId: string) =>
    apiRequest<{ activated: boolean }>(
      `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/final-videos/${finalVideoVersionId}/activate`,
      { method: "POST", session },
    ),
  listCommerceProductionRuns: (
    session: StudioSession,
    projectId: string,
    filters: { scriptUnitId?: string; runType?: CommerceProductionRunType; limit?: number } = {},
  ) => apiRequest<ListEnvelope<CommerceProductionRun>>(`/api/projects/${projectId}/commerce/production-runs`, {
    session,
    query: {
      "filter[scriptUnitId]": filters.scriptUnitId,
      "filter[runType]": filters.runType,
      limit: filters.limit,
    },
  }),
  getCommerceProductionRun: (session: StudioSession, projectId: string, runId: string) =>
    apiRequest<CommerceProductionRunDetail>(`/api/projects/${projectId}/commerce/production-runs/${runId}`, { session }),
  retryFailedCommerceProductionRun: (
    session: StudioSession,
    projectId: string,
    runId: string,
    body: { itemIds?: string[]; concurrency?: number },
    idempotencyKey: string,
  ) => apiRequest<CommerceProductionRun>(
    `/api/projects/${projectId}/commerce/production-runs/${runId}/retry-failed`,
    { method: "POST", session, body, idempotencyKey },
  ),
  cancelCommerceProductionRun: (
    session: StudioSession,
    projectId: string,
    runId: string,
    reason = "用户取消带货视频生产批次",
  ) => apiRequest<CommerceProductionRunDetail>(
    `/api/projects/${projectId}/commerce/production-runs/${runId}/cancel`,
    { method: "POST", session, body: { reason } },
  ),
  listCommerceScriptUnitBatches: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<CommerceScriptUnitBatch>>(
      `/api/projects/${projectId}/commerce/script-unit-batches`,
      { session },
    ),
  getCommerceScriptUnitBatch: (session: StudioSession, projectId: string, coordinatorId: string) =>
    apiRequest<CommerceScriptUnitBatch>(
      `/api/projects/${projectId}/commerce/script-unit-batches/${coordinatorId}`,
      { session },
    ),
  createCommerceScriptUnitBatch: (
    session: StudioSession,
    projectId: string,
    body: {
      targetStage: CommerceScriptUnitBatchStage;
      items: CommerceScriptUnitBatchAdvanceItem[];
      unitConcurrency?: number;
      maxConcurrency?: number;
    },
    idempotencyKey: string,
  ) => apiRequest<CommerceScriptUnitBatch>(
    `/api/projects/${projectId}/commerce/script-unit-batches`,
    { method: "POST", session, body, idempotencyKey },
  ),
  retryCommerceScriptUnitBatch: (
    session: StudioSession,
    projectId: string,
    coordinatorId: string,
    body: { scriptUnitIds?: string[]; maxConcurrency?: number },
    idempotencyKey: string,
  ) => apiRequest<CommerceScriptUnitBatch>(
    `/api/projects/${projectId}/commerce/script-unit-batches/${coordinatorId}/retry-failed`,
    { method: "POST", session, body, idempotencyKey },
  ),
  cancelCommerceScriptUnitBatch: (
    session: StudioSession,
    projectId: string,
    coordinatorId: string,
    reason = "用户取消跨脚本批量任务",
  ) => apiRequest<CommerceScriptUnitBatch>(
    `/api/projects/${projectId}/commerce/script-unit-batches/${coordinatorId}/cancel`,
    { method: "POST", session, body: { reason } },
  ),
  updateProject: (session: StudioSession, projectId: string, body: UpdateProjectRequest) =>
    apiRequest<Project>(`/api/projects/${projectId}`, { method: "PATCH", session, body }),
  listVideoProductionProfiles: (session: StudioSession) =>
    apiRequest<ListEnvelope<VideoProductionProfileVersion>>("/api/video-production-profiles", { session }),
  getProjectVideoProductionProfile: (session: StudioSession, projectId: string) =>
    apiRequest<{
      profile: VideoProductionProfileVersion;
      binding: Project["videoProductionBinding"];
      productionGeneration: Project["productionGeneration"];
      state: NonNullable<Project["videoProductionState"]>;
      locked: boolean;
    }>(`/api/projects/${projectId}/video-production-profile`, { session }),
  getProjectVideoProductionCompatibility: (
    session: StudioSession,
    projectId: string,
    targetProfileKey?: VideoProductionProfileKey,
    targetProfileVersion?: number,
  ) =>
    apiRequest<VideoProductionCompatibility>(`/api/projects/${projectId}/video-production-profile/compatibility`, {
      session,
      query: { targetProfileKey, targetProfileVersion },
    }),
  getProjectVideoProductionRebuildImpact: (
    session: StudioSession,
    projectId: string,
    targetProfileKey: VideoProductionProfileKey,
    targetProfileVersion?: number,
    targetConfiguration?: VideoProductionConfigurationInput,
  ) =>
    apiRequest<{ impact: VideoProductionRebuildImpact; compatibility: VideoProductionCompatibility }>(
      `/api/projects/${projectId}/video-production/rebuild-impact`,
      { method: "POST", session, body: { targetProfileKey, targetProfileVersion, targetConfiguration } },
    ),
  createProjectVideoProductionRebuild: (
    session: StudioSession,
    projectId: string,
    idempotencyKey: string,
    body: { expectedProjectRevision: number; targetProfileKey: VideoProductionProfileKey; targetProfileVersion?: number; targetConfiguration: VideoProductionConfigurationInput; impactToken: string },
  ) =>
    apiRequest<VideoProductionRebuild>(`/api/projects/${projectId}/video-production/rebuilds`, {
      method: "POST",
      session,
      idempotencyKey,
      body,
    }),
  getCurrentProjectVideoProductionRebuild: (session: StudioSession, projectId: string) =>
    apiRequest<VideoProductionRebuild | null>(`/api/projects/${projectId}/video-production/rebuilds/current`, { session }),
  getProjectVideoProductionRebuild: (session: StudioSession, projectId: string, rebuildId: string) =>
    apiRequest<VideoProductionRebuild>(`/api/projects/${projectId}/video-production/rebuilds/${rebuildId}`, { session }),
  listProjectVideoProductionRebuildItems: (session: StudioSession, projectId: string, rebuildId: string) =>
    apiRequest<ListEnvelope<VideoProductionRebuildItem>>(`/api/projects/${projectId}/video-production/rebuilds/${rebuildId}/items`, { session }),
  retryFailedProjectVideoProductionRebuildItems: (session: StudioSession, projectId: string, rebuildId: string, idempotencyKey: string) =>
    apiRequest<VideoProductionRebuild>(`/api/projects/${projectId}/video-production/rebuilds/${rebuildId}/retry-failed`, {
      method: "POST",
      session,
      idempotencyKey,
      body: {},
    }),
  listProjectManualTemplates: (session: StudioSession, kind?: "director" | "visual") =>
    apiRequest<ListEnvelope<PromptTemplate>>("/api/project-manual-templates", {
      session,
      query: kind ? { "filter[kind]": kind } : undefined,
    }),
  listProjectManualBindings: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ProjectManualBinding>>(`/api/projects/${projectId}/manual-bindings`, { session }),
  bindProjectManual: (session: StudioSession, projectId: string, manualKind: "director" | "visual", promptVersionId: string) =>
    apiRequest<ProjectManualBinding>(`/api/projects/${projectId}/manual-bindings/${manualKind}`, {
      method: "PUT",
      session,
      body: { promptVersionId },
    }),
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
  generateReviewFix: (session: StudioSession, projectId: string, itemId: string, body: JsonRecord) =>
    apiRequest<ReviewFix>(`/api/projects/${projectId}/review-items/${itemId}/fixes/generate`, { method: "POST", session, body }),
  listReviewFixes: (session: StudioSession, projectId: string, itemId: string) =>
    apiRequest<ListEnvelope<ReviewFix>>(`/api/projects/${projectId}/review-items/${itemId}/fixes`, { session }),
  getReviewFix: (session: StudioSession, projectId: string, fixId: string) =>
    apiRequest<ReviewFix>(`/api/projects/${projectId}/review-fixes/${fixId}`, { session }),
  applyReviewFix: (session: StudioSession, projectId: string, fixId: string, body: JsonRecord) =>
    apiRequest<ApplyReviewFixResponse>(`/api/projects/${projectId}/review-fixes/${fixId}/apply`, { method: "POST", session, body }),
  dismissReviewFix: (session: StudioSession, projectId: string, fixId: string) =>
    apiRequest<DismissReviewFixResponse>(`/api/projects/${projectId}/review-fixes/${fixId}/dismiss`, { method: "POST", session }),
  getShotProductionStatus: (session: StudioSession, projectId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ShotProductionStatus>(`/api/projects/${projectId}/shot-production/status`, { session, query }),
  runShotProductionAction: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ShotProductionActionResponse>(`/api/projects/${projectId}/shot-production/actions`, { method: "POST", session, body }),
  generateVideoPromptsBatch: (session: StudioSession, projectId: string, body: ShotProductionBatchRequest) =>
    apiRequest<ShotProductionActionResponse>(`/api/projects/${projectId}/video-prompts/generate-batch`, { method: "POST", session, body }),
  generateShotVideosBatch: (session: StudioSession, projectId: string, body: ShotProductionBatchRequest) =>
    apiRequest<ShotProductionActionResponse>(`/api/projects/${projectId}/shot-videos/generate-batch`, { method: "POST", session, body }),
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
  deleteFinalVideo: (session: StudioSession, projectId: string, versionId: string, confirmActive = false) =>
    apiRequest<{ deleted: boolean; versionId: string }>(`/api/projects/${projectId}/final-videos/${versionId}`, {
      method: "DELETE",
      session,
      query: confirmActive ? { confirmActive: true } : undefined,
    }),

  listSources: (session: StudioSession, projectId: string, status?: ArchiveListStatus) =>
    apiRequest<ListEnvelope<ProjectSource>>(`/api/projects/${projectId}/sources`, {
      session,
      query: status ? { "filter[status]": status } : undefined,
    }),
  getSource: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources/${sourceId}`, { session }),
  createSource: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ImportProjectSourceResponse>(`/api/projects/${projectId}/sources`, { method: "POST", session, body }),
  importSourceFile: (session: StudioSession, projectId: string, body: FormData) =>
    apiRequest<ImportProjectSourceResponse>(`/api/projects/${projectId}/sources/import`, { method: "POST", session, body }),
  updateSource: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "PATCH", session, body }),
  getSourceImpact: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<OutputImpact>(`/api/projects/${projectId}/sources/${sourceId}/impact`, { session }),
  deleteSource: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<{ deleted: boolean; mode?: string }>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "DELETE", session }),
  listSourceChapters: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<ListEnvelope<NovelChapterSummary>>(`/api/projects/${projectId}/sources/${sourceId}/chapters`, { session }),
  getSourceChapter: (session: StudioSession, projectId: string, sourceId: string, chapterId: string) =>
    apiRequest<NovelChapter>(`/api/projects/${projectId}/sources/${sourceId}/chapters/${chapterId}`, { session }),
  deleteSourceChapter: (session: StudioSession, projectId: string, sourceId: string, chapterId: string) =>
    apiRequest<{
      deleted: boolean;
      mode: "delete_chapter";
      sourceId: string;
      chapterId: string;
      deletedChapterIndex: number;
      remainingChapterCount: number;
    }>(`/api/projects/${projectId}/sources/${sourceId}/chapters/${chapterId}`, { method: "DELETE", session }),
  extractNovelEvents: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/sources/${sourceId}/extract-events`, { method: "POST", session, body }),
  listSourceNovelEvents: (session: StudioSession, projectId: string, sourceId: string, query?: { chapterId?: string }) =>
    apiRequest<{ items: NovelEvent[]; links: NovelEventLink[] }>(`/api/projects/${projectId}/sources/${sourceId}/events`, { session, query }),
  updateNovelEvent: (session: StudioSession, projectId: string, eventId: string, body: JsonRecord) =>
    apiRequest<NovelEvent>(`/api/projects/${projectId}/novel-events/${eventId}`, { method: "PATCH", session, body }),
  reviewNovelEvent: (session: StudioSession, projectId: string, eventId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/novel-events/${eventId}/review`, { method: "POST", session, body }),
  listAdaptationPlans: (session: StudioSession, projectId: string, sourceId?: string) =>
    apiRequest<ListEnvelope<AdaptationPlan>>(`/api/projects/${projectId}/adaptation-plans`, { session, query: sourceId ? { sourceId } : undefined }),
  getAdaptationPlan: (session: StudioSession, projectId: string, planId: string) =>
    apiRequest<AdaptationPlan>(`/api/projects/${projectId}/adaptation-plans/${planId}`, { session }),
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
  updateScript: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<Script>(`/api/projects/${projectId}/scripts/${scriptId}`, { method: "PATCH", session, body }),
  listScriptVersions: (session: StudioSession, projectId: string, scriptId: string) =>
    apiRequest<ListEnvelope<ScriptVersion>>(`/api/projects/${projectId}/scripts/${scriptId}/versions`, { session }),
  listScriptEpisodes: (session: StudioSession, projectId: string, scriptId: string, versionId: string) =>
    apiRequest<ListEnvelope<ScriptEpisode>>(`/api/projects/${projectId}/scripts/${scriptId}/versions/${versionId}/episodes`, { session }),
  createScriptVersion: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<ScriptVersion>(`/api/projects/${projectId}/scripts/${scriptId}/versions`, { method: "POST", session, body }),
  activateScriptVersion: (session: StudioSession, projectId: string, scriptId: string, versionId: string) =>
    apiRequest<Script>(`/api/projects/${projectId}/scripts/${scriptId}/activate-version`, { method: "POST", session, body: { versionId } }),
  deleteScriptVersion: (session: StudioSession, projectId: string, scriptId: string, versionId: string) =>
    apiRequest<{ deleted: boolean; mode?: string; versionId: string }>(`/api/projects/${projectId}/scripts/${scriptId}/versions/${versionId}`, { method: "DELETE", session }),
  parseScriptScenes: (session: StudioSession, projectId: string, scriptId: string, versionId: string, body: JsonRecord) =>
    apiRequest<ParseScriptScenesResponse>(`/api/projects/${projectId}/scripts/${scriptId}/versions/${versionId}/parse-scenes`, { method: "POST", session, body }),
  listScriptScenes: (session: StudioSession, projectId: string, scriptId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ListEnvelope<ScriptScene>>(`/api/projects/${projectId}/scripts/${scriptId}/scenes`, { session, query }),
  updateScriptEpisode: (session: StudioSession, projectId: string, episodeId: string, body: JsonRecord) =>
    apiRequest<ScriptEpisode>(`/api/projects/${projectId}/script-episodes/${episodeId}`, { method: "PATCH", session, body }),
  getEpisodeAudio: (session: StudioSession, projectId: string, episodeId: string) =>
    apiRequest<EpisodeAudio>(`/api/projects/${projectId}/script-episodes/${episodeId}/audio`, { session }),
  produceEpisodeAudio: (session: StudioSession, projectId: string, episodeId: string, body: JsonRecord = {}) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/script-episodes/${episodeId}/audio/produce`, { method: "POST", session, body }),
  updateScriptScene: (session: StudioSession, projectId: string, sceneId: string, body: JsonRecord) =>
    apiRequest<ScriptScene>(`/api/projects/${projectId}/script-scenes/${sceneId}`, { method: "PATCH", session, body }),
  deleteScriptScene: (session: StudioSession, projectId: string, sceneId: string) =>
    apiRequest<{ deleted: boolean; mode?: string; sceneId: string }>(`/api/projects/${projectId}/script-scenes/${sceneId}`, { method: "DELETE", session }),
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
    apiRequest<{ scriptId: string; versionId: string; content: string; agentRunId: string; activated?: boolean; previousVersionId?: string }>(`/api/projects/${projectId}/script-agent/rewrite-script`, {
      method: "POST",
      session,
      body,
    }),
  listAgentTools: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<AgentToolDescriptor>>(`/api/projects/${projectId}/agent/tools`, { session }),
  createAgentTask: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<AgentTask>(`/api/projects/${projectId}/agent/tasks`, { method: "POST", session, body }),
  listAgentTasks: (session: StudioSession, projectId: string, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ListEnvelope<AgentTask>>(`/api/projects/${projectId}/agent/tasks`, { session, query }),
  getAgentTask: (session: StudioSession, projectId: string, taskId: string) =>
    apiRequest<AgentTask>(`/api/projects/${projectId}/agent/tasks/${taskId}`, { session }),
  cancelAgentTask: (session: StudioSession, projectId: string, taskId: string, body: JsonRecord = {}) =>
    apiRequest<AgentTask>(`/api/projects/${projectId}/agent/tasks/${taskId}/cancel`, { method: "POST", session, body }),
  approveAgentStep: (session: StudioSession, projectId: string, taskId: string, stepId: string, body: JsonRecord = {}) =>
    apiRequest<AgentApproval>(`/api/projects/${projectId}/agent/tasks/${taskId}/steps/${stepId}/approve`, { method: "POST", session, body }),
  rejectAgentStep: (session: StudioSession, projectId: string, taskId: string, stepId: string, body: JsonRecord = {}) =>
    apiRequest<AgentApproval>(`/api/projects/${projectId}/agent/tasks/${taskId}/steps/${stepId}/reject`, { method: "POST", session, body }),
  resumeAgentTask: (session: StudioSession, projectId: string, taskId: string) =>
    apiRequest<AgentTask>(`/api/projects/${projectId}/agent/tasks/${taskId}/resume`, { method: "POST", session, body: {} }),

  listCanonicalAssets: (
    session: StudioSession,
    projectId: string,
    options: CanonicalAssetListOptions = {},
  ) =>
    apiRequest<ListEnvelope<CanonicalAsset>>(`/api/projects/${projectId}/canonical-assets`, {
      session,
      query: {
        "filter[status]": options.status,
        "filter[type]": options.assetType?.trim() || undefined,
        includePreviewUrl: options.includePreviewUrl || undefined,
        previewExpiresSeconds: options.includePreviewUrl ? options.previewExpiresSeconds ?? 900 : undefined,
      },
    }),
  getCanonicalAsset: (session: StudioSession, projectId: string, assetId: string, includePreviewUrl = false, previewExpiresSeconds = 900) =>
    apiRequest<CanonicalAsset>(`/api/projects/${projectId}/canonical-assets/${assetId}`, {
      session,
      query: includePreviewUrl ? { includePreviewUrl: true, previewExpiresSeconds } : undefined,
    }),
  updateCanonicalAsset: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<CanonicalAsset>(`/api/projects/${projectId}/canonical-assets/${assetId}`, { method: "PATCH", session, body }),
  getCanonicalAssetImpact: (session: StudioSession, projectId: string, assetId: string) =>
    apiRequest<OutputImpact>(`/api/projects/${projectId}/canonical-assets/${assetId}/impact`, { session }),
  deleteCanonicalAsset: (session: StudioSession, projectId: string, assetId: string, expectedRevision: number) =>
    apiRequest<{ deleted: boolean; mode?: string; assetId: string }>(`/api/projects/${projectId}/canonical-assets/${assetId}`, {
      method: "DELETE",
      session,
      body: { expectedRevision },
    }),
  generateAssetCard: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<GenerateAssetCardResponse>(`/api/projects/${projectId}/canonical-assets/${assetId}/generate-card`, { method: "POST", session, body }),
  listAssetReferences: (session: StudioSession, projectId: string, assetId: string, includePreviewUrl = false, previewExpiresSeconds = 900) =>
    apiRequest<ListEnvelope<AssetReference>>(`/api/projects/${projectId}/canonical-assets/${assetId}/references`, {
      session,
      query: includePreviewUrl ? { includePreviewUrl: true, previewExpiresSeconds } : undefined,
    }),
  createAssetReferenceUploadUrl: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<{ storageKey: string; uploadUrl: string; method: string; headers: Record<string, string | string[]>; expiresAt: string }>(`/api/projects/${projectId}/canonical-assets/${assetId}/references/upload-url`, { method: "POST", session, body }),
  createAssetReference: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<AssetReference>(`/api/projects/${projectId}/canonical-assets/${assetId}/references`, { method: "POST", session, body }),
  setPrimaryAssetReference: (session: StudioSession, projectId: string, assetId: string, referenceId: string) =>
    apiRequest<{ assetId: string; reference: AssetReference }>(`/api/projects/${projectId}/canonical-assets/${assetId}/references/${referenceId}/set-primary`, { method: "POST", session, body: {} }),
  deleteAssetReference: (session: StudioSession, projectId: string, assetId: string, referenceId: string) =>
    apiRequest<{ deleted: boolean; mode: string; referenceId: string; artifactDeleted: boolean; mediaDeleted: boolean }>(
      `/api/projects/${projectId}/canonical-assets/${assetId}/references/${referenceId}`,
      { method: "DELETE", session },
    ),
  analyzeScriptAssets: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/scripts/${scriptId}/analyze-assets`, { method: "POST", session, body }),
  generateAssetImage: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord = {}) =>
    apiRequest<{ asset: CanonicalAsset; providerCallId: string }>(`/api/projects/${projectId}/canonical-assets/${assetId}/generate-image`, { method: "POST", session, body }),
  reviewAsset: (session: StudioSession, projectId: string, assetId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/assets/${assetId}/review`, { method: "POST", session, body }),
  listShotAssetRequirements: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ShotAssetRequirement>>(`/api/projects/${projectId}/shot-asset-requirements`, { session }),
  generateDerivedAssetImage: (session: StudioSession, projectId: string, requirementId: string) =>
    apiRequest<DerivedAssetBatchCommandResult>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/generate-image`, { method: "POST", session, body: {} }),
  reviewShotAssetRequirement: (session: StudioSession, projectId: string, requirementId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/review`, { method: "POST", session, body }),
  batchReviewShotAssetRequirements: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<BatchReviewShotAssetRequirementsResponse>(`/api/projects/${projectId}/shot-asset-requirements/review-batch`, { method: "POST", session, body }),
  updateShotAssetRequirement: (session: StudioSession, projectId: string, requirementId: string, body: JsonRecord) =>
    apiRequest<ShotAssetRequirement>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}`, { method: "PATCH", session, body }),
  skipShotAssetRequirement: (session: StudioSession, projectId: string, requirementId: string) =>
    apiRequest<ShotAssetRequirement>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/skip`, { method: "POST", session, body: {} }),

  generateStoryboard: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/scripts/${scriptId}/generate-storyboard`, { method: "POST", session, body }),
  analyzeScriptEpisodeTiming: (session: StudioSession, projectId: string, episodeId: string, body: JsonRecord = {}) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/script-episodes/${episodeId}/timing/analyze`, { method: "POST", session, body }),
  getScriptEpisodeTiming: (session: StudioSession, projectId: string, episodeId: string) =>
    apiRequest<ScriptTimingAnalysis>(`/api/projects/${projectId}/script-episodes/${episodeId}/timing`, { session }),
  listStoryboardPlans: (session: StudioSession, projectId: string, episodeId: string) =>
    apiRequest<ListEnvelope<StoryboardPlan>>(`/api/projects/${projectId}/script-episodes/${episodeId}/storyboard-plans`, { session }),
  getStoryboardPlan: (session: StudioSession, projectId: string, planId: string) =>
    apiRequest<StoryboardPlan>(`/api/projects/${projectId}/storyboard-plans/${planId}`, { session }),
  activateStoryboardPlan: (session: StudioSession, projectId: string, planId: string) =>
    apiRequest<StoryboardPlan>(`/api/projects/${projectId}/storyboard-plans/${planId}/activate`, { method: "POST", session, body: {} }),
  splitStoryboardShot: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord) =>
    apiRequest<StoryboardPlanEditResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/split`, { method: "POST", session, body }),
  mergeStoryboardShots: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<StoryboardPlanEditResponse>(`/api/projects/${projectId}/storyboard-shots/merge`, { method: "POST", session, body }),
  updateStoryboardShotTiming: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord) =>
    apiRequest<StoryboardPlanEditResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/timing`, { method: "PATCH", session, body }),
  createWorkflowRun: (session: StudioSession, body: JsonRecord) => apiRequest<WorkflowRun>("/api/workflow-runs", { method: "POST", session, body }),
  createAssetBatch: (session: StudioSession, projectId: string, body: CreateAssetBatchRequest) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/asset-batches`, { method: "POST", session, body }),
  listWorkflowRuns: (session: StudioSession, projectId?: string) =>
    apiRequest<ListEnvelope<WorkflowRun>>("/api/workflow-runs", { session, query: projectId ? { "filter[projectId]": projectId } : undefined }),
  cancelWorkflowRun: (session: StudioSession, workflowRunId: string, reason: string) =>
    apiRequest<WorkflowRun>(`/api/workflow-runs/${workflowRunId}/cancel`, { method: "POST", session, body: { reason } }),
  retryFailedWorkflowRun: (session: StudioSession, workflowRunId: string, body: RetryFailedWorkflowRequest) =>
    apiRequest<WorkflowRun>(`/api/workflow-runs/${workflowRunId}/retry-failed`, { method: "POST", session, body }),
  getRuntimeOperation: (session: StudioSession, projectId: string, operationId: string) =>
    apiRequest<RuntimeOperation>(`/api/projects/${projectId}/operations/${operationId}`, { session }),
  reconcileRuntimeOperation: (session: StudioSession, projectId: string, operationId: string) =>
    apiRequest<RuntimeOperation>(`/api/projects/${projectId}/operations/${operationId}/reconcile`, { method: "POST", session, body: {} }),
  listWorkflowNodes: (session: StudioSession, workflowRunId: string) =>
    apiRequest<ListEnvelope<WorkflowNodeRun>>(`/api/workflow-runs/${workflowRunId}/nodes`, { session }),
  getWorkflowVideoProductionActivity: (session: StudioSession, workflowRunId: string) =>
    apiRequest<WorkflowVideoProductionActivity>(`/api/workflow-runs/${workflowRunId}/video-production`, { session }),
  getWorkflowDerivedAssetBatch: (session: StudioSession, workflowRunId: string) =>
    apiRequest<DerivedAssetBatchProjection>(`/api/workflow-runs/${workflowRunId}/derived-asset-batch`, { session }),
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
      query: { previewExpiresSeconds: 3600 },
    }),
  getStoryboardShotState: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<StoryboardShotStateResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/state`, { session }),
  replanStoryboardShotState: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<RegenerateResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/state/replan`, { method: "POST", session, body: {} }),
  getStoryboardShotTransition: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<StoryboardShotTransitionResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/transition`, { session }),
  updateStoryboardShotTransition: (
    session: StudioSession,
    projectId: string,
    shotId: string,
    body: { expectedRevision: number; transitionType: StoryboardShotTransition["transitionType"]; confidence?: number; reason?: string },
  ) => apiRequest<StoryboardShotTransition>(`/api/projects/${projectId}/storyboard-shots/${shotId}/transition`, { method: "PATCH", session, body }),
  listStoryboardShotAnchors: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<ShotVisualAnchorResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/anchors`, { session }),
  generateStoryboardShotAnchor: (session: StudioSession, projectId: string, shotId: string, anchorRole?: string) =>
    apiRequest<{ anchorId: string; anchorRole: string; workflowRunId: string; status: string; workflowType: string }>(
      `/api/projects/${projectId}/storyboard-shots/${shotId}/anchors/generate`,
      { method: "POST", session, body: anchorRole ? { anchorRole } : {} },
    ),
  reviewStoryboardShotAnchor: (
    session: StudioSession,
    projectId: string,
    shotId: string,
    anchorId: string,
    decision: "approve" | "reject",
    body: { expectedRevision: number; reason?: string },
  ) => apiRequest<ShotVisualAnchor>(`/api/projects/${projectId}/storyboard-shots/${shotId}/anchors/${anchorId}/${decision}`, { method: "POST", session, body }),
  getStoryboardShotReferencePack: (session: StudioSession, projectId: string, shotId: string, purpose: "anchor" | "video" = "anchor") =>
    apiRequest<ShotReferencePackResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/reference-pack`, { session, query: { purpose } }),
  getStoryboardShotStoryboardSheet: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<StoryboardSheetResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/storyboard-sheet`, { session }),
  getStoryboardShotVideoPromptPlan: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<VideoPromptPlanResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/video-prompt-plan`, { session }),
  createManualVideoPromptPlanRevision: (
    session: StudioSession,
    projectId: string,
    shotId: string,
    body: { expectedRevision: number; renderedPrompt: string; reason?: string },
  ) => apiRequest<VideoPromptPlan>(`/api/projects/${projectId}/storyboard-shots/${shotId}/video-prompt-plan/revisions`, { method: "POST", session, body }),
  reviewVideoPromptPlan: (
    session: StudioSession,
    projectId: string,
    promptPlanId: string,
    decision: "approve" | "reject",
    body: { expectedRevision: number; reason?: string },
  ) => apiRequest<{ id: string; storyboardShotId: string; revision: number; status: "approved" | "rejected" }>(
    `/api/projects/${projectId}/video-prompts/${promptPlanId}/${decision}`,
    { method: "POST", session, body },
  ),
  getStoryboardShotRenderPlan: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<VideoRenderPlan>(`/api/projects/${projectId}/storyboard-shots/${shotId}/video-render-plan`, { session }),
  createStoryboardShotRenderPlan: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord = {}) =>
    apiRequest<VideoRenderPlan>(`/api/projects/${projectId}/storyboard-shots/${shotId}/render-plan`, { method: "POST", session, body }),
  verifyStoryboardShotRenderPlanAudio: (session: StudioSession, projectId: string, shotId: string, body: { decision: "approve" | "reject"; notes?: string }) =>
    apiRequest<VideoRenderPlan>(`/api/projects/${projectId}/storyboard-shots/${shotId}/render-plan/audio-verification`, { method: "POST", session, body }),
  startNativeAudioReview: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord = {}) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/storyboard-shots/${shotId}/render-plan/audio-review`, { method: "POST", session, body }),
  listNativeAudioReviews: (session: StudioSession, projectId: string, shotId: string) =>
    apiRequest<ListEnvelope<NativeAudioReview>>(`/api/projects/${projectId}/storyboard-shots/${shotId}/render-plan/audio-reviews`, { session }),
  reviewStoryboardShot: (session: StudioSession, projectId: string, shotId: string, body: JsonRecord) =>
    apiRequest<ReviewResponse>(`/api/projects/${projectId}/storyboard-shots/${shotId}/review`, { method: "POST", session, body }),
  updateStoryboardShot: (session: StudioSession, projectId: string, shotId: string, body: UpdateStoryboardShotRequest) =>
    apiRequest<StoryboardShot>(`/api/projects/${projectId}/storyboard-shots/${shotId}`, { method: "PATCH", session, body }),
  unlinkStoryboardShotMedia: (session: StudioSession, projectId: string, shotId: string, kind: "image" | "video") =>
    apiRequest<StoryboardShot>(`/api/projects/${projectId}/storyboard-shots/${shotId}/media/unlink`, { method: "POST", session, body: { kind } }),

  listArtifacts: (session: StudioSession, projectId?: string) =>
    apiRequest<ListEnvelope<Artifact>>("/api/artifacts", {
      session,
      query: { "filter[projectId]": projectId, includePreviewUrl: true, previewExpiresSeconds: 900 },
    }),

  listProviderAccounts: (session: StudioSession, status?: ProviderListStatus) =>
    apiRequest<ListEnvelope<ProviderAccount>>("/api/providers/accounts", {
      session,
      query: status ? { "filter[status]": status } : undefined,
    }),
  listCharacterVoices: (session: StudioSession, projectId: string, status?: ArchiveListStatus) =>
    apiRequest<ListEnvelope<CharacterVoiceProfile>>(`/api/projects/${projectId}/character-voices`, {
      session,
      query: status ? { "filter[status]": status } : undefined,
    }),
  createCharacterVoice: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<CharacterVoiceProfile>(`/api/projects/${projectId}/character-voices`, { method: "POST", session, body }),
  updateCharacterVoice: (session: StudioSession, projectId: string, voiceId: string, body: JsonRecord) =>
    apiRequest<CharacterVoiceProfile>(`/api/projects/${projectId}/character-voices/${voiceId}`, { method: "PATCH", session, body }),
  deleteCharacterVoice: (session: StudioSession, projectId: string, voiceId: string) =>
    apiRequest<void>(`/api/projects/${projectId}/character-voices/${voiceId}`, { method: "DELETE", session }),
  listProviderConnectors: (session: StudioSession) => apiRequest<ListEnvelope<ProviderConnector>>("/api/providers/connectors", { session }),
  importProviderConnector: (session: StudioSession, body: JsonRecord) =>
    apiRequest<ProviderConnector>("/api/providers/connectors/import", { method: "POST", session, body }),
  createProviderAccount: (session: StudioSession, body: JsonRecord) => apiRequest<ProviderAccount>("/api/providers/accounts", { method: "POST", session, body }),
  getProviderAccount: (session: StudioSession, accountId: string) => apiRequest<ProviderAccount>(`/api/providers/accounts/${accountId}`, { session }),
  updateProviderAccount: (session: StudioSession, accountId: string, body: JsonRecord) =>
    apiRequest<ProviderAccount>(`/api/providers/accounts/${accountId}`, { method: "PATCH", session, body }),
  deleteProviderAccount: (session: StudioSession, accountId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/providers/accounts/${accountId}`, { method: "DELETE", session }),
  listProviderCredentials: (session: StudioSession, accountId: string, status: ProviderCredentialStatus = "active") =>
    apiRequest<ListEnvelope<ProviderCredential>>(`/api/providers/accounts/${accountId}/credentials`, {
      session,
      query: { "filter[status]": status },
    }),
  createProviderCredential: (session: StudioSession, accountId: string, body: JsonRecord) =>
    apiRequest<ProviderCredential>(`/api/providers/accounts/${accountId}/credentials`, { method: "POST", session, body }),
  rotateProviderCredential: (session: StudioSession, accountId: string, body: JsonRecord) =>
    apiRequest<ProviderAccount>(`/api/providers/accounts/${accountId}/credentials/rotate`, { method: "POST", session, body }),
  rotateProviderCredentialById: (session: StudioSession, accountId: string, credentialId: string, body: JsonRecord) =>
    apiRequest<ProviderCredential>(`/api/providers/accounts/${accountId}/credentials/${credentialId}/rotate`, { method: "POST", session, body }),
  revokeProviderCredential: (session: StudioSession, accountId: string, credentialId: string) =>
    apiRequest<{ revoked: boolean }>(`/api/providers/accounts/${accountId}/credentials/${credentialId}`, { method: "DELETE", session }),
  discoverProviderCredentialModels: (session: StudioSession, accountId: string, credentialId: string) =>
    apiRequest<ProviderModelDiscoveryResult>(`/api/providers/accounts/${accountId}/credentials/${credentialId}/discover-models`, {
      method: "POST",
      session,
    }),
  discoverProviderModels: (session: StudioSession, accountId: string, body: JsonRecord = {}) =>
    apiRequest<ProviderModelDiscoveryResult>(`/api/providers/accounts/${accountId}/discover-models`, { method: "POST", session, body }),
  listProviderCatalog: (session: StudioSession) => apiRequest<ListEnvelope<ProviderCatalogEntry>>("/api/provider-catalog", { session }),
  getProviderCatalogEntry: (session: StudioSession, providerKey: string) => apiRequest<ProviderCatalogEntry>(`/api/provider-catalog/${providerKey}`, { session }),
  installProviderCatalogEntry: (session: StudioSession, providerKey: string, body: JsonRecord) =>
    apiRequest<ProviderCatalogInstallResponse>(`/api/provider-catalog/${providerKey}/install`, { method: "POST", session, body }),
  listProviderModels: (session: StudioSession, accountId: string, status?: ProviderListStatus) =>
    apiRequest<ListEnvelope<ProviderModel>>(`/api/providers/accounts/${accountId}/models`, {
      session,
      query: status ? { "filter[status]": status } : undefined,
    }),
  createProviderModel: (session: StudioSession, accountId: string, body: JsonRecord) =>
    apiRequest<ProviderModel>(`/api/providers/accounts/${accountId}/models`, { method: "POST", session, body }),
  updateProviderModel: (session: StudioSession, modelId: string, body: JsonRecord) =>
    apiRequest<ProviderModel>(`/api/providers/models/${modelId}`, { method: "PATCH", session, body }),
  deleteProviderModel: (session: StudioSession, modelId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/providers/models/${modelId}`, { method: "DELETE", session }),
  testProviderModel: (session: StudioSession, modelId: string, body: JsonRecord) =>
    apiRequest<ProviderTestResult>(`/api/providers/models/${modelId}/test`, { method: "POST", session, body }),
  listProviderModelVideoCapabilities: (session: StudioSession, modelId: string) =>
    apiRequest<VideoCapabilityAttestationList>(`/api/providers/models/${modelId}/video-capability-attestations`, { session }),
  attestProviderModelVideoCapability: (session: StudioSession, modelId: string, body: JsonRecord) =>
    apiRequest<VideoCapabilityAttestation>(`/api/providers/models/${modelId}/video-capability-attestations`, { method: "POST", session, body }),
  revokeProviderModelVideoCapability: (session: StudioSession, modelId: string, attestationId: string, body: JsonRecord) =>
    apiRequest<VideoCapabilityAttestation>(`/api/providers/models/${modelId}/video-capability-attestations/${attestationId}/revoke`, { method: "POST", session, body }),
  verifyProviderModelVideoCapability: (session: StudioSession, modelId: string, body: JsonRecord) =>
    apiRequest<VideoCapabilityAttestation>(`/api/providers/models/${modelId}/video-capabilities/verify`, { method: "POST", session, body }),
  validateProviderManifest: (session: StudioSession, body: JsonRecord) =>
    apiRequest<ProviderManifestValidationResult>("/api/providers/manifests/validate", { method: "POST", session, body }),
  runProviderManifestTest: (session: StudioSession, body: JsonRecord) =>
    apiRequest<ProviderManifestTestRunResult>("/api/providers/manifests/test-run", { method: "POST", session, body }),
  listModelProfiles: (session: StudioSession) => apiRequest<ListEnvelope<ModelProfile>>("/api/model-profiles", { session }),
  createModelProfile: (session: StudioSession, body: JsonRecord) => apiRequest<ModelProfile>("/api/model-profiles", { method: "POST", session, body }),
  updateModelProfile: (session: StudioSession, profileId: string, body: JsonRecord) =>
    apiRequest<ModelProfile>(`/api/model-profiles/${profileId}`, { method: "PATCH", session, body }),
  createModelProfileBinding: (session: StudioSession, profileId: string, body: CreateModelProfileBindingRequest) =>
    apiRequest<ModelProfile>(`/api/model-profiles/${profileId}/bindings`, { method: "POST", session, body }),
  updateModelProfileBinding: (session: StudioSession, profileId: string, bindingId: string, body: UpdateModelProfileBindingRequest) =>
    apiRequest<ModelProfile>(`/api/model-profiles/${profileId}/bindings/${bindingId}`, { method: "PATCH", session, body }),
  deleteModelProfileBinding: (session: StudioSession, profileId: string, bindingId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/model-profiles/${profileId}/bindings/${bindingId}`, { method: "DELETE", session }),
  listProviderCallLogs: (session: StudioSession, query?: Record<string, string | number | boolean | undefined | null>) =>
    apiRequest<ListEnvelope<ProviderCallLog>>("/api/provider-call-logs", { session, query }),
  getProviderUsageSummary: (session: StudioSession) => apiRequest<ProviderUsageSummary>("/api/provider-usage/summary", { session }),
  listProviderLimitPolicies: (session: StudioSession) => apiRequest<ListEnvelope<ProviderLimitPolicy>>("/api/provider-limit-policies", { session }),
  createProviderLimitPolicy: (session: StudioSession, body: JsonRecord) =>
    apiRequest<ProviderLimitPolicy>("/api/provider-limit-policies", { method: "POST", session, body }),
  getProviderLimitPolicy: (session: StudioSession, policyId: string) => apiRequest<ProviderLimitPolicy>(`/api/provider-limit-policies/${policyId}`, { session }),
  updateProviderLimitPolicy: (session: StudioSession, policyId: string, body: JsonRecord) =>
    apiRequest<ProviderLimitPolicy>(`/api/provider-limit-policies/${policyId}`, { method: "PATCH", session, body }),
  deleteProviderLimitPolicy: (session: StudioSession, policyId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/provider-limit-policies/${policyId}`, { method: "DELETE", session }),
  listProviderCircuitStates: (session: StudioSession) => apiRequest<ListEnvelope<ProviderCircuitState>>("/api/provider-circuit-states", { session }),
  resetProviderCircuitState: (session: StudioSession, stateId: string) =>
    apiRequest<ProviderCircuitState>(`/api/provider-circuit-states/${stateId}/reset`, { method: "POST", session }),
  listPromptTemplates: (session: StudioSession) => apiRequest<ListEnvelope<PromptTemplate>>("/api/prompt-templates", { session }),
  createPromptTemplate: (session: StudioSession, body: JsonRecord) => apiRequest<PromptTemplate>("/api/prompt-templates", { method: "POST", session, body }),
  createPromptVersion: (session: StudioSession, templateId: string, body: JsonRecord) =>
    apiRequest<{ id: string }>(`/api/prompt-templates/${templateId}/versions`, { method: "POST", session, body }),
};

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}

function resolveApiUrl(path: string) {
  if (typeof window !== "undefined") {
    const sameOriginUrl = new URL(path, window.location.origin);
    if (!configuredApiBase) {
      return sameOriginUrl;
    }
    const configuredUrl = new URL(`${configuredApiBase}${path}`);
    if (!isLoopbackHost(window.location.hostname) && isLoopbackHost(configuredUrl.hostname)) {
      return sameOriginUrl;
    }
    return configuredUrl;
  }
  if (!configuredApiBase) {
    throw new StudioApiError(
      "服务端 API 地址未配置",
      "API_BASE_URL_NOT_CONFIGURED",
      0,
      false,
    );
  }
  return new URL(`${configuredApiBase}${path}`);
}

function isLoopbackHost(hostname: string) {
  const normalized = hostname.trim().toLowerCase().replace(/^\[|\]$/g, "");
  return normalized === "localhost" || normalized === "127.0.0.1" || normalized === "::1";
}
