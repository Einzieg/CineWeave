export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
export type JsonRecord = { [key: string]: JsonValue };

export type ListEnvelope<TItem> = {
  items: TItem[];
};

export type ApiErrorBody = {
  code: string;
  message: string;
  retryable?: boolean;
  details?: JsonValue;
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
  ttsModelProfileKey?: string;
  asrModelProfileKey?: string;
  audioStrategy?: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement?: "preferred" | "required" | "disabled";
  audioConfigurationRevision?: number;
  imageQuality?: string;
  productionMode?: string;
  timelineTimebase?: number;
  fpsNumerator?: number;
  fpsDenominator?: number;
  activeFinalVideoVersionId?: string | null;
  activeAudioMixVersionId?: string | null;
  status?: string;
  settings?: JsonRecord;
  revision: number;
  createdAt?: string;
  updatedAt?: string;
};

export type ProjectManualBinding = {
  id: string;
  organizationId: string;
  projectId: string;
  manualKind: "director" | "visual" | string;
  promptVersionId: string;
  templateId: string;
  templateKey: string;
  templateName: string;
  version: number;
  status: string;
  contentHash: string;
  createdBy?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type ProjectSource = {
  id: string;
  organizationId: string;
  projectId: string;
  sourceType: "novel" | "script" | "brief" | string;
  title: string;
  content?: string;
  contentFormat: "plain_text" | "markdown" | string;
  originalFileName?: string;
  storageKey?: string;
  status: string;
  metadata?: JsonRecord;
  chapterCount?: number;
  firstVolumeIndex?: number;
  chapters?: NovelChapter[];
  createdAt?: string;
  updatedAt?: string;
};

export type NovelChapter = {
  id: string;
  sourceId: string;
  chapterIndex: number;
  volumeIndex?: number;
  sectionIndex?: number;
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
  sourceId: string;
  chapterIndex: number;
  volumeIndex?: number;
  sectionIndex?: number;
  volumeTitle?: string;
  chapterTitle?: string;
  contentLength: number;
  eventState: string;
  eventSummary?: JsonValue;
  errorMessage?: string;
  eventCount: number;
  approvedEventCount: number;
  pendingEventReviewCount: number;
  createdAt?: string;
  updatedAt?: string;
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

export type OutputImpactAffected = {
  entityType: string;
  count: number;
};

export type OutputImpact = {
  entityType: string;
  entityId: string;
  canDelete: boolean;
  recommendedMode: string;
  deleteModes: string[];
  affected: OutputImpactAffected[];
  warnings: string[];
};

export type ScriptVersion = {
  id: string;
  scriptId: string;
  version: number;
  content: string;
  contentFormat: string;
  status: string;
  sourceType?: string;
  promptVersionId?: string;
  promptHash?: string;
  createdAt?: string;
};

export type ScriptEpisode = {
  id: string;
  projectId: string;
  scriptId: string;
  scriptVersionId: string;
  sourceId?: string;
  sourceChapterId?: string;
  episodeIndex: number;
  volumeIndex?: number;
  sectionIndex?: number;
  volumeTitle?: string;
  episodeTitle: string;
  content: string;
  contentFormat: string;
  promptVersionId?: string;
  promptHash?: string;
  providerCallId?: string;
  reviewStatus: string;
  manualOverride: boolean;
  staleState: string;
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
};

export type CharacterVoiceProfile = {
  id: string;
  organizationId: string;
  projectId: string;
  canonicalAssetId?: string;
  characterName: string;
  displayName: string;
  language: string;
  modelProfileKey: string;
  providerModelId?: string;
  voiceKey: string;
  instructions?: string;
  referenceArtifactId?: string;
  referenceMediaFileId?: string;
  parameters: JsonRecord;
  isDefault: boolean;
  status: string;
  metadata: JsonRecord;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
};

export type TTSAudioClip = {
  id: string;
  timingAnalysisId: string;
  timingUnitId: string;
  appliedTimingAnalysisId?: string;
  voiceProfileId?: string;
  providerModelId?: string;
  providerCallId?: string;
  sourceText: string;
  speaker?: string;
  language: string;
  voiceKey: string;
  outputFormat: string;
  status: string;
  revision: number;
  audioConfigurationRevision: number;
  active: boolean;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  mimeType?: string;
  sampleRate?: number;
  sampleCount?: number;
  channelCount?: number;
  durationTicks?: number;
  timelineTimebase: number;
  durationSeconds?: number;
  errorCode?: string;
  errorMessage?: string;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

export type AudioMixVersion = {
  id: string;
  scriptEpisodeId?: string;
  storyboardPlanId?: string;
  timingAnalysisId?: string;
  workflowRunId?: string;
  revision: number;
  audioConfigurationRevision: number;
  status: string;
  active: boolean;
  audioStrategy: string;
  timelineTimebase: number;
  durationTicks?: number;
  durationSeconds?: number;
  sampleRate: number;
  channelCount: number;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  mimeType?: string;
  productionReadiness: string;
  trackSummary: JsonRecord;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

export type EpisodeAudio = {
  clips: TTSAudioClip[];
  mixes: AudioMixVersion[];
};

export type NativeAudioReview = {
  id: string;
  videoRenderPlanId: string;
  videoRenderSegmentId: string;
  workflowRunId?: string;
  providerCallId?: string;
  providerModelId?: string;
  revision: number;
  audioConfigurationRevision: number;
  status: string;
  expectedDialogue: JsonValue;
  transcript?: string;
  language?: string;
  alignment: JsonValue;
  dialogueCoverage?: number;
  textAccuracy?: number;
  timingAccuracy?: number;
  speakerTurnAccuracy?: number;
  errorCode?: string;
  errorMessage?: string;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
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
  scriptEpisodeId?: string;
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

export type StoryboardDialogueLine = {
  timingUnitId: string;
  speaker: string;
  text: string;
  delivery?: string;
  kind?: "dialogue" | "voiceover" | "narration" | "system" | string;
  spanStartTick: number;
  spanEndTick: number;
  sourceStartOffset?: number;
  sourceEndOffset?: number;
  continuesFromPrevious: boolean;
  continuesToNext: boolean;
};

export type ScriptTimingUnit = {
  id: string;
  sceneId?: string;
  ordinal: number;
  type: string;
  track: "audio" | "visual" | string;
  parallelGroup?: string;
  speaker?: string;
  sourceText?: string;
  delivery?: string;
  durationTicks: number;
  startTick: number;
  endTick: number;
  durationSource: string;
  forceBoundaryBefore?: boolean;
  forceBoundaryAfter?: boolean;
};

export type ScriptTimingBlock = {
  id: string;
  sceneId?: string;
  ordinal: number;
  parallelGroup?: string;
  startTick: number;
  endTick: number;
  durationTicks: number;
  units: ScriptTimingUnit[];
};

export type ScriptTimingScene = {
  sceneKey: string;
  scriptSceneId?: string;
  sceneOrdinal: number;
  startTick: number;
  endTick: number;
  units: ScriptTimingUnit[];
  blocks: ScriptTimingBlock[];
};

export type ScriptTimingAnalysis = {
  id: string;
  scriptId: string;
  scriptVersionId: string;
  scriptEpisodeId: string;
  revision: number;
  status: string;
  estimatedDurationTicks: number;
  minimumDurationTicks: number;
  targetDurationTicks?: number;
  dialogueDurationTicks: number;
  actionDurationTicks: number;
  pauseDurationTicks: number;
  estimatedDurationFrames: number;
  minimumDurationFrames: number;
  targetDurationFrames?: number;
  estimatedDurationSeconds: number;
  minimumDurationSeconds: number;
  targetDurationSeconds?: number;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  methodVersion: string;
  scenes: ScriptTimingScene[];
  providerCallId?: string;
  modelId?: string;
  promptVersionId?: string;
  promptHash?: string;
  metadata: JsonRecord;
  createdAt: string;
};

export type StoryboardScenePlan = {
  id: string;
  storyboardPlanId: string;
  blueprintId: string;
  scriptSceneId?: string;
  sceneKey: string;
  sceneOrdinal: number;
  dependencyGroup?: string;
  status: string;
  retryGeneration: number;
  startTick: number;
  endTick: number;
  durationTicks: number;
  shotCount: number;
  plannerInput: JsonRecord;
  plannerOutput: JsonRecord;
  reviewerOutput: JsonRecord;
  entryState: JsonRecord;
  exitState: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

export type StoryboardPlanReview = {
  id: string;
  storyboardPlanId: string;
  revision: number;
  status: string;
  approved: boolean;
  issues: JsonRecord[];
  corrections: JsonRecord[];
  deterministicReport: JsonRecord;
  providerCallId?: string;
  modelId?: string;
  errorCode?: string;
  errorMessage?: string;
  metadata: JsonRecord;
  createdAt: string;
  completedAt?: string;
};

export type StoryboardPlan = {
  id: string;
  organizationId: string;
  projectId: string;
  scriptId: string;
  scriptVersionId: string;
  scriptEpisodeId: string;
  timingAnalysisId: string;
  revision: number;
  status: string;
  pacingProfile: JsonRecord;
  targetDurationTicks: number;
  targetDurationFrames: number;
  targetDurationSeconds: number;
  estimatedShotCount: number;
  actualShotCount: number;
  sceneCount: number;
  completedSceneCount: number;
  failedSceneCount: number;
  active: boolean;
  staleState: string;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  metadata: JsonRecord;
  createdBy?: string;
  createdAt: string;
  activatedAt?: string;
  scenePlans?: StoryboardScenePlan[];
  reviews?: StoryboardPlanReview[];
  shots?: StoryboardShot[];
};

export type StoryboardPlanValidationReport = {
  storyboardPlanId: string;
  scriptEpisodeId: string;
  timingAnalysisId: string;
  shotCount: number;
  timingUnitCount: number;
  timingSpanCount: number;
  targetDurationTicks: number;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  valid: boolean;
};

export type StoryboardPlanEditResponse = {
  edit: {
    storyboardPlanId: string;
    sourceStoryboardPlanId: string;
    scriptEpisodeId: string;
    revision: number;
    shotIds: string[];
    validation: StoryboardPlanValidationReport;
  };
  plan: StoryboardPlan;
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
  metadata?: JsonRecord;
  createdAt?: string;
};

export type AgentToolRisk = "read" | "draft" | "write" | "workflow" | "costed" | "destructive" | "admin" | string;

export type AgentPermissionMode = "require_approval" | "auto_approve" | "full_access";

export type AgentToolDescriptor = {
  name: string;
  label: string;
  description: string;
  risk: AgentToolRisk;
  permission?: string;
  inputSchema: JsonRecord;
  requiresApproval: boolean;
};

export type AgentStep = {
  id: string;
  taskId: string;
  stepIndex: number;
  toolName: string;
  risk: AgentToolRisk;
  permission?: string;
  status: string;
  requiresApproval: boolean;
  input: JsonRecord;
  dryRunOutput: JsonRecord;
  supervisorDecision: JsonRecord;
  output: JsonRecord;
  verifierOutput: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
};

export type AgentApproval = {
  id: string;
  taskId: string;
  stepId?: string;
  approvalType: string;
  status: string;
  requestedPayload: JsonRecord;
  decisionPayload: JsonRecord;
  decidedBy?: string;
  decidedAt?: string;
  expiresAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type AgentTask = {
  id: string;
  organizationId: string;
  projectId: string;
  sessionId?: string;
  agentType: string;
  userGoal: string;
  mode: "plan_only" | "supervised" | "auto_low_risk" | string;
  status: string;
  temporalWorkflowId?: string;
  constraints: JsonRecord;
  plan: JsonRecord;
  summary: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  steps?: AgentStep[];
  approvals?: AgentApproval[];
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
  metadata?: JsonRecord;
  createdAt?: string;
  updatedAt?: string;
  sceneLinks?: AssetSceneLink[];
  references?: AssetReference[];
  shotRequirements?: ShotAssetRequirement[];
  sceneCount?: number;
  storyboardShotCount?: number;
  referenceCount?: number;
  shotRequirementCount?: number;
  revision: number;
  promptRevision: number;
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
  visualManualPromptVersionId?: string;
  visualManualTemplateKey?: string;
  visualStyleSlug?: string;
  assetTypeTemplateKey?: string;
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
  workflowType: string;
  status: string;
  input: JsonRecord;
  output: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  totalItems: number;
  completedItems: number;
  failedItems: number;
  revision: number;
  attemptGeneration: number;
  rootWorkflowRunId?: string;
  retryOfWorkflowRunId?: string;
  createdAt?: string;
  startedAt?: string;
  completedAt?: string;
  cancelledAt?: string;
  updatedAt: string;
};

export type RuntimeOperation = {
  id: string;
  organizationId: string;
  projectId?: string;
  operationType: string;
  status: "processing" | "succeeded" | "failed_retryable" | "failed_terminal" | "unknown_outcome";
  workflowRunId?: string;
  requestHash: string;
  hashSchemaVersion: number;
  resultSnapshot?: unknown;
  errorCode?: string;
  errorMessage?: string;
  leaseExpiresAt?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  reconcileRequired: boolean;
  retryAllowed: boolean;
};

export type WorkflowNodeRun = {
  id: string;
  organizationId: string;
  projectId: string;
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
  revision: number;
  updatedAt: string;
};

export type AssetBatchOperation = "generate_prompts" | "generate_images";

export type CreateAssetBatchRequest = {
  operation: AssetBatchOperation;
  assetIds: string[];
  maxConcurrency?: number;
  force?: boolean;
  expectedProjectRevision: number;
  idempotencyKey?: string;
};

export type RetryFailedWorkflowRequest = {
  maxConcurrency?: number;
  force?: boolean;
  expectedProjectRevision: number;
  idempotencyKey?: string;
};

export type RetryAssetBatchRequest = RetryFailedWorkflowRequest;

export type StoryboardShot = {
  id: string;
  storyboardPlanId?: string;
  workflowRunId: string;
  scriptSceneId?: string;
  scriptEpisodeId?: string;
  episodeIndex?: number;
  episodeShotIndex?: number;
  episodeTitle?: string;
  sourceScene?: {
    id: string;
    sceneNo: number;
    title: string;
    location?: string;
    characters?: string[];
  };
  shotIndex: number;
  shotNo: number;
  title?: string;
  startTick: number;
  endTick: number;
  plannedDurationTicks: number;
  durationMinTicks?: number;
  durationMaxTicks?: number;
  durationSource: string;
  timingConfidence?: number;
  durationLocked: boolean;
  shotGroupId?: string;
  continuityGroupId?: string;
  oneTake: boolean;
  timingRevision: number;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  durationSeconds?: number;
  visual?: string;
  camera?: string;
  motion?: string;
  mood?: string;
  imagePrompt?: string;
  imagePromptStatus: string;
  imagePromptErrorCode?: string;
  imagePromptErrorMessage?: string;
  imagePromptWorkflowRunId?: string;
  imagePromptUpdatedAt?: string;
  videoPrompt?: string;
  scriptDialogue: StoryboardDialogueLine[];
  videoPromptStatus: string;
  videoPromptErrorCode?: string;
  videoPromptErrorMessage?: string;
  videoPromptWorkflowRunId?: string;
  videoPromptUpdatedAt?: string;
  imageReferenceMode: "auto" | "custom" | "none" | string;
  imageReferenceKeys: string[];
  videoReferenceMode: "auto" | "custom" | "none" | string;
  videoReferenceKeys: string[];
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
  imageStatus?: string;
  videoStatus?: string;
  activeVideoRenderPlanId?: string;
  nativeAudioStatus?: string;
  productionReadiness?: string;
  imageErrorCode?: string;
  imageErrorMessage?: string;
  videoErrorCode?: string;
  videoErrorMessage?: string;
  imageStartedAt?: string;
  imageCompletedAt?: string;
  videoStartedAt?: string;
  videoCompletedAt?: string;
  imageWorkflowRunId?: string;
  videoWorkflowRunId?: string;
  status: string;
  reviewStatus?: string;
  manualOverride?: boolean;
  staleState?: string;
  editedBy?: string;
  editedAt?: string;
};

export type UpdateStoryboardShotRequest = {
  visual?: string;
  camera?: string;
  motion?: string;
  mood?: string;
  plannedDurationTicks?: number;
  imagePrompt?: string;
  videoPrompt?: string;
  imageReferenceMode?: "auto" | "custom" | "none";
  imageReferenceKeys?: string[];
  videoReferenceMode?: "auto" | "custom" | "none";
  videoReferenceKeys?: string[];
};

export type StoryboardShotDetail = {
  aspectRatio: string;
  shot: StoryboardShot;
  scriptScene?: StoryboardShot["sourceScene"];
  requirements: StoryboardShotRequirementDetail[];
  imageReferenceOptions: StoryboardShotImageReferenceOption[];
  imageGenerationRuns: StoryboardShotImageGenerationRun[];
  videoReferenceOptions: StoryboardShotVideoReferenceOption[];
  videoGenerationRuns: StoryboardShotVideoGenerationRun[];
  imageArtifact?: Artifact;
  imagePreviewUrl?: string;
  videoArtifact?: Artifact;
  videoPreviewUrl?: string;
};

export type VideoRenderSegment = {
  id: string;
  segmentIndex: number;
  plannedStartTick: number;
  plannedEndTick: number;
  plannedDurationTicks: number;
  plannedDurationFrames: number;
  plannedDurationSeconds: number;
  requestedDurationSeconds: number;
  trimEndTick?: number;
  continuityMode: string;
  status: string;
  retryGeneration: number;
  providerAsyncTaskId?: string;
  providerCallId?: string;
  providerModelId?: string;
  externalTaskId?: string;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  prompt?: string;
  dialogue?: JsonValue;
  nativeAudioRequested: boolean;
  nativeAudioDetected?: boolean;
  audioVerificationStatus: string;
  productionReadiness: string;
  rawAvArtifactId?: string;
  mezzanineArtifactId?: string;
  mezzaninePreviewUrl?: string;
  extractedAudioArtifactId?: string;
  extractedAudioPreviewUrl?: string;
  audioVerifiedBy?: string;
  audioVerifiedAt?: string;
  audioVerificationNotes?: string;
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
};

export type VideoRenderPlan = {
  id: string;
  storyboardPlanId?: string;
  storyboardShotId: string;
  providerAccountId: string;
  providerModelId: string;
  modelFamily: string;
  variantKey: string;
  capabilitySnapshot: VideoGenerationVariant;
  capabilitySnapshotHash: string;
  status: string;
  active: boolean;
  targetDurationTicks: number;
  targetDurationFrames: number;
  targetDurationSeconds: number;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  taskType: string;
  referenceMode: string;
  aspectRatio: string;
  resolution: string;
  audioStrategy: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement: "preferred" | "required" | "disabled";
  nativeAudioStatus: string;
  productionReadiness: string;
  outputArtifactId?: string;
  outputMediaFileId?: string;
  outputStorageKey?: string;
  outputPreviewUrl?: string;
  audioVerifiedBy?: string;
  audioVerifiedAt?: string;
  audioVerificationNotes?: string;
  expiresAt: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  segments: VideoRenderSegment[];
};

export type StoryboardShotImageReferenceOption = {
  key: string;
  sourceType: "derived_asset" | "asset_primary" | string;
  sourceId: string;
  assetId: string;
  assetType: "character" | "scene" | "prop" | string;
  assetName: string;
  title: string;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  isShotAsset: boolean;
  selected: boolean;
  autoSelected: boolean;
};

export type StoryboardShotImageGenerationRun = {
  providerCallId: string;
  modelId?: string;
  modelName?: string;
  status: string;
  prompt?: string;
  promptTruncated: boolean;
  promptVersionId?: string;
  promptHash?: string;
  artifactId?: string;
  previewUrl?: string;
  errorCode?: string;
  errorMessage?: string;
  startedAt?: string;
  completedAt?: string;
  references: string[];
};

export type StoryboardShotVideoReferenceOption = {
  key: string;
  referenceType: "first_frame" | "image" | "last_frame" | "video" | string;
  sourceType: "shot_image" | "derived_asset" | "asset_reference" | "asset_primary" | string;
  sourceId: string;
  assetId?: string;
  assetName?: string;
  title: string;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  selected: boolean;
  autoSelected: boolean;
};

export type StoryboardShotVideoGenerationRun = {
  providerCallId: string;
  providerAsyncTaskId?: string;
  externalTaskId?: string;
  modelId?: string;
  modelName?: string;
  status: string;
  prompt?: string;
  promptTruncated: boolean;
  promptVersionId?: string;
  promptHash?: string;
  artifactId?: string;
  previewUrl?: string;
  errorCode?: string;
  errorMessage?: string;
  startedAt?: string;
  completedAt?: string;
  references: string[];
};

export type ShotProductionSummary = {
  total: number;
  imageSucceeded: number;
  imageMissing: number;
  imageFailed: number;
  imageStale: number;
  imagePromptSucceeded: number;
  imagePromptMissing: number;
  imagePromptFailed: number;
  imagePromptRunning: number;
  videoSucceeded: number;
  videoMissing: number;
  videoFailed: number;
  videoStale: number;
  videoPromptSucceeded: number;
  videoPromptMissing: number;
  videoPromptFailed: number;
  videoPromptRunning: number;
  running: number;
};

export type ShotProductionShot = {
  id: string;
  workflowRunId: string;
  storyboardPlanId?: string;
  scriptSceneId?: string;
  scriptEpisodeId?: string;
  episodeIndex?: number;
  episodeShotIndex?: number;
  episodeTitle?: string;
  shotIndex: number;
  shotNo: number;
  title?: string;
  durationSeconds?: number;
  visual?: string;
  imagePrompt?: string;
  imagePromptStatus: string;
  imagePromptErrorCode?: string;
  imagePromptErrorMessage?: string;
  imagePromptWorkflowRunId?: string;
  videoPrompt?: string;
  scriptDialogue: StoryboardDialogueLine[];
  videoPromptStatus: string;
  videoPromptErrorCode?: string;
  videoPromptErrorMessage?: string;
  videoPromptWorkflowRunId?: string;
  imageStatus: string;
  videoStatus: string;
  staleState: string;
  imageArtifactId?: string;
  imageMediaFileId?: string;
  imageStorageKey?: string;
  imagePreviewUrl?: string;
  videoArtifactId?: string;
  videoMediaFileId?: string;
  videoStorageKey?: string;
  videoPreviewUrl?: string;
  videoReferenceMode: "auto" | "custom" | "none" | string;
  videoReferenceKeys: string[];
  imageErrorCode?: string;
  imageErrorMessage?: string;
  videoErrorCode?: string;
  videoErrorMessage?: string;
  imageWorkflowRunId?: string;
  videoWorkflowRunId?: string;
  providerAsyncTaskId?: string;
  externalTaskId?: string;
  canGenerateImage: boolean;
  canGenerateImagePrompt: boolean;
  canGenerateVideo: boolean;
  canGenerateVideoPrompt: boolean;
  canRetryImage: boolean;
  canRetryVideo: boolean;
};

export type ShotProductionStatus = {
  projectId: string;
  aspectRatio: string;
  summary: ShotProductionSummary;
  shots: ShotProductionShot[];
};

export type ShotProductionActionResponse = {
  action: string;
  workflowRunId: string;
  status: string;
  workflowType: string;
  targetShotIds: string[];
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
      briefSourceCount: number;
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
      finalVideoVersionId?: string | null;
      timelineId?: string | null;
      artifactId?: string | null;
      mediaFileId?: string | null;
      previewUrl?: string | null;
      storageKey?: string | null;
      workflowRunId?: string | null;
      sourceWorkflowRunId?: string | null;
      timelineCount?: number;
      enabledClipCount?: number;
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

export type ReviewItemAction = {
  label: string;
  actionType: string;
  href?: string;
  targetType?: string;
  targetId?: string;
};

export type ReviewRun = {
  id: string;
  organizationId: string;
  projectId: string;
  workflowRunId?: string | null;
  reviewType: string;
  status: string;
  summary?: JsonRecord;
  input?: JsonRecord;
  output?: JsonRecord;
  providerCallId?: string | null;
  promptVersionId?: string | null;
  promptHash?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  createdBy?: string | null;
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
};

export type ReviewItem = {
  id: string;
  organizationId: string;
  projectId: string;
  reviewRunId?: string | null;
  itemType: string;
  category: string;
  severity: string;
  title: string;
  description: string;
  suggestion?: string | null;
  entityType: string;
  entityId?: string | null;
  relatedEntityType?: string | null;
  relatedEntityId?: string | null;
  status: string;
  resolutionNote?: string | null;
  metadata?: JsonRecord;
  actions?: ReviewItemAction[];
  createdBy?: string | null;
  resolvedBy?: string | null;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string | null;
};

export type RunProjectReviewResponse = {
  reviewRunId: string;
  status: string;
  summary?: JsonRecord;
  itemCount: number;
};

export type ReviewFix = {
  id: string;
  organizationId: string;
  projectId: string;
  reviewItemId: string;
  targetEntityType: string;
  targetEntityId?: string | null;
  status: string;
  fixType: string;
  title: string;
  explanation: string;
  beforeSnapshot?: JsonRecord;
  patch?: JsonRecord;
  afterPreview?: JsonRecord;
  regenerateRequest?: JsonRecord | null;
  promptVersionId?: string | null;
  promptHash?: string | null;
  providerCallId?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  createdBy?: string | null;
  appliedBy?: string | null;
  createdAt: string;
  appliedAt?: string | null;
  updatedAt: string;
};

export type ApplyReviewFixResponse = {
  fixId: string;
  status: string;
  reviewItemStatus?: string | null;
  workflowRunId?: string | null;
};

export type DismissReviewFixResponse = {
  fixId: string;
  status: string;
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

export type ProjectTimeline = {
  id: string;
  organizationId: string;
  projectId: string;
  workflowRunId?: string | null;
  title: string;
  status: string;
  aspectRatio: string;
  resolution: string;
	timelineTimebase: number;
	fpsNumerator: number;
	fpsDenominator: number;
  metadata?: JsonRecord;
  createdBy?: string | null;
  editedBy?: string | null;
  createdAt: string;
  updatedAt: string;
  editedAt?: string | null;
};

export type TimelineClip = {
  id: string;
  organizationId: string;
  projectId: string;
  timelineId: string;
  storyboardShotId?: string | null;
  videoArtifactId?: string | null;
  videoMediaFileId?: string | null;
  clipIndex: number;
  title: string;
  enabled: boolean;
  sourceStorageKey?: string | null;
	startTick: number;
	endTick: number;
	durationTicks: number;
	sourceDurationTicks?: number | null;
	trimStartTick: number;
	trimEndTick?: number | null;
	timelineTimebase: number;
	fpsNumerator: number;
	fpsDenominator: number;
	durationSeconds: number;
  sourceDurationSeconds?: number | null;
  trimStartSeconds: number;
  trimEndSeconds?: number | null;
  notes?: string | null;
  metadata?: JsonRecord;
  createdAt: string;
  updatedAt: string;
};

export type TimelineClipDetail = TimelineClip & {
  shot?: StoryboardShot | null;
  videoArtifact?: Artifact | null;
  previewUrl?: string | null;
};

export type FinalVideoVersion = {
  id: string;
  organizationId: string;
  projectId: string;
  timelineId: string;
  workflowRunId?: string | null;
  version: number;
  title: string;
  status: string;
  nativeAudioStatus: string;
  productionReadiness: string;
  artifactId?: string | null;
  mediaFileId?: string | null;
  storageKey?: string | null;
	durationTicks?: number | null;
  durationSeconds?: number | null;
	timelineTimebase: number;
	fpsNumerator: number;
	fpsDenominator: number;
  resolution: string;
  aspectRatio: string;
  composeSettings?: JsonRecord;
  metadata?: JsonRecord;
  createdBy?: string | null;
  createdAt: string;
  previewUrl?: string | null;
};

export type TimelineDetail = {
  timeline: ProjectTimeline;
  clips: TimelineClipDetail[];
  finalVideoVersions: FinalVideoVersion[];
};

export type ComposeTimelineResponse = {
  workflowRunId: string;
  timelineId: string;
  status: string;
};

export type ProjectExport = {
  id: string;
  organizationId: string;
  projectId: string;
  exportType: string;
  status: string;
  title: string;
  format: string;
  workflowRunId?: string | null;
  artifactId?: string | null;
  mediaFileId?: string | null;
  storageKey?: string | null;
  byteSize?: number | null;
  contentHash?: string | null;
  request?: JsonRecord;
  output?: JsonRecord;
  errorCode?: string | null;
  errorMessage?: string | null;
  createdBy?: string | null;
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
};

export type CreateProjectExportResponse = {
  exportId: string;
  workflowRunId: string;
  status: string;
};

export type DownloadUrlResponse = {
  storageKey: string;
  url: string;
  method: string;
  expiresAt: string;
  exportId?: string;
  finalVideoVersionId?: string;
};

export type ProviderAccount = {
  id: string;
  organizationId?: string;
  connectorId?: string;
  connectorKey?: string;
  displayName?: string;
  name?: string;
  baseUrl?: string | null;
  authType?: string;
  providerType?: string;
  config?: JsonRecord;
  credentialPreview?: string | null;
  status: string;
  createdBy?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type ProviderConnector = {
  id: string;
  connectorKey: string;
  name: string;
  type: string;
  isOfficial: boolean;
  manifest?: JsonRecord;
  version: string;
  createdAt: string;
};

export type ProviderModelCapability = {
  id: string;
  providerModelId: string;
  taskTypes?: string[] | JsonValue;
  inputLimits?: JsonRecord;
  outputLimits?: JsonRecord;
  qualityTiers?: JsonValue;
  providerOptionsSchema?: ProviderModelCapabilityOptions;
  pricingPolicy?: JsonRecord;
};

export type ProviderModelCapabilityOptions = JsonRecord & {
  xCapabilities?: ProviderModelXCapabilities;
};

export type VideoGenerationVariant = {
  variantKey: string;
  modelFamily?: string;
  when: {
    taskTypes?: string[];
    referenceModes?: string[];
    nativeAudioRequested?: boolean;
  };
  duration: {
    mode: "continuous_range" | "discrete" | "fixed" | "source_duration";
    minSeconds?: number;
    maxSeconds?: number;
    values?: number[];
    stepSeconds?: number;
  };
  resolutions?: string[];
  aspectRatios?: string[];
  frameRate: {
    mode: "fixed" | "selectable" | "unknown";
    values?: number[];
  };
  supportedPromptLanguages?: string[];
  nativeAudio: {
    support: "true" | "false" | "unknown";
    canDisable?: boolean;
    supportsDialogue?: boolean;
    supportsVoiceover?: boolean;
    supportsAmbientSound?: boolean;
    supportsMusic?: boolean;
    supportsLipSync?: boolean;
    supportedDialogueLanguages?: string[];
    audioTrackSeparable: boolean;
  };
  continuation: {
    supportsExtension: boolean;
    supportsFirstFrame: boolean;
    supportsLastFrame: boolean;
    supportsVideoReference: boolean;
  };
  requestModes?: string[];
  source?: string;
  sourceUrl?: string;
  verifiedAt?: string;
  capabilityVersion?: string;
};

export type ProviderModelXCapabilities = JsonRecord & {
  supportsAsyncTask?: boolean;
  supportsStreaming?: boolean;
  streamTerminalMode?: "done_marker" | "finish_reason" | "done_or_finish_reason";
  supportsReasoning?: boolean;
  supportsReasoningLevels?: boolean;
  supportsMultimodalInput?: boolean;
  supportsReferences?: boolean;
  supportsReferenceImages?: boolean;
  supportsFirstFrame?: boolean;
  supportsLastFrame?: boolean;
  supportsVideoReference?: boolean;
  supportedInputTypes?: string[];
  supportedOutputTypes?: string[];
  requestModes?: string[];
  reasoningLevels?: string[];
  referenceTypes?: string[];
  maxReferenceImages?: number;
  maxReferenceVideos?: number;
  responseFormats?: string[];
  supportedAspectRatios?: string[];
  supportedResolutions?: string[];
  durations?: number[];
  minDurationSeconds?: number;
  maxDurationSeconds?: number;
  videoGenerationVariants?: VideoGenerationVariant[];
};

export type ProviderModel = {
  id: string;
  providerAccountId: string;
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
  capabilities?: ProviderModelCapability[];
  createdAt?: string;
  updatedAt?: string;
};

export type ProviderCatalogSetupField = {
  key: string;
  label?: string;
  type?: string;
  required?: boolean;
  defaultValue?: JsonValue;
};

export type ProviderCatalogSetupSchema = {
  fields?: ProviderCatalogSetupField[];
  defaultConfig?: JsonRecord;
};

export type ProviderCatalogModelTemplate = {
  modelKey: string;
  displayName: string;
  modality: string;
  taskTypes: string[];
  inputLimits?: JsonRecord;
  outputLimits?: JsonRecord;
  qualityTiers?: JsonValue;
  providerOptionsSchema?: ProviderModelCapabilityOptions;
  pricingPolicy?: JsonRecord;
};

export type ProviderCatalogEntry = {
  id: string;
  providerKey: string;
  name: string;
  displayName: string;
  description?: string | null;
  providerType: string;
  category: string;
  logoKey?: string | null;
  docsUrl?: string | null;
  defaultBaseUrl?: string | null;
  defaultAuthType: string;
  connectorManifest?: JsonRecord;
  modelTemplates: ProviderCatalogModelTemplate[];
  supportedTaskTypes: string[];
  setupSchema: ProviderCatalogSetupSchema;
  enabled: boolean;
  isOfficial: boolean;
  installedCount?: number;
  createdAt?: string;
  updatedAt?: string;
};

export type ProviderCatalogInstallResponse = {
  providerKey: string;
  connector?: ProviderConnector;
  account: ProviderAccount;
  models: ProviderModel[];
  bindings: { profileId: string; profileKey: string; modelId: string; bindingId: string }[];
};

export type ProviderGatewayAttempt = {
  providerCallId?: string;
  providerModelId?: string;
  providerAccountId?: string;
  modelProfileBindingId?: string;
  status: string;
  errorCode?: string;
  errorMessage?: string;
  retryable: boolean;
  latencyMs?: number;
};

export type ProviderTestResult = {
  testRunId: string;
  providerCallId: string;
  status: string;
  latencyMs: number;
  errorCode?: string | null;
  errorMessage?: string | null;
  normalizedOutput?: JsonValue;
  attempts?: ProviderGatewayAttempt[];
};

export type ProviderManifestValidationIssue = {
  path: string;
  message: string;
};

export type ProviderManifestValidationResult = {
  valid: boolean;
  errors: ProviderManifestValidationIssue[];
};

export type ProviderManifestTestRunResult = {
  testRunId: string;
  providerCallId: string;
  endpointKey: string;
  status: string;
  latencyMs: number;
  errorCode?: string | null;
  errorMessage?: string | null;
  normalizedOutput?: JsonValue;
};

export type DiscoveredProviderModel = {
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
};

export type ProviderModelDiscoveryResult = {
  models: DiscoveredProviderModel[];
  unsupported: JsonValue[];
};

export type ModelProfileBinding = {
  id: string;
  modelProfileId: string;
  providerModelId: string;
  priority: number;
  weight: number;
  enabled: boolean;
  runtimeOptions: ModelProfileBindingRuntimeOptions;
  createdAt: string;
};

export type ModelProfileBindingRuntimeOptions = {
  reasoningLevel?: string;
};

export type CreateModelProfileBindingRequest = {
  providerModelId: string;
  priority?: number;
  weight?: number;
  enabled?: boolean;
  runtimeOptions?: ModelProfileBindingRuntimeOptions;
};

export type UpdateModelProfileBindingRequest = {
  priority?: number;
  weight?: number;
  enabled?: boolean;
  runtimeOptions?: ModelProfileBindingRuntimeOptions;
};

export type ProviderFallbackStrategy = {
  enabled?: boolean;
  maxAttempts?: number;
  fallbackOn?: string[];
  stopOn?: string[];
};

export type ModelProfile = {
  id: string;
  organizationId?: string;
  profileKey: string;
  name?: string;
  purpose?: string;
  routingStrategy?: "priority" | "priority_with_fallback" | "weighted" | "cost_optimized" | "latency_optimized" | string;
  fallbackStrategy?: ProviderFallbackStrategy | JsonRecord;
  status?: string;
  bindings?: ModelProfileBinding[];
  createdAt?: string;
  updatedAt?: string;
};

export type ProviderVideoMediaProbe = {
  status: "succeeded" | "failed" | "unavailable" | string;
  error?: string;
  durationSeconds?: number;
  width?: number;
  height?: number;
  frameRateNumerator?: number;
  frameRateDenominator?: number;
  frameRate?: number;
  frameCount?: number;
  frameCountEstimated: boolean;
  videoStreamCount: number;
  audioStreamCount: number;
  hasAudio: boolean;
  videoCodec?: string;
  audioCodecs?: string[];
};

export type ProviderCallLog = {
  id: string;
  providerRequestId?: string | null;
  attemptGeneration: number;
  attemptSequence: number;
  organizationId: string;
  projectId?: string | null;
  workflowRunId?: string | null;
  nodeRunId?: string | null;
  providerAccountId: string;
  providerModelId?: string | null;
  credentialId?: string | null;
  modelProfileId?: string | null;
  modelProfileBindingId?: string | null;
  modelProfileKey?: string | null;
  taskType: string;
  executionMode: string;
  status: string;
  latencyMs?: number | null;
  inputTokens?: number | null;
  outputTokens?: number | null;
  requestedDurationSeconds?: number | null;
  actualDurationSeconds?: number | null;
  mediaProbe?: ProviderVideoMediaProbe | JsonRecord;
  estimatedCost?: string | null;
  currency?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  upstreamStatus?: number | null;
  upstreamErrorCode?: string | null;
  requestSnapshot?: JsonRecord;
  responseSnapshot?: JsonRecord;
  normalizedOutput?: JsonRecord;
  artifactIds?: string[] | JsonValue;
  mediaFileIds?: string[] | JsonValue;
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
};

export type ProviderUsageSummary = {
  totalCalls: number;
  failedCalls: number;
  totalCost: string;
  currency: string;
};

export type ProviderTaskType =
  | "any"
  | "text.generate"
  | "text.stream"
  | "image.generate"
  | "video.create_task"
  | "video.poll_task"
  | "video.cancel_task"
  | string;

export type ProviderLimitPolicy = {
  id: string;
  organizationId: string;
  providerAccountId?: string | null;
  providerModelId?: string | null;
  taskType: ProviderTaskType;
  maxConcurrency?: number | null;
  requestsPerMinute?: number | null;
  requestsPerDay?: number | null;
  dailyBudget?: string | null;
  monthlyBudget?: string | null;
  currency: string;
  failureThreshold?: number | null;
  failureWindowSeconds?: number | null;
  circuitCooldownSeconds?: number | null;
  enabled: boolean;
  createdBy?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type ProviderCircuitState = {
  id: string;
  organizationId: string;
  providerAccountId: string;
  providerModelId?: string | null;
  taskType: string;
  state: "closed" | "open" | "half_open" | string;
  failureCount: number;
  successCount: number;
  openedAt?: string | null;
  halfOpenAt?: string | null;
  nextAttemptAt?: string | null;
  lastErrorCode?: string | null;
  lastErrorMessage?: string | null;
  updatedAt: string;
};

export type PromptVersionSummary = {
  id: string;
  version: number;
  status: string;
  title?: string | null;
  contentHash: string;
  createdAt?: string;
  activatedAt?: string;
};

export type PromptTemplate = {
  id: string;
  organizationId?: string | null;
  templateKey: string;
  name: string;
  description?: string | null;
  purpose: string;
  modality: string;
  taskType: string;
  scope: string;
  status: string;
  isSystem: boolean;
  activeVersion?: PromptVersionSummary | null;
  createdBy?: string | null;
  createdAt: string;
  updatedAt?: string | null;
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
