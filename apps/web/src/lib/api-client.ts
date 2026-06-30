import type {
  AgentMessage,
  AgentSession,
  ApiEnvelope,
  Artifact,
  CanonicalAsset,
  JsonRecord,
  ListEnvelope,
  ModelProfile,
  Organization,
  Permission,
  Project,
  ProjectSource,
  PromptTemplate,
  ProviderAccount,
  Role,
  Script,
  ScriptVersion,
  ShotAssetRequirement,
  StoryboardShot,
  StudioSession,
  Team,
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
  if (options.body !== undefined) {
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
  const response = await fetch(url, {
    method: options.method ?? (options.body === undefined ? "GET" : "POST"),
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
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

  listSources: (session: StudioSession, projectId: string) => apiRequest<ListEnvelope<ProjectSource>>(`/api/projects/${projectId}/sources`, { session }),
  createSource: (session: StudioSession, projectId: string, body: JsonRecord) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources`, { method: "POST", session, body }),
  updateSource: (session: StudioSession, projectId: string, sourceId: string, body: JsonRecord) =>
    apiRequest<ProjectSource>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "PATCH", session, body }),
  deleteSource: (session: StudioSession, projectId: string, sourceId: string) =>
    apiRequest<{ deleted: boolean }>(`/api/projects/${projectId}/sources/${sourceId}`, { method: "DELETE", session }),

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
  analyzeScriptAssets: (session: StudioSession, projectId: string, scriptId: string, body: JsonRecord) =>
    apiRequest<WorkflowRun>(`/api/projects/${projectId}/scripts/${scriptId}/analyze-assets`, { method: "POST", session, body }),
  generateAssetImage: (session: StudioSession, projectId: string, assetId: string) =>
    apiRequest<{ asset: CanonicalAsset; providerCallId: string }>(`/api/projects/${projectId}/assets/${assetId}/generate-image`, { method: "POST", session, body: {} }),
  listShotAssetRequirements: (session: StudioSession, projectId: string) =>
    apiRequest<ListEnvelope<ShotAssetRequirement>>(`/api/projects/${projectId}/shot-asset-requirements`, { session }),
  generateDerivedAssetImage: (session: StudioSession, projectId: string, requirementId: string) =>
    apiRequest<{ requirement: ShotAssetRequirement; providerCallId: string }>(`/api/projects/${projectId}/shot-asset-requirements/${requirementId}/generate-image`, { method: "POST", session, body: {} }),

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
