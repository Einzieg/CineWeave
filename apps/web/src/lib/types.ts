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
  username?: string;
  displayName?: string;
  avatarUrl?: string;
  systemAdministrator?: boolean;
};

export type OrganizationChoice = {
  id: string;
  name: string;
};

export type MemberTeamSummary = {
  id: string;
  name: string;
};

export type MemberRoleSummary = {
  bindingId: string;
  roleId: string;
  roleKey: string;
  roleName: string;
  resourceType: "organization" | "workspace" | "project";
  resourceId: string;
  viaTeam: boolean;
  teamId?: string;
  teamName?: string;
  expiresAt?: string;
};

export type OrganizationMember = {
  organizationId: string;
  user: AuthUser;
  status: "active" | "disabled" | "removed";
  accountManagementAllowed: boolean;
  createdAt: string;
  updatedAt: string;
  disabledAt?: string;
  removedAt?: string;
  teams: MemberTeamSummary[];
  roles: MemberRoleSummary[];
};

export type MemberPasswordReset = {
  userId: string;
  resetToken: string;
  expiresAt: string;
};

export type OrganizationMemberList = {
  items: OrganizationMember[];
  page: number;
  pageSize: number;
  total: number;
};

export type AuditActor = {
  id: string;
  username?: string;
  displayName?: string;
  avatarUrl?: string;
};

export type OrganizationAuditLog = {
  id: string;
  organizationId: string;
  actorUserId?: string;
  actor?: AuditActor;
  action: string;
  resourceType: string;
  resourceId?: string;
  metadata: JsonRecord;
  createdAt: string;
};

export type OrganizationAuditLogList = {
  items: OrganizationAuditLog[];
  page: number;
  pageSize: number;
  total: number;
  retentionPolicy: "organization_lifetime";
};

export type InvitationBinding = {
  roleId: string;
  resourceType: "organization" | "workspace" | "project";
  organizationId?: string;
  workspaceId?: string;
  projectId?: string;
};

export type OrganizationInvitation = {
  id: string;
  organizationId: string;
  email: string;
  status: "pending" | "accepted" | "revoked" | "expired";
  baseRoleId: string;
  expiresAt: string;
  acceptedAt?: string;
  acceptedBy?: string;
  invitedBy: string;
  createdAt: string;
  updatedAt: string;
  bindings: InvitationBinding[];
  invitationToken?: string;
  requiresRegistration?: boolean;
  organizationName?: string;
};

export type OrganizationInvitationList = {
  items: OrganizationInvitation[];
  page: number;
  pageSize: number;
  total: number;
};

