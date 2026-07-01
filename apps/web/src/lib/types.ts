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

export type AuthUser = {
  id: string;
  email: string;
  displayName?: string;
};

export type StudioSession = {
  accessToken: string;
  refreshToken: string;
  organizationId: string;
  workspaceId?: string;
  user?: AuthUser;
  currentProjectId: string;
};

export type AuthResponse = {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  organizationId: string;
  workspaceId?: string;
  user: AuthUser;
};

export type SetupState = {
  needsSetup: boolean;
  userCount: number;
  organizationCount: number;
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
  chapters?: NovelChapter[];
  createdAt?: string;
  updatedAt?: string;
};

export type NovelChapter = {
  id: string;
  sourceId: string;
  chapterIndex: number;
  volumeTitle?: string;
  chapterTitle?: string;
  content: string;
  eventState: string;
  eventSummary?: JsonValue;
  errorMessage?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type NovelChapterSummary = {
  id: string;
  chapterIndex: number;
  volumeTitle?: string;
  chapterTitle?: string;
  contentLength: number;
};

export type NovelEvent = {
  id: string;
  projectId: string;
  sourceId: string;
  chapterId?: string;
  chapterIndex?: number;
  eventIndex: number;
  sequenceNo: number;
  title: string;
  summary: string;
  eventType?: string;
  importance: number;
  timelineHint?: string;
  locationHint?: string;
  emotionalTone?: string;
  conflict?: string;
  outcome?: string;
  adaptationHint?: string;
  characters: string[];
  scenes: string[];
  props: string[];
  keywords: string[];
  rawExcerpt?: string;
  reviewStatus: string;
  manualOverride: boolean;
  staleState: string;
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type NovelEventLink = {
  id: string;
  projectId: string;
  sourceEventId: string;
  targetEventId: string;
  linkType: string;
  description?: string;
  metadata?: JsonRecord;
  createdAt?: string;
};

export type AdaptationPlan = {
  id: string;
  projectId: string;
  sourceId?: string;
  scriptId?: string;
  title: string;
  status: string;
  targetFormat: string;
  targetDurationSeconds?: number;
  maxShots?: number;
  selectedEventIds: string[];
  structure: JsonRecord;
  content: string;
  reviewStatus: string;
  manualOverride: boolean;
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type CreatedScriptSummary = {
  id: string;
  currentVersionId: string;
  title: string;
};

export type ImportProjectSourceResponse = {
  source: ProjectSource;
  chapters: NovelChapterSummary[];
  script?: CreatedScriptSummary;
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

export type ScriptScene = {
  id: string;
  projectId: string;
  scriptId: string;
  scriptVersionId: string;
  sceneIndex: number;
  sceneNo: number;
  title: string;
  summary?: string;
  location?: string;
  timeOfDay?: string;
  atmosphere?: string;
  characters?: string[];
  scenes?: string[];
  props?: string[];
  action?: string;
  dialogue?: string;
  visualGoal?: string;
  emotionalTone?: string;
  conflict?: string;
  outcome?: string;
  sourceEventIds?: string[];
  content: string;
  contentFormat: string;
  reviewStatus: string;
  manualOverride?: boolean;
  staleState?: string;
  metadata?: JsonRecord;
  editedBy?: string;
  editedAt?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type ParseScriptScenesResponse = {
  scriptId: string;
  versionId: string;
  sceneCount: number;
  scenes: ScriptScene[];
  providerCallId?: string;
  modelId?: string;
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
  profile?: JsonRecord;
  basePrompt?: string;
  consistencyPrompt?: string;
  negativePrompt?: string;
  visualTraits?: JsonRecord;
  primaryReferenceArtifactId?: string;
  primaryReferenceMediaFileId?: string;
  primaryReferenceStorageKey?: string;
  lockReference?: boolean;
  referenceArtifactId?: string;
  referenceMediaFileId?: string;
  referenceStorageKey?: string;
  status: string;
  reviewStatus?: string;
  manualOverride?: boolean;
  staleState?: string;
  editedBy?: string;
  editedAt?: string;
  sourceScriptIds?: string[];
  createdAt?: string;
  updatedAt?: string;
  sceneLinks?: AssetSceneLink[];
  references?: AssetReference[];
  shotRequirements?: ShotAssetRequirement[];
  sceneCount?: number;
  storyboardShotCount?: number;
  referenceCount?: number;
  shotRequirementCount?: number;
};

export type AssetReference = {
  id: string;
  assetId: string;
  referenceType: string;
  title?: string;
  description?: string;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  prompt?: string;
  promptVersionId?: string;
  promptHash?: string;
  isPrimary: boolean;
  status: string;
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type GenerateAssetCardResponse = {
  assetId: string;
  profile: JsonRecord;
  basePrompt: string;
  consistencyPrompt: string;
  negativePrompt: string;
  providerCallId?: string;
  modelId?: string;
  applied: boolean;
};

export type AssetSceneLink = {
  scriptSceneId: string;
  sceneNo: number;
  title: string;
  location?: string;
  assetRole?: string;
  usageNote?: string;
  storyboardShotCount: number;
};

export type ShotAssetRequirement = {
  id: string;
  organizationId?: string;
  projectId?: string;
  workflowRunId?: string;
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
  manualOverride?: boolean;
  staleState?: string;
  editedBy?: string;
  editedAt?: string;
  createdAt?: string;
  updatedAt?: string;
  metadata?: JsonRecord;
  asset?: CanonicalAsset;
};

export type StoryboardShotRequirementDetail = ShotAssetRequirement & {
  derivedPreviewUrl?: string;
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
  scriptSceneId?: string;
  sourceScene?: {
    id: string;
    sceneNo: number;
    title: string;
    location?: string;
    characters?: string[];
  };
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
  manualOverride?: boolean;
  staleState?: string;
  editedBy?: string;
  editedAt?: string;
};

export type StoryboardShotDetail = {
  shot: StoryboardShot;
  scriptScene?: StoryboardShot["sourceScene"];
  requirements: StoryboardShotRequirementDetail[];
  imageArtifact?: Artifact;
  imagePreviewUrl?: string;
  videoArtifact?: Artifact;
  videoPreviewUrl?: string;
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
      chapterCount: number;
      eventCount: number;
      approvedEventCount: number;
      pendingEventReviewCount: number;
      adaptationPlanCount: number;
      activeAdaptationPlanId?: string | null;
      activeAdaptationTitle?: string | null;
      activeAdaptationStatus?: string | null;
      activeScriptId?: string | null;
      activeScriptTitle?: string | null;
      scriptSceneCount?: number;
      approvedScriptSceneCount?: number;
      pendingScriptSceneCount?: number;
      staleScriptSceneCount?: number;
      summary: string[];
    };
    assets: {
      status: string;
      characterCount: number;
      sceneCount: number;
      propCount: number;
      assetCardCount: number;
      missingAssetCardCount: number;
      referenceImageCount: number;
      missingReferenceImageCount: number;
      primaryReferenceCount: number;
      missingPrimaryReferenceCount: number;
      lockedReferenceCount: number;
      approvedCount: number;
      pendingReviewCount: number;
      manualOverrideCount: number;
      staleCount: number;
      downstreamStaleCount: number;
      summary: Record<string, string[]>;
    };
    storyboard: {
      status: string;
      shotCount: number;
      confirmedShotCount: number;
      pendingReviewCount: number;
      manualOverrideCount: number;
      staleShotCount: number;
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
      manualOverrideCount: number;
      staleRequirementCount: number;
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
      workflowRunId?: string | null;
      sourceWorkflowRunId?: string | null;
      stale?: boolean;
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
  stale: number;
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

export type RegenerateResponse = {
  targetType: string;
  targetId: string;
  workflowRunId: string;
  status: string;
  workflowType: string;
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
