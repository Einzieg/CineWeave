export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
export type JsonRecord = { [key: string]: JsonValue };

export type ListEnvelope<TItem> = {
  items: TItem[];
};

export type ApiErrorBody = {
  code: string;
  message: string;
  retryable?: boolean;
};

export type ApiEnvelope<TData> = {
  requestId?: string;
  data?: TData;
  error?: ApiErrorBody;
};

export type StudioSession = {
  accessToken: string;
  currentUserId: string;
  organizationId: string;
  workspaceId: string;
  currentProjectId: string;
};

export type Project = {
  id: string;
  organizationId: string;
  workspaceId?: string;
  name: string;
  description?: string | null;
  projectType?: string | null;
  contentType?: string | null;
  aspectRatio?: string | null;
  videoRatio?: string;
  artStyle?: string;
  directorManual?: string;
  visualManual?: string;
  imageModelProfileKey?: string;
  videoModelProfileKey?: string;
  scriptModelProfileKey?: string;
  imageQuality?: string;
  productionMode?: string;
  status?: string;
  settings?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type ProjectSource = {
  id: string;
  organizationId: string;
  projectId: string;
  sourceType: "novel" | "script" | string;
  title: string;
  content: string;
  contentFormat: "plain_text" | "markdown" | string;
  originalFileName?: string;
  storageKey?: string;
  status: string;
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type ScriptVersion = {
  id: string;
  scriptId: string;
  version: number;
  content: string;
  contentFormat: string;
  sourceType?: string;
  promptVersionId?: string;
  promptHash?: string;
  createdAt?: string;
};

export type Script = {
  id: string;
  organizationId: string;
  projectId: string;
  sourceId?: string;
  title: string;
  status: string;
  currentVersionId?: string;
  currentVersion?: ScriptVersion;
  createdAt?: string;
  updatedAt?: string;
};

export type AgentSession = {
  id: string;
  projectId: string;
  agentType: string;
  title?: string;
  status: string;
  createdAt?: string;
  updatedAt?: string;
};

export type AgentMessage = {
  id: string;
  sessionId: string;
  role: "user" | "assistant" | "system" | "tool" | string;
  content: string;
  createdAt?: string;
};

export type CanonicalAsset = {
  id: string;
  projectId: string;
  assetType: "character" | "scene" | "prop" | string;
  name: string;
  description: string;
  basePrompt?: string;
  visualTraits?: JsonRecord;
  referenceArtifactId?: string;
  referenceMediaFileId?: string;
  referenceStorageKey?: string;
  status: string;
  reviewStatus?: string;
  sourceScriptIds?: string[];
  createdAt?: string;
  updatedAt?: string;
};

export type ShotAssetRequirement = {
  id: string;
  storyboardShotId: string;
  assetId: string;
  assetType?: string;
  assetName?: string;
  requirementType: string;
  roleInShot?: string;
  costume?: string;
  pose?: string;
  expression?: string;
  action?: string;
  cameraRelation?: string;
  sceneState?: string;
  propState?: string;
  prompt?: string;
  derivedArtifactId?: string;
  derivedMediaFileId?: string;
  derivedStorageKey?: string;
  status: string;
  reviewStatus?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type WorkflowRun = {
  id: string;
  organizationId: string;
  projectId: string;
  temporalWorkflowId: string;
  status: string;
  input: JsonRecord;
  output: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  createdAt?: string;
  startedAt?: string;
  completedAt?: string;
  cancelledAt?: string;
};

export type WorkflowNodeRun = {
  id: string;
  workflowRunId: string;
  nodeKey: string;
  nodeType: string;
  status: string;
  input: JsonRecord;
  output: JsonRecord;
  retryCount?: number;
  errorCode?: string;
  errorMessage?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt?: string;
};

export type StoryboardShot = {
  id: string;
  workflowRunId: string;
  shotIndex: number;
  shotNo: number;
  durationSeconds?: number;
  visual?: string;
  camera?: string;
  motion?: string;
  mood?: string;
  imagePrompt?: string;
  videoPrompt?: string;
  imageArtifactId?: string;
  imageMediaFileId?: string;
  imageStorageKey?: string;
  imagePreviewUrl?: string;
  videoArtifactId?: string;
  videoMediaFileId?: string;
  videoStorageKey?: string;
  videoPreviewUrl?: string;
  providerAsyncTaskId?: string;
  externalTaskId?: string;
  status: string;
  reviewStatus?: string;
};

export type ProductionStatus = {
  projectId: string;
  project: {
    name: string;
    projectType: string;
    contentType: string;
    videoRatio: string;
    artStyle: string;
  };
  overall: {
    stage: string;
    progress: number;
    status: string;
  };
  stages: {
    source: {
      status: string;
      novelSourceCount: number;
      scriptSourceCount: number;
      activeScriptId?: string | null;
      activeScriptTitle?: string | null;
      summary: string[];
    };
    assets: {
      status: string;
      characterCount: number;
      sceneCount: number;
      propCount: number;
      referenceImageCount: number;
      missingReferenceImageCount: number;
      approvedCount: number;
      pendingReviewCount: number;
      summary: Record<string, string[]>;
    };
    storyboard: {
      status: string;
      shotCount: number;
      confirmedShotCount: number;
      pendingReviewCount: number;
      summary: string[];
    };
    shotAssets: {
      status: string;
      requirementCount: number;
      characterRequirementCount: number;
      sceneRequirementCount: number;
      propRequirementCount: number;
      derivedImageCount: number;
      missingDerivedImageCount: number;
      approvedCount: number;
      pendingReviewCount: number;
      summary: string[];
    };
    shotImages: ProductionShotMediaStage;
    shotVideos: ProductionShotMediaStage;
    finalVideo: {
      status: string;
      artifactId?: string | null;
      mediaFileId?: string | null;
      previewUrl?: string | null;
      storageKey?: string | null;
    };
  };
};

export type ProductionShotMediaStage = {
  status: string;
  total: number;
  succeeded: number;
  failed: number;
  running: number;
  pending: number;
};

export type ProductionActionResponse = {
  action: string;
  workflowRunId: string;
  status: string;
  workflowType: string;
  note?: string;
};

export type ReviewResponse = {
  id: string;
  reviewStatus: string;
  note?: string;
  updatedAt: string;
};

export type Artifact = {
  id: string;
  organizationId: string;
  projectId?: string;
  workflowRunId?: string;
  nodeRunId?: string;
  type: string;
  storageKey?: string;
  mimeType?: string;
  metadata?: JsonRecord;
  createdAt?: string;
  previewUrl?: string;
  previewExpiresAt?: string;
};

export type ProviderAccount = {
  id: string;
  displayName?: string;
  name?: string;
  providerType?: string;
  status: string;
  createdAt?: string;
  updatedAt?: string;
};

export type ModelProfile = {
  id: string;
  profileKey: string;
  purpose?: string;
  status?: string;
  bindings?: { id: string; enabled: boolean; providerModelId?: string }[];
};

export type PromptTemplate = {
  id: string;
  templateKey: string;
  name: string;
  purpose?: string;
  modality?: string;
  taskType?: string;
  status: string;
  activeVersionId?: string;
  updatedAt?: string;
};

export type Organization = {
  id: string;
  name: string;
  slug?: string;
  createdAt?: string;
};

export type Workspace = {
  id: string;
  organizationId: string;
  name: string;
  createdAt?: string;
};

export type Team = {
  id: string;
  name: string;
  status: string;
  createdAt?: string;
};

export type Role = {
  id: string;
  roleKey: string;
  name?: string;
  scope?: string;
};

export type Permission = {
  permissionKey: string;
  name?: string;
  description?: string;
};