export type StudioSession = {
  accessToken: string;
  refreshToken: string;
  organizationId: string;
  workspaceId?: string;
  user?: AuthUser;
  membership?: OrganizationMember;
  permissions?: string[];
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

export type LoginResponse =
  | (AuthResponse & { requiresOrganizationSelection: false })
  | {
      requiresOrganizationSelection: true;
      organizationSelectionToken: string;
      organizations: OrganizationChoice[];
    };

export type PendingOrganizationSelection = {
  token: string;
  organizations: OrganizationChoice[];
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
  videoProductionBinding?: ProjectVideoProductionBinding;
  productionGeneration?: ProductionGeneration;
  videoProductionState?: "unconfigured" | "storyboard_required" | "ready" | "rebuilding" | "blocked" | "reconfiguration_required";
  videoProductionLocked?: boolean;
  timelineTimebase?: number;
  fpsNumerator?: number;
  fpsDenominator?: number;
  activeScriptId?: string | null;
  activeFinalVideoVersionId?: string | null;
  activeAudioMixVersionId?: string | null;
  status?: string;
  settings?: JsonRecord;
  revision: number;
  createdAt?: string;
  updatedAt?: string;
};

export type VideoProductionProfileKey =
  | "single_frame_i2v"
  | "first_last_frame"
  | "multimodal_reference"
  | "storyboard_sheet";

export type VideoProductionProfileVersion = {
  id: string;
  profileId: string;
  profileKey: VideoProductionProfileKey;
  profileName: string;
  strategyFamily: string;
  description: string;
  version: number;
  lifecycleState: "draft" | "published" | "retired";
  implementationState: "available" | "reserved";
  configuration: JsonRecord;
  capabilityRequirements: JsonRecord;
  promptContract: JsonRecord;
  inputContractVersion: string;
  configurationHash: string;
  promptContractHash: string;
  createdAt: string;
  publishedAt?: string | null;
  retiredAt?: string | null;
  available: boolean;
};

export type ProjectVideoProductionBinding = {
  id: string;
  projectId: string;
  profileVersionId: string;
  profileKey: VideoProductionProfileKey;
  profileName: string;
  profileVersion: number;
  lifecycleState: "draft" | "published" | "retired";
  implementationState: "available" | "reserved";
  status: "active" | "superseded";
  compatibilityPolicy: "strict" | "compatible_fallback";
  overrides: JsonRecord;
  profileSnapshot: JsonRecord;
  profileSnapshotHash: string;
  revision: number;
  createdAt: string;
  supersededAt?: string | null;
};

export type ProductionGeneration = {
  id: string;
  organizationId: string;
  projectId: string;
  bindingId: string;
  generationNo: number;
  status: "active" | "superseded";
  sourceGenerationId?: string | null;
  rebuildId?: string | null;
  createdAt: string;
  activatedAt?: string | null;
  supersededAt?: string | null;
};

export type ProductionManualBindingSnapshot = {
  promptVersionId: string;
  templateKey: string;
  contentHash: string;
};

export type ProductionConfigurationSnapshot = {
  schemaVersion: 2;
  projectType: string;
  contentType: string;
  aspectRatio: string;
  videoRatio: string;
  artStyle: string;
  directorManual: string;
  visualManual: string;
  imageModelProfileKey: string;
  videoModelProfileKey: string;
  scriptModelProfileKey: string;
  ttsModelProfileKey: string;
  asrModelProfileKey: string;
  audioStrategy: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement: "preferred" | "required" | "disabled";
  imageQuality: string;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  settings: JsonRecord;
  manualBindings: Record<string, ProductionManualBindingSnapshot>;
};

export type VideoProductionConfigurationInput = {
  projectType: string;
  contentType: string;
  aspectRatio: string;
  videoRatio: string;
  artStyle: string;
  directorManualPromptVersionId?: string;
  visualManualPromptVersionId?: string;
  imageModelProfileKey: string;
  videoModelProfileKey: string;
  scriptModelProfileKey: string;
  ttsModelProfileKey: string;
  asrModelProfileKey: string;
  audioStrategy: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement: "preferred" | "required" | "disabled";
  imageQuality: string;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  settings: JsonRecord;
};

export type VideoProductionCompatibilityIssue = { code: string; message: string };

export type VideoProductionCompatibility = {
  profile: VideoProductionProfileVersion;
  modelProfileKey: string;
  nativeAudioRequired: boolean;
  compatible: boolean;
  executable: boolean;
  issues: VideoProductionCompatibilityIssue[];
  candidates: Array<{
    modelProfileBindingId: string;
    modelProfileId: string;
    modelProfileKey: string;
    modelProfileName: string;
    providerAccountId: string;
    providerAccountName: string;
    providerModelId: string;
    providerModelKey: string;
    providerModelName: string;
    priority: number;
    weight: number;
    capability: { taskTypes: string[]; providerOptionsSchema: JsonRecord };
    compatibility: { compatible: boolean; issues: VideoProductionCompatibilityIssue[] };
  }>;
};

export type VideoProductionRebuildImpact = {
  projectId: string;
  expectedProjectRevision: number;
  sourceBindingId: string;
  sourceBindingRevision: number;
  sourceGenerationId: string;
  sourceGenerationNo: number;
  targetProfileVersionId: string;
  targetProfileKey: VideoProductionProfileKey;
  targetProfileVersion: number;
  reason: "profile_change" | "configuration_change" | "profile_and_configuration_change" | string;
  targetConfiguration: ProductionConfigurationSnapshot;
  targetConfigurationHash: string;
  episodes: Array<{
    scriptEpisodeId: string;
    episodeOrdinal: number;
    scriptEpisodeRevision: number;
    scriptEpisodeContentHash: string;
    sourceStoryboardPlanId?: string | null;
  }>;
  counts: Record<string, number>;
  impactToken: string;
};

export type VideoProductionRebuild = {
  id: string;
  organizationId: string;
  projectId: string;
  sourceBindingId: string;
  sourceGenerationId: string;
  sourceVideoProductionState: "unconfigured" | "storyboard_required" | "ready" | "rebuilding" | "blocked" | "reconfiguration_required";
  targetProfileVersionId: string;
  targetBindingId?: string | null;
  targetGenerationId?: string | null;
  status: "planned" | "approved" | "running" | "partial_succeeded" | "storyboard_required" | "succeeded" | "failed" | "cancelled";
  reason: string;
  targetConfiguration: ProductionConfigurationSnapshot;
  targetConfigurationHash: string;
  impactSnapshot: VideoProductionRebuildImpact;
  impactToken: string;
  expectedProjectRevision: number;
  episodeCount: number;
  retainedAssetCount: number;
  workflowRunId?: string | null;
  idempotencyKey: string;
  requestedBy: string;
  requestedAt: string;
  approvedAt?: string | null;
  startedAt?: string | null;
  completedAt?: string | null;
  failureCode?: string | null;
  failureMessage?: string | null;
};

export type VideoProductionRebuildItem = {
  id: string;
  rebuildId: string;
  projectId: string;
  scriptEpisodeId: string;
  episodeOrdinal: number;
  scriptEpisodeRevision: number;
  scriptEpisodeContentHash: string;
  sourceStoryboardPlanId?: string | null;
  targetStoryboardPlanId?: string | null;
  workflowRunId?: string | null;
  status: "pending" | "running" | "succeeded" | "failed" | "skipped";
  checkpoint: JsonRecord;
  attemptCount: number;
  startedAt?: string | null;
  completedAt?: string | null;
  failureCode?: string | null;
  failureMessage?: string | null;
};

export type CreateProjectRequest = {
  workspaceId: string;
  name: string;
  description?: string;
  projectType?: string;
  contentType?: string;
  aspectRatio?: string;
  videoRatio?: string;
  artStyle?: string;
  directorManualPromptVersionId?: string;
  visualManualPromptVersionId?: string;
  imageModelProfileKey?: string;
  videoModelProfileKey?: string;
  scriptModelProfileKey?: string;
  ttsModelProfileKey?: string;
  asrModelProfileKey?: string;
  audioStrategy?: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement?: "preferred" | "required" | "disabled";
  imageQuality?: string;
  videoProductionProfileKey?: VideoProductionProfileKey;
  videoProductionProfileVersion?: number;
  compatibilityPolicy?: "strict" | "compatible_fallback";
  timelineTimebase?: number;
  fpsNumerator?: number;
  fpsDenominator?: number;
  settings?: JsonRecord;
};

export type UpdateProjectRequest = {
  name?: string;
  description?: string;
  expectedRevision?: number;
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
  isCurrent: boolean;
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
  previewExpiresAt?: string;
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

export type ShotAssetRequirementReviewIssue = {
  code: string;
  message: string;
};

export type ShotAssetRequirementReviewItem = {
  requirementId: string;
  storyboardShotId: string;
  shotNo: number;
  assetId: string;
  assetType: string;
  assetName: string;
  requirementType: string;
  status: string;
  previousReviewStatus: string;
  reviewStatus: string;
  eligible: boolean;
  issues: ShotAssetRequirementReviewIssue[];
  warnings: ShotAssetRequirementReviewIssue[];
  updatedAt: string;
};

export type BatchReviewShotAssetRequirementsResponse = {
  validationVersion: string;
  requestedStatus: string;
  totalItems: number;
  eligibleCount: number;
  blockedCount: number;
  approvedCount: number;
  needsEditCount: number;
  rejectedCount: number;
  unchangedCount: number;
  notFoundIds: string[];
  items: ShotAssetRequirementReviewItem[];
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

export type StoryboardShotStateVersion = {
  id: string;
  productionGenerationId: string;
  storyboardShotId: string;
  purpose: "anchor" | "video";
  stateRole: "planned_entry" | "planned_exit" | "observed_exit";
  revision: number;
  status: "draft" | "approved" | "rejected" | "stale";
  state: JsonRecord;
  stateHash: string;
  sourceType: string;
  sourceId?: string;
  promptVersionId?: string;
  providerCallId?: string;
  modelId?: string;
  createdBy?: string;
  createdAt: string;
  approvedAt?: string;
};

export type StoryboardShotTransition = {
  id: string;
  productionGenerationId: string;
  storyboardPlanId: string;
  sourceShotId?: string;
  targetShotId: string;
  transitionType: "match_action_cut" | "same_scene_cut" | "camera_cut" | "subject_change" | "scene_cut" | "time_jump" | "montage_cut" | "unclassified";
  tailPolicy: "soft" | "none";
  anchorPolicy: "new_anchor" | "match_action_anchor" | "independent_anchor";
  carryConstraints: string[];
  resetConstraints: string[];
  confidence: number;
  revision: number;
  status: "active" | "superseded" | "stale";
  reviewStatus: "pending" | "approved" | "rejected" | "needs_edit";
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
};

export type EpisodeVideoProductionProviderTask = {
  id: string;
  status: string;
  externalTaskId?: string;
  pollCount: number;
  errorCode?: string;
  errorMessage?: string;
  requestHash?: string;
  createdAt: string;
  completedAt?: string;
};

export type EpisodeVideoProductionSegment = {
  id: string;
  segmentIndex: number;
  status: string;
  inputContractKey?: string;
  requestedDurationSeconds: number;
  providerTasks: EpisodeVideoProductionProviderTask[];
};

export type EpisodeVideoProductionItem = {
  id: string;
  batchId: string;
  storyboardShotId: string;
  shotNo: number;
  shotTitle: string;
  shotStateHash: string;
  executionIdentityVersion: number;
  predecessorVideoRenderPlanId?: string;
  referencePackId?: string;
  referencePackStatus?: string;
  videoPromptPlanId?: string;
  videoPromptPlanStatus?: string;
  videoPromptPlanRevision?: number;
  videoRenderPlanId?: string;
  videoRenderPlanStatus?: string;
  providerAsyncTaskId?: string;
  providerAsyncTaskStatus?: string;
  externalTaskId?: string;
  providerPollCount?: number;
  providerErrorCode?: string;
  providerErrorMessage?: string;
  anchorId?: string;
  anchorStatus?: string;
  anchorReviewStatus?: string;
  mediaStatus: "pending" | "transferring" | "stored" | "failed";
  status: "queued" | "running" | "succeeded" | "failed" | "cancelling" | "cancelled" | "discarded";
  attempt: number;
  revision: number;
  errorCode?: string;
  errorDetail: JsonRecord;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  segments: EpisodeVideoProductionSegment[];
};

export type EpisodeVideoProductionBatch = {
  id: string;
  checkpointId: string;
  ordinal: number;
  dependencySnapshotHash: string;
  workflowRunId?: string;
  temporalWorkflowId?: string;
  temporalRunId?: string;
  status: string;
  attempt: number;
  totalItems: number;
  succeededItems: number;
  failedItems: number;
  cancelledItems: number;
  revision: number;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  items: EpisodeVideoProductionItem[];
};

export type EpisodeVideoProductionCheckpoint = {
  id: string;
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  scriptEpisodeId: string;
  episodeIndex: number;
  episodeTitle: string;
  profileVersionId: string;
  profileSnapshotHash: string;
  temporalWorkflowId: string;
  temporalRunId?: string;
  status: string;
  nextBatchOrdinal: number;
  revision: number;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  batches: EpisodeVideoProductionBatch[];
};

export type WorkflowVideoProductionActivity = {
  workflowRunId: string;
  checkpoints: EpisodeVideoProductionCheckpoint[];
  totalItems: number;
  succeededItems: number;
  failedItems: number;
  activeItems: number;
};

export type ShotVisualAnchor = {
  id: string;
  productionGenerationId: string;
  storyboardShotId: string;
  shotStateVersionId?: string;
  anchorRole: "planned_first_frame" | "planned_last_frame" | "storyboard_sheet" | "storyboard_panel" | "observed_tail_frame" | "continuity_hint";
  revision: number;
  status: "draft" | "generating" | "ready" | "failed" | "stale" | "archived";
  reviewStatus: "pending" | "approved" | "rejected" | "needs_edit";
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  prompt?: string;
  promptVersionId?: string;
  promptHash?: string;
  providerCallId?: string;
  modelId?: string;
  referencePackId?: string;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
};

export type ShotReferencePack = {
  id: string;
  productionGenerationId: string;
  storyboardShotId: string;
  profileSnapshotHash: string;
  shotStateHash: string;
  capabilitySnapshotHash: string;
  manifest: JsonRecord;
  manifestHash: string;
  status: "active" | "stale" | "archived";
  createdAt: string;
};

export type ShotReferencePackItem = {
  id: string;
  referenceKey: string;
  role: string;
  mediaType: "image" | "video" | "audio";
  semantics: string;
  required: boolean;
  priority: number;
  sourceType: string;
  sourceId?: string;
  assetId?: string;
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  contentHash: string;
  metadata: JsonRecord;
};

export type StoryboardSheetManifest = {
  id: string;
  productionGenerationId: string;
  storyboardShotId: string;
  sheetAnchorId: string;
  sheetPreviewUrl?: string;
  revision: number;
  contractVersion: string;
  plannedDurationTicks: number;
  timelineTimebase: number;
  videoAspectRatio: string;
  sheetAspectRatio: string;
  gridRows: number;
  gridColumns: number;
  panelCount: number;
  entryStateHash: string;
  exitStateHash: string;
  manifest: JsonRecord;
  manifestHash: string;
  status: "draft" | "processing" | "ready" | "failed" | "stale" | "archived";
  reviewStatus: "pending" | "approved" | "rejected" | "needs_edit";
  reviewerPromptVersionId?: string;
  reviewerProviderCallId?: string;
  reviewerModelId?: string;
  reviewerOutput: JsonRecord;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
  reviewedAt?: string;
};

export type StoryboardSheetPanel = {
  id: string;
  productionGenerationId: string;
  storyboardShotId: string;
  manifestId: string;
  visualAnchorId: string;
  ordinal: number;
  gridRow: number;
  gridColumn: number;
  timeTick: number;
  normalizedPosition: number;
  stage: string;
  actionStage: string;
  expectedState: JsonRecord;
  expectedStateHash: string;
  status: "planned" | "cropped" | "failed" | "stale" | "archived";
  reviewStatus: "pending" | "approved" | "rejected" | "needs_edit";
  artifactId?: string;
  mediaFileId?: string;
  storageKey?: string;
  previewUrl?: string;
  contentHash?: string;
  crop: JsonRecord;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
};

export type PromptContextPlan = {
  id: string;
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  storyboardPlanId: string;
  storyboardShotId: string;
  scriptEpisodeId: string;
  scriptSceneId?: string;
  revision: number;
  status: "active" | "stale" | "archived";
  episodeContinuityDigest: string;
  currentSceneScript: string;
  adjacentSceneSummaries: JsonValue[];
  currentShotState: JsonRecord;
  verbatimDialogueCues: JsonValue[];
  modelContextLimit: number;
  modelPromptLimit: number;
  budgetAllocation: JsonRecord;
  sourceHashes: JsonRecord;
  planHash: string;
  createdAt: string;
  staleAt?: string;
  archivedAt?: string;
};

export type VideoPromptPlan = {
  id: string;
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  profileVersionId: string;
  storyboardShotId: string;
  promptContextPlanId: string;
  promptVersionId: string;
  reviewerPromptVersionId?: string;
  workflowRunId?: string;
  nodeRunId?: string;
  providerCallId?: string;
  reviewerProviderCallId?: string;
  providerModelId?: string;
  revision: number;
  status: "draft" | "generating" | "reviewing" | "approved" | "rejected" | "failed" | "stale" | "archived";
  renderedPrompt: string;
  promptHash: string;
  promptContextPlanHash: string;
  profileSnapshotHash: string;
  shotStateHash: string;
  transitionHash?: string;
  referencePackHash: string;
  capabilitySnapshotHash: string;
  inputContractVersion: string;
  dialogueCues: JsonValue[];
  nativeAudioRequired: boolean;
  audioStrategy: "native_av" | "hybrid" | "tts_postdub";
  audioRequirement: "preferred" | "required" | "disabled";
  reviewerOutput: JsonRecord;
  metadata: JsonRecord;
  createdAt: string;
  reviewedAt?: string;
  approvedAt?: string;
  staleAt?: string;
  archivedAt?: string;
};

export type StoryboardShotStateResponse = { items: StoryboardShotStateVersion[] };
export type StoryboardShotTransitionResponse = { active?: StoryboardShotTransition; items: StoryboardShotTransition[] };
export type ShotVisualAnchorResponse = { items: ShotVisualAnchor[] };
export type ShotReferencePackResponse = { pack?: ShotReferencePack; items: ShotReferencePackItem[]; history: ShotReferencePack[] };
export type StoryboardSheetResponse = {
  active?: StoryboardSheetManifest;
  manifest?: StoryboardSheetManifest;
  panels: StoryboardSheetPanel[];
  history: StoryboardSheetManifest[];
};
export type VideoPromptPlanResponse = { active?: VideoPromptPlan; items: VideoPromptPlan[]; contextPlan?: PromptContextPlan };

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
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  profileVersionId: string;
  productionProfileSnapshot: JsonRecord;
  productionProfileSnapshotHash: string;
  storyboardPlanId?: string;
  storyboardShotId: string;
  providerAccountId: string;
  providerModelId?: string;
  modelFamily: string;
  variantKey: string;
  capabilitySnapshot: VideoGenerationVariant;
  capabilitySnapshotHash: string;
  capabilityAttestationId?: string;
  shotStateRevision?: number;
  shotStateHash?: string;
  transitionSnapshot: JsonRecord;
  transitionHash?: string;
  referencePackId?: string;
  referencePackHash?: string;
  initialInputContractSnapshot?: JsonRecord;
  initialInputContractHash?: string;
  continuationInputContractSnapshot?: JsonRecord;
  continuationInputContractHash?: string;
  promptContextPlanId?: string;
  promptContextPlanHash?: string;
  videoPromptPlanId?: string;
  dialogueCues: JsonValue[];
  nativeAudioRequired: boolean;
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
  metadata: JsonRecord;
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
      approvedMissingDerivedImageCount: number;
      approvedCount: number;
      pendingReviewCount: number;
      reviewPendingCount: number;
      needsEditCount: number;
      rejectedCount: number;
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
  operationId?: string;
  derivedAssets?: DerivedAssetBatchProjection;
};

export type DerivedAssetExecutionProjection = {
  id: string;
  nodeRunId: string;
  nodeKey: string;
  attemptNo: number;
  status: string;
  revision: number;
  providerRequestId?: string | null;
  providerCallId?: string | null;
  selectedCredentialId?: string | null;
  artifactId?: string | null;
  mediaFileId?: string | null;
  storageKey?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  diagnostic: JsonRecord;
  lateResultCount: number;
  lateResultDiagnostics: JsonValue;
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
  productionGenerationId: string;
};

export type DerivedAssetRequestItemProjection = {
  id: string;
  inputOrdinal: number;
  originalId: string;
  requirementId?: string | null;
  duplicateOfRequestItemId?: string | null;
  rootRequestItemId?: string | null;
  retryOfRequestItemId?: string | null;
  disposition: string;
  dispositionDetail: JsonRecord;
  errorCode?: string | null;
  errorMessage?: string | null;
  retryable: boolean;
  inputSnapshot: JsonRecord;
  inputHash: string;
  status: string;
  currentAttemptId?: string | null;
  currentAttemptNo?: number | null;
  revision: number;
  createdAt: string;
  updatedAt: string;
  execution?: DerivedAssetExecutionProjection | null;
};

export type DerivedAssetBatchProjection = {
  id: string;
  organizationId: string;
  projectId: string;
  workflowRunId: string;
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  rootBatchId?: string | null;
  retryOfBatchId?: string | null;
  retryDepth: number;
  requestMode: string;
  filters: JsonRecord;
  filtersHash: string;
  selectorCandidateCount: number;
  selectorSkippedCount: number;
  idempotencyKey: string;
  requestHash: string;
  status: string;
  revision: number;
  totalItems: number;
  executableItems: number;
  reviewRequiredItems: number;
  notFoundItems: number;
  generationMismatchItems: number;
  alreadyRunningItems: number;
  duplicateItems: number;
  skippedItems: number;
  pendingItems: number;
  queuedItems: number;
  runningItems: number;
  succeededItems: number;
  failedRetryableItems: number;
  failedTerminalItems: number;
  cancelledItems: number;
  discardedItems: number;
  errorCode?: string | null;
  errorMessage?: string | null;
  createdBy?: string | null;
  createdAt: string;
  updatedAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
  items: DerivedAssetRequestItemProjection[];
};

export type DerivedAssetBatchCommandResult = {
  batch: DerivedAssetBatchProjection;
  workflowRun: WorkflowRun;
  idempotentReplay?: boolean;
  operationId?: string;
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
  operationId?: string;
  derivedAssets?: DerivedAssetBatchProjection;
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
  credentialCount?: number;
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

export type ProviderCredential = {
  id: string;
  organizationId: string;
  providerAccountId: string;
  credentialKey: string;
  credentialType: string;
  maskedPreview: string;
  status: "active" | "rotated" | "revoked" | "expired";
  isActive: boolean;
  availableModelCount: number;
  lastDiscoveredAt?: string | null;
  createdBy?: string | null;
  createdAt: string;
  expiresAt?: string | null;
  rotatedAt?: string | null;
};

export type ShotProductionBatchRequest = {
  scriptEpisodeId?: string;
  workflowRunId?: string;
  shotIds?: string[];
  force?: boolean;
  maxConcurrency?: number;
  resolution?: string;
  pollIntervalSeconds?: number;
  maxPolls?: number;
};

export type VideoInputSlot = {
  role: string;
  mediaType: "image" | "video" | "audio" | "text" | string;
  semantics: string;
  min: number;
  max: number;
  ordered: boolean;
};

export type VideoInputContractKey =
  | "text_only"
  | "first_frame"
  | "first_last_frames"
  | "semantic_references"
  | "first_frame_plus_references"
  | "storyboard_sheet_reference"
  | "video_reference"
  | "video_extension";

export type VideoInputContract = {
  contractKey: VideoInputContractKey;
  requestMode: string;
  slots: VideoInputSlot[];
  mutuallyExclusiveRoles: string[][];
  supportsStoryboardSheetReference: boolean;
  supportsVideoExtension: boolean;
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
  inputContract?: VideoInputContract;
  continuationInputContracts?: VideoInputContract[];
  requestModes?: string[];
  source?: string;
  sourceUrl?: string;
  verifiedAt?: string;
  capabilityVersion?: string;
  verificationStatus?: "official" | "tested" | "inferred" | "unknown";
};

export type VideoCapabilityAttestation = {
  id: string;
  organizationId: string;
  providerModelId: string;
  variantKey: string;
  capabilitySnapshotHash: string;
  verificationStatus: "official" | "tested" | "inferred" | "unknown";
  evidenceType: "official_documentation" | "adapter_contract_test" | "controlled_probe" | "administrator_review";
  evidence: JsonRecord;
  decision: "approved" | "rejected";
  reason: string;
  decidedBy?: string;
  decidedAt: string;
  supersedesAttestationId?: string;
  revokedBy?: string;
  revokedAt?: string;
  currentSnapshot: boolean;
  active: boolean;
  createdAt: string;
};

export type VideoCapabilityVariantStatus = {
  variantKey: string;
  capabilitySnapshotHash: string;
  verificationStatus: "official" | "tested" | "inferred" | "unknown";
  source?: string;
  sourceUrl?: string;
  verifiedAt?: string;
  initialInputContract: VideoInputContract;
  continuationInputContracts: VideoInputContract[];
  nativeAudio: VideoGenerationVariant["nativeAudio"];
  duration: VideoGenerationVariant["duration"];
  currentAttestation?: VideoCapabilityAttestation;
};

export type VideoCapabilityAttestationList = {
  variants: VideoCapabilityVariantStatus[];
  attestations: VideoCapabilityAttestation[];
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
  credentialId?: string;
  credentialKey?: string;
  models: DiscoveredProviderModel[];
  unsupported: JsonValue[];
  sync: {
    discoveredCount: number;
    createdCount: number;
    existingCount: number;
    skippedDisabledCount: number;
    ignoredCount: number;
  };
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

export type SystemOrganization = {
  id: string;
  name: string;
  slug: string;
  createdAt: string;
  activeMemberCount: number;
  workspaceCount: number;
  projectCount: number;
  ownerCount: number;
};

export type SystemOrganizationList = {
  items: SystemOrganization[];
  page: number;
  pageSize: number;
  total: number;
};

export type CreateSystemOrganizationRequest = {
  name: string;
  workspaceName?: string;
  ownerIdentifier: string;
};

export type CreatedSystemOrganization = {
  organization: SystemOrganization;
  initialOwner: AuthUser;
  defaultWorkspaceId: string;
};

export type Workspace = {
  id: string;
  organizationId: string;
  name: string;
  createdAt?: string;
};

export type Team = {
  id: string;
  organizationId?: string;
  name: string;
  description?: string | null;
  status: string;
  memberCount: number;
  bindingCount: number;
  createdAt?: string;
};

export type TeamMember = {
  teamId: string;
  userId: string;
  status: "active" | "disabled";
  createdBy?: string;
  createdAt: string;
  user: AuthUser;
};

export type TeamImpact = {
  teamId: string;
  activeMemberCount: number;
  activeBindingCount: number;
};

export type Role = {
  id: string;
  roleKey: string;
  name: string;
  description?: string | null;
  scope: "organization" | "workspace" | "project";
  isSystem: boolean;
  permissions?: Permission[];
  bindingCount: number;
};

export type RoleImpact = {
  roleId: string;
  bindingCount: number;
  directUserCount: number;
  teamCount: number;
  affectedUserCount: number;
  organizationBindings: number;
  workspaceBindings: number;
  projectBindings: number;
  permissionKeys: string[];
};

export type Permission = {
  permissionKey: string;
  name?: string;
  description?: string;
};

export type RoleBinding = {
  id: string;
  organizationId: string;
  roleId: string;
  roleKey: string;
  roleName?: string;
  subjectType: "user" | "team";
  subjectUserId?: string;
  subjectTeamId?: string;
  subjectName?: string;
  resourceType: "organization" | "workspace" | "project";
  resourceOrganizationId?: string;
  resourceWorkspaceId?: string;
  resourceProjectId?: string;
  resourceName?: string;
  expiresAt?: string;
  createdBy?: string;
  createdAt: string;
};

export type RoleBindingList = {
  items: RoleBinding[];
  page: number;
  pageSize: number;
  total: number;
};
