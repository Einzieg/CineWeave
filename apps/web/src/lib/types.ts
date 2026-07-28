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

export type ProjectKind = "narrative" | "commerce_video";

export type NarrativeProjectType =
  | "short_film"
  | "comic_drama"
  | "brand_ad"
  | "character_ip"
  | "other";

export type NarrativeContentType = "novel" | "script" | "storyboard_first" | "original";

export type Project = {
  id: string;
  organizationId: string;
  workspaceId?: string;
  name: string;
  description?: string | null;
  projectKind: ProjectKind;
  projectType?: NarrativeProjectType | "commerce_video" | null;
  contentType?: NarrativeContentType | null;
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
  lifecycleStatus: "active" | "deleting";
  deletionRevision: number;
  deletionRequestedAt?: string | null;
  settings?: JsonRecord;
  revision: number;
  setupSessionId?: string;
  setupState?: CommerceSetupState;
  workflowTemplateVersionId?: string;
  setupConfigurationHash?: string;
  scriptUnitDefaults?: CommerceScriptUnitDefaults;
  createdAt?: string;
  updatedAt?: string;
};

export type ProjectDeletionImpact = {
  projectId: string;
  projectName: string;
  projectRevision: number;
  currentDeletionRevision: number;
  productCount: number;
  scriptUnitCount: number;
  storyboardShotCount: number;
  artifactCount: number;
  mediaFileCount: number;
  finalVideoCount: number;
  activeWorkflowCount: number;
  activeAgentTaskCount: number;
  activeProviderTaskCount: number;
  storageObjectCount: number;
  storageByteSize: number;
  generatedAt: string;
  impactHash: string;
};

export type ProjectDeletionRequestStatus =
  | "requested"
  | "cancelling_tasks"
  | "waiting_for_terminal"
  | "deleting_storage"
  | "deleting_business_data"
  | "completed"
  | "failed_retryable"
  | "failed_terminal";

export type ProjectDeletionRequest = {
  id: string;
  organizationId: string;
  workspaceId: string;
  projectId: string;
  projectName: string;
  projectRevision: number;
  deletionRevision: number;
  status: ProjectDeletionRequestStatus;
  impactSnapshot: JsonRecord;
  impactHash: string;
  manifestCursor: number;
  storageObjectCount: number;
  storageDeletedCount: number;
  storageFailedCount: number;
  storageSkippedSharedCount: number;
  temporalWorkflowId: string;
  idempotencyKey: string;
  requestedBy?: string | null;
  requestedAt: string;
  startedAt?: string | null;
  drainDeadlineAt: string;
  updatedAt: string;
  completedAt?: string | null;
  expiresAt?: string | null;
  errorCode?: string | null;
  errorMessage?: string | null;
  retryCount: number;
};

export type CreateProjectDeletionRequest = {
  projectName: string;
  expectedProjectRevision: number;
  impactHash: string;
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

type CreateProjectBaseRequest = {
  workspaceId: string;
  name: string;
  description?: string;
  aspectRatio?: string;
  videoRatio?: string;
  audioStrategy?: "native_av" | "hybrid" | "tts_postdub" | "external_audio";
  audioRequirement?: "preferred" | "required" | "disabled";
  imageQuality?: string;
  timelineTimebase?: number;
  fpsNumerator?: number;
  fpsDenominator?: number;
  settings?: JsonRecord;
};

export type CreateNarrativeProjectRequest = CreateProjectBaseRequest & {
  projectKind: "narrative";
  projectType?: NarrativeProjectType;
  contentType?: NarrativeContentType;
  artStyle?: string;
  directorManualPromptVersionId?: string;
  visualManualPromptVersionId?: string;
  imageModelProfileKey?: string;
  videoModelProfileKey?: string;
  scriptModelProfileKey?: string;
  ttsModelProfileKey?: string;
  asrModelProfileKey?: string;
  videoProductionProfileKey?: VideoProductionProfileKey;
  videoProductionProfileVersion?: number;
  compatibilityPolicy?: "strict" | "compatible_fallback";
};

export type CreateCommerceVideoProjectRequest = CreateProjectBaseRequest & {
  projectKind: "commerce_video";
  defaultTargetDurationSeconds?: number;
  defaultTargetPlatform?: string;
  defaultLanguageMode?: "auto" | "explicit";
  defaultTargetLanguage?: string;
};

export type CreateProjectRequest = CreateNarrativeProjectRequest | CreateCommerceVideoProjectRequest;

export type CommerceSetupState =
  | "draft"
  | "uploading"
  | "resolving_language"
  | "waiting_user_confirmation"
  | "localizing"
  | "validating"
  | "needs_user_review"
  | "ready"
  | "starting"
  | "started"
  | "completed"
  | "failed"
  | "abandoned";

export type CommerceSetupRunState =
  | "queued"
  | "running"
  | "waiting_user_confirmation"
  | "needs_user_review"
  | "succeeded"
  | "failed"
  | "cancelled";

export type CommerceLanguageMode = "auto" | "explicit";

export type CommerceScriptUnitDefaults = {
  targetDurationSeconds: number;
  targetPlatform: string;
  languageMode: CommerceLanguageMode;
  targetLanguage?: string | null;
};

export type UpdateCommerceScriptUnitDefaultsRequest = CommerceScriptUnitDefaults & {
  expectedRevision: number;
};

export type CommerceSetupSession = {
  id: string;
  organizationId: string;
  workspaceId: string;
  projectId: string;
  workflowTemplateVersionId: string;
  clientRequestId: string;
  scopeType: string;
  state: CommerceSetupState;
  step: string;
  revision: number;
  inputSnapshot: JsonRecord;
  setupAttempt: number;
  setupWorkflowRunId?: string;
  productionWorkflowRunId?: string;
  productId?: string;
  scriptUnitId?: string;
  sourceScriptVersionId?: string;
  localizationId?: string;
  lastErrorCode?: string;
  lastErrorMessage?: string;
  createdAt: string;
  updatedAt: string;
  expiresAt: string;
  completedAt?: string;
};

export type CommerceSetupRun = {
  id: string;
  organizationId: string;
  projectId: string;
  setupSessionId: string;
  attemptNo: number;
  temporalWorkflowId: string;
  status: CommerceSetupRunState;
  input: JsonRecord;
  output: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  completedAt?: string;
  revision: number;
};

export type CompleteCommerceSetupSessionRequest = {
  expectedRevision: number;
};

export type CommerceSetupCompletionResult = {
  setupWorkflowRunId: string;
  setupSession: CommerceSetupSession;
};

export type ConfirmCommerceSetupLanguageRequest = {
  expectedRevision: number;
  resolutionId: string;
  targetLanguage: string;
};

export type CommerceSetupLanguageConfirmationResult = {
  setupSession: CommerceSetupSession;
  setupRun: CommerceSetupRun;
};

export type CommerceProjectLanguageOption = {
  locale: string;
  label: string;
  textAvailable: boolean;
  imagePromptAvailable: boolean;
  videoPromptAvailable: boolean;
  nativeAudioAvailable: boolean;
  blockers: string[];
};

export type CommerceProjectModelRequirement = {
  role: string;
  label: string;
  profileKey: string;
  taskType: string;
  modality: "text" | "image" | "video" | string;
  usesInputLanguage: boolean;
  usesOutputLanguage: boolean;
  usesPromptLanguage: boolean;
  usesNativeAudio: boolean;
  ready: boolean;
  candidateCount: number;
  blocker?: string;
};

export type CommerceProjectOptions = {
  workflowTemplateVersionId: string;
  workflowTemplateVersion: number;
  templateContentHash: string;
  videoProductionProfileKey: VideoProductionProfileKey;
  videoProductionProfileVersion: number;
  available: boolean;
  blockers: string[];
  durations: number[];
  aspectRatios: string[];
  imageQualities: string[];
  languageModes: CommerceLanguageMode[];
  audioStrategies: Array<"native_av" | "external_audio">;
  audioRequirements: Array<"preferred" | "required" | "disabled">;
  languages: CommerceProjectLanguageOption[];
  modelRequirements: CommerceProjectModelRequirement[];
};

export type CommerceProductVersion = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  version: number;
  name: string;
  brand: string;
  sellingPoints: JsonValue;
  immutableFeatures: JsonValue;
  prohibitedClaims: JsonValue;
  factsSnapshot: JsonValue;
  factsHash: string;
  sourceVersionId?: string;
  createdAt: string;
};

export type CommerceProduct = {
  id: string;
  organizationId: string;
  projectId: string;
  currentVersionId?: string;
  status: string;
  revision: number;
  scriptUnitsRevision: number;
  metadata: JsonRecord;
  currentVersion?: CommerceProductVersion;
  createdAt: string;
  updatedAt: string;
};

export type CommerceProductMutationResult = {
  product: CommerceProduct;
  version: CommerceProductVersion;
  activated: boolean;
  requiresRebuild: boolean;
};

export type CommerceProductReference = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  artifactId: string;
  mediaFileId: string;
  referenceRole: string;
  ordinal: number;
  isPrimary: boolean;
  status: "active" | "archived" | string;
  width: number;
  height: number;
  mimeType: string;
  contentHash: string;
  qualityReview: JsonValue;
  revision: number;
  previewUrl?: string;
  createdAt: string;
  updatedAt: string;
  archivedAt?: string;
};

export type CommerceProductReferenceUpload = {
  uploadId: string;
  uploadUrl: string;
  method: string;
  headers: Record<string, string | string[]>;
  expiresAt: string;
};

export type CommerceProductReferencePackItem = {
  id: string;
  referencePackId: string;
  productReferenceId: string;
  ordinal: number;
  referenceRole: string;
  artifactId: string;
  mediaFileId: string;
  contentHash: string;
  previewUrl?: string;
  createdAt: string;
};

export type CommerceProductReferencePack = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  productVersionId: string;
  productFactsHash: string;
  referenceSetHash: string;
  packHash: string;
  status: "active" | "stale" | "archived";
  workflowRunId?: string;
  createdAt: string;
  staleAt?: string;
  archivedAt?: string;
  items: CommerceProductReferencePackItem[];
};

export type CommerceProductRebuildImpact = {
  projectId: string;
  projectGenerationId: string;
  productId: string;
  sourceProductVersionId: string;
  targetProductVersionId: string;
  expectedProductRevision: number;
  targetReferenceIds: string[];
  targetReferenceSetHash: string;
  impactToken: string;
  expiresAt: string;
  affectedUnits: Array<{
    scriptUnitId: string;
    unitNo: number;
    title: string;
    sourceUnitGenerationId: string;
    sourceReferencePackId: string;
  }>;
  reusableArtifactCount: number;
  blockers: string[];
};

export type CommerceProductRebuildResult = {
  rebuildId: string;
  status: string;
  productVersionId: string;
  referencePackId: string;
  affectedUnitCount: number;
  idempotentReplay: boolean;
};

export type CommerceScriptVersion = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  scriptUnitId: string;
  version: number;
  content: string;
  contentHash: string;
  sourceLanguageHint?: string;
  detectedSourceLanguage?: string;
  manualOverride: boolean;
  sourceVersionId?: string;
  createdAt: string;
};

export type CommerceLanguageResolution = {
  id: string;
  scriptUnitId: string;
  sourceScriptVersionId: string;
  languageMode: CommerceLanguageMode;
  sourceLanguage?: string;
  targetLanguage?: string;
  confidence?: number;
  reasoning: string;
  needsUserConfirmation: boolean;
  status: string;
  inputHash: string;
  revision: number;
  confirmedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type CommerceScriptLocalization = {
  id: string;
  scriptUnitId: string;
  sourceScriptVersionId: string;
  languageResolutionId: string;
  version: number;
  sourceLanguage: string;
  targetLanguage: string;
  localizedContent: string;
  localizedContentHash: string;
  structuredContract: JsonValue;
  estimatedVoiceoverSeconds: number;
  timingAnalysis: JsonValue;
  timingPolicyVersion: string;
  reviewStatus: string;
  reviewerOutput: JsonValue;
  status: string;
  revision: number;
  createdAt: string;
  approvedAt?: string;
  archivedAt?: string;
};

export type CommerceScriptUnit = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  unitNo: number;
  title: string;
  sortOrder: number;
  status: string;
  currentSourceVersionId?: string;
  currentLocalizationId?: string;
  languageMode: CommerceLanguageMode;
  explicitTargetLanguage?: string;
  targetDurationSeconds: number;
  targetPlatform: string;
  draftContent: string;
  draftContentHash?: string;
  currentContent: string;
  currentContentHash: string;
  draftUpdatedAt?: string;
  activeUnitGenerationId?: string;
  unitGenerationNo: number;
  storyboardStrategy?: CommerceStoryboardStrategy;
  derivedFromScriptUnitId?: string;
  derivationKind?: "copy" | "language_variant" | "agent_idea" | string;
  revision: number;
  metadata: JsonRecord;
  currentSourceVersion?: CommerceScriptVersion;
  currentLocalization?: CommerceScriptLocalization;
  languageResolution?: CommerceLanguageResolution;
  productionSummary?: CommerceScriptUnitProductionSummary;
  createdAt: string;
  updatedAt: string;
  archivedAt?: string;
};

export type CommerceScriptUnitProductionSummary = {
  status: string;
  currentStage: string;
  progress: number;
  failedCount: number;
  finalVideoStatus: string;
};

export type CommerceScriptUnitList = {
  items: CommerceScriptUnit[];
  nextCursor?: string;
  hasMore: boolean;
  scriptUnitsRevision: number;
};

export type CommerceScriptDerivationDimension =
  | "scene"
  | "hook"
  | "audience"
  | "tone"
  | "language"
  | "cta"
  | "custom";

export type CommerceScriptDerivationVariation = {
  ordinal?: number;
  key: string;
  label: string;
  brief: string;
};

export type CreateCommerceScriptDerivationRequest = {
  dimension: CommerceScriptDerivationDimension;
  instruction: string;
  preserve?: string[];
  variations: CommerceScriptDerivationVariation[];
};

export type CommerceScriptDerivationPromptBinding = {
  templateKey: string;
  promptVersionId: string;
  contentHash: string;
  metadata?: JsonRecord;
};

export type CommerceScriptDerivationPromptContract = {
  candidatePlanner: CommerceScriptDerivationPromptBinding;
  generator: CommerceScriptDerivationPromptBinding;
  reviewer: CommerceScriptDerivationPromptBinding;
  reviser: CommerceScriptDerivationPromptBinding;
};

export type CommerceScriptDerivationAttemptCall = {
  id: string;
  batchId: string;
  itemId: string;
  attemptId: string;
  roundNo: number;
  phase: "generate" | "review" | "revise";
  providerRequestId?: string;
  providerCallId?: string;
  modelProfileKey: string;
  modelProfileBindingId?: string;
  providerModelId?: string;
  promptTemplateKey: string;
  promptVersionId: string;
  promptHash: string;
  outputContentHash?: string;
  status: "running" | "succeeded" | "failed";
  errorCode?: string;
  errorMessage?: string;
  startedAt: string;
  completedAt?: string;
  createdAt: string;
};

export type CommerceScriptDerivationAttempt = {
  id: string;
  batchId: string;
  itemId: string;
  attemptNo: number;
  rootAttemptId?: string;
  retryOfAttemptId?: string;
  status: "queued" | "generating" | "reviewing" | "revising" | "succeeded" | "failed" | "cancelled";
  finalOutputContentHash?: string;
  reviewRound: number;
  reviewResult: JsonRecord;
  reviewFeedback?: string;
  errorCode?: string;
  errorMessage?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
  updatedAt: string;
  calls: CommerceScriptDerivationAttemptCall[];
};

export type CommerceScriptDerivationItemStatus =
  | "queued"
  | "running"
  | "reviewing"
  | "succeeded"
  | "failed_retryable"
  | "failed_terminal"
  | "cancelled";

export type CommerceScriptDerivationItem = {
  id: string;
  batchId: string;
  organizationId: string;
  projectId: string;
  productId: string;
  inputOrdinal: number;
  rootItemId?: string;
  retryOfItemId?: string;
  variationKey: string;
  variationLabel: string;
  variationBrief: string;
  inputSnapshot: JsonRecord;
  inputHash: string;
  reservedUnitNo: number;
  reservedSortOrder: number;
  status: CommerceScriptDerivationItemStatus;
  currentAttemptId?: string;
  outputScriptUnitId?: string;
  outputScriptVersionId?: string;
  errorCode?: string;
  errorMessage?: string;
  revision: number;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  updatedAt: string;
  attempts: CommerceScriptDerivationAttempt[];
};

export type CommerceScriptDerivationBatchStatus =
  | "queued"
  | "running"
  | "partial_succeeded"
  | "succeeded"
  | "failed"
  | "cancelling"
  | "cancelled";

export type CommerceScriptDerivationBatchSummary = {
  id: string;
  rootBatchId?: string;
  retryOfBatchId?: string;
  retryDepth: number;
  status: CommerceScriptDerivationBatchStatus;
  succeededCount: number;
  failedRetryableCount: number;
  failedTerminalCount: number;
  cancelledCount: number;
};

export type CommerceScriptDerivationLineageResult = {
  variationKey: string;
  variationLabel: string;
  rootItemId: string;
  latestResult: CommerceScriptDerivationItem;
  items: CommerceScriptDerivationItem[];
};

export type CommerceScriptDerivationBatch = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  sourceScriptUnitId: string;
  sourceContentSnapshot: string;
  sourceContentHash: string;
  productVersionId: string;
  productSnapshotHash: string;
  productionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  productionConfigurationHash: string;
  scriptModelProfileKey: string;
  modelProfileBindingId?: string;
  modelProfileBindingRevision: number;
  providerModelId?: string;
  routingSnapshotHash: string;
  promptContract: CommerceScriptDerivationPromptContract;
  dimension: CommerceScriptDerivationDimension;
  instruction: string;
  preserve: string[];
  variations: CommerceScriptDerivationVariation[];
  requestedCount: number;
  rootBatchId?: string;
  retryOfBatchId?: string;
  retryDepth: number;
  workflowRunId?: string;
  status: CommerceScriptDerivationBatchStatus;
  queuedCount: number;
  runningCount: number;
  succeededCount: number;
  failedRetryableCount: number;
  failedTerminalCount: number;
  cancelledCount: number;
  revision: number;
  createdBy?: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  cancelledAt?: string;
  updatedAt: string;
  items: CommerceScriptDerivationItem[];
  lineage: CommerceScriptDerivationBatchSummary[];
  lineageResults?: CommerceScriptDerivationLineageResult[];
};

export type CommerceScriptDerivationBatchList = {
  items: CommerceScriptDerivationBatch[];
  nextCursor?: string;
  hasMore: boolean;
};

export type CommerceScriptVersionMutation = {
  scriptUnit: CommerceScriptUnit;
  version: CommerceScriptVersion;
  activated: boolean;
  requiresRebuild: boolean;
};

export type CommerceDirectVideoInputSlot = {
  role: string;
  mediaType: string;
  semantics: string;
  min: number;
  max: number;
  ordered: boolean;
};

export type CommerceDirectVideoInputContract = {
  contractKey: string;
  requestMode: string;
  slots: CommerceDirectVideoInputSlot[];
  mutuallyExclusiveRoles?: string[][];
};

export type CommercePromptLengthConstraint = {
  maxLength: number;
  unit: "characters" | "utf8_bytes";
};

export type CommerceDirectVideoRoute = {
  routeKey: string;
  modelProfileId: string;
  modelProfileKey: string;
  modelProfileBindingId: string;
  providerModelId: string;
  providerAccountId: string;
  providerModelKey: string;
  priority: number;
  weight: number;
  variantKey: string;
  capabilitySnapshotHash: string;
  executableDurationSeconds: number[];
  resolutions: string[];
  aspectRatios: string[];
  promptConstraint: CommercePromptLengthConstraint;
  inputContract: CommerceDirectVideoInputContract;
  nativeAudio: {
    support: string;
    supportsDialogue?: boolean;
    supportsVoiceover?: boolean;
  };
};

export type CommerceDirectVideoOptions = {
  contractVersion: string;
  projectProductionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  videoProductionProfileVersionId: string;
  videoProductionProfileSnapshotHash: string;
  defaultAspectRatio: string;
  defaultResolution: string;
  defaultDurationSeconds: number;
  scriptPromptConstraint: CommercePromptLengthConstraint;
  executableDurationSeconds: number[];
  resolutions: string[];
  aspectRatios: string[];
  routes: CommerceDirectVideoRoute[];
};

export type CommerceScriptReferenceImage = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  scriptUnitId: string;
  artifactId: string;
  mediaFileId: string;
  fileName: string;
  mimeType: string;
  width: number;
  height: number;
  byteSize: number;
  contentHash: string;
  status: "active" | "archived";
  revision: number;
  previewUrl?: string;
  createdAt: string;
  updatedAt: string;
  archivedAt?: string;
};

export type CommerceDirectVideoReferenceSelection = {
  sourceType: "product" | "custom";
  sourceId: string;
};

export type CommerceDirectVideoReference = CommerceDirectVideoReferenceSelection & {
  id: string;
  productReferenceId?: string;
  scriptReferenceImageId?: string;
  artifactId: string;
  mediaFileId: string;
  mimeType: string;
  referenceRole: "first_frame" | "semantic_reference";
  ordinal: number;
  contentHash: string;
  sourceRevision: number;
  snapshot: JsonValue;
  previewUrl?: string;
};

export type CommerceDirectVideoJobStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelling"
  | "cancelled";

export type CommerceDirectVideoJob = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  productVersionId: string;
  scriptUnitId: string;
  scriptUnitRevision: number;
  projectProductionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  videoProfileVersionId: string;
  videoProfileSnapshotHash: string;
  modelProfileKey: string;
  modelProfileId?: string;
  modelProfileBindingId?: string;
  providerModelId?: string;
  providerAccountId?: string;
  providerModelKey: string;
  routeKey: string;
  variantKey: string;
  capabilitySnapshotHash: string;
  requestedDurationSeconds: number;
  aspectRatio: string;
  resolution: string;
  generateAudio: boolean;
  scriptSnapshot: string;
  scriptHash: string;
  productSnapshot: JsonValue;
  productSnapshotHash: string;
  executionContract: JsonValue;
  executionContractHash: string;
  referenceSetHash: string;
  promptHash: string;
  status: CommerceDirectVideoJobStatus;
  attemptGeneration: number;
  workflowRunId?: string;
  nodeRunId?: string;
  providerRequestId?: string;
  providerCallId?: string;
  providerAsyncTaskId?: string;
  externalTaskId?: string;
  outputArtifactId?: string;
  outputMediaFileId?: string;
  outputMimeType?: string;
  outputPreviewUrl?: string;
  errorCode?: string;
  errorMessage?: string;
  createdBy?: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  cancelledAt?: string;
  updatedAt: string;
  references: CommerceDirectVideoReference[];
};

export type CreateCommerceDirectVideoRequest = {
  durationSeconds?: number;
  resolution?: string;
  aspectRatio?: string;
  generateAudio?: boolean;
  references?: CommerceDirectVideoReferenceSelection[];
};

export type CommerceScriptUnitRebuildAffectedCounts = {
  storyboardPlans: number;
  storyboardShots: number;
  referenceImages: number;
  videoPrompts: number;
  shotVideos: number;
  timelines: number;
  finalVideos: number;
};

export type CommerceScriptUnitRebuildImpact = {
  projectId: string;
  projectGenerationId: string;
  scriptUnitId: string;
  sourceUnitGenerationId: string;
  sourceScriptVersionId: string;
  targetSourceScriptVersionId: string;
  expectedRevision: number;
  targetLanguageMode: CommerceLanguageMode;
  targetLanguage?: string;
  targetDurationSeconds: number;
  targetPlatform: string;
  targetStoryboardStrategy: CommerceStoryboardStrategy;
  targetConfigurationHash: string;
  impactToken: string;
  expiresAt: string;
  affected: CommerceScriptUnitRebuildAffectedCounts;
  estimatedAgentCalls: number;
  blockers: string[];
};

export type CommerceLanguageConfirmationAccepted = {
  languageResolution: CommerceLanguageResolution;
  workflowRun: WorkflowRun;
};

export type CommerceTimingEstimate = {
  locale: string;
  policyVersion: string;
  units: number;
  unitsPerSecond: number;
  estimatedVoiceoverSeconds: number;
  targetDurationSeconds: number;
  exceeded: boolean;
};

export type CommerceStoryboardPlanStatus = "planning" | "reviewing" | "ready" | "failed" | "stale" | "archived";

export type CommerceStoryboardStrategy = "smart" | "single_take" | "manual";

export type CommerceVideoExecutionRoute = {
  routeKey: string;
  modelProfileId: string;
  modelProfileKey: string;
  modelProfileBindingId: string;
  providerModelId: string;
  providerAccountId: string;
  modelKey: string;
  priority: number;
  weight: number;
  variantKey: string;
  capabilitySnapshotHash: string;
  executableDurationSeconds: number[];
  resolutions: string[];
  aspectRatios: string[];
  supportsContinuousExtension: boolean;
};

export type CommerceVideoExecutionEnvelope = {
  contractVersion: "commerce-video-execution-envelope/v1";
  projectProductionGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  videoProductionProfileVersionId: string;
  videoProductionProfileSnapshotHash: string;
  modelProfileKey: string;
  targetResolution: string;
  aspectRatio: string;
  routes: CommerceVideoExecutionRoute[];
  executableDurationSeconds: number[];
};

export type CommerceSegmentationShot = {
  shotOrdinal: number;
  beatOrdinals: number[];
  localizationSegmentIds: string[];
  sourceSegmentIds: string[];
  editDurationSeconds: number;
  requestedDurationSeconds: number;
  trimDurationSeconds: number;
  estimatedVoiceoverTicks: number;
  voiceoverOverflowTicks: number;
  timingAdvisoryLevel: "none" | "info" | "warning" | "critical";
  eligibleRouteKeys: string[];
  eligibleRouteSetHash: string;
  semanticBoundaryPenalty: number;
  continuityPenalty: number;
  complexityPenalty: number;
};

export type CommerceSegmentationPlan = {
  contractVersion: "commerce-storyboard-segmentation/v1";
  strategy: CommerceStoryboardStrategy;
  segmentationPolicyVersion: string;
  targetDurationSeconds: number;
  timelineTimebase: number;
  videoExecutionEnvelopeHash: string;
  shots: CommerceSegmentationShot[];
  totalRequestedSeconds: number;
  totalTrimSeconds: number;
  estimatedVoiceoverTicks: number;
  voiceoverOverflowTicks: number;
  timingAdvisoryLevel: "none" | "info" | "warning" | "critical";
};

export type CommerceStoryboardTimingAdvisory = {
  targetDurationSeconds: number;
  estimatedVoiceoverSeconds: number;
  voiceoverOverflowSeconds: number;
  exceeded: boolean;
  level: "none" | "info" | "warning" | "critical";
  message: string;
};

export type CommerceStoryboardPlanningPreview = {
  identity: JsonRecord;
  inputHash: string;
  storyboardStrategy: CommerceStoryboardStrategy;
  segmentationPolicyVersion: string;
  targetDurationSeconds: number;
  estimatedVoiceoverSeconds: number;
  voiceoverOverflowSeconds: number;
  voiceoverExceeded: boolean;
  providerDurationOptions: number[];
  recommendedShotCount: number;
  plannedEditDurations: number[];
  recommendedRequestDurations: number[];
  estimatedTrimSeconds: number;
  videoExecutionEnvelopeHash: string;
  segmentationPlanHash: string;
  previewHash: string;
  timingAdvisory: CommerceStoryboardTimingAdvisory;
  segmentation: CommerceSegmentationPlan;
};

export type CommerceStoryboardPlan = {
  id: string;
  organizationId: string;
  projectId: string;
  productId: string;
  productVersionId: string;
  scriptUnitId: string;
  sourceScriptVersionId: string;
  localizationId: string;
  referencePackId: string;
  projectProductionGenerationId: string;
  scriptUnitGenerationId: string;
  commerceWorkflowBindingId: string;
  commerceWorkflowBindingRevision: number;
  salesScriptContractId: string;
  salesScriptContractHash: string;
  workflowRunId?: string;
  planRevision: number;
  revision: number;
  status: CommerceStoryboardPlanStatus;
  active: boolean;
  staleState: "fresh" | "upstream_changed" | "needs_regeneration";
  targetLanguage: string;
  targetDurationSeconds: number;
  aspectRatio: string;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  allowedShotDurations: number[];
  storyboardStrategy?: CommerceStoryboardStrategy;
  segmentationPolicyVersion?: string;
  segmentationPlan?: CommerceSegmentationPlan;
  segmentationPlanHash?: string;
  videoExecutionEnvelope?: CommerceVideoExecutionEnvelope;
  videoExecutionEnvelopeHash?: string;
  timingAdvisory?: CommerceStoryboardTimingAdvisory;
  previewHash?: string;
  shotCount: number;
  reviewStatus: string;
  planHash: string;
  projectionHash: string;
  createdAt: string;
  activatedAt?: string;
};

export type CommerceStoryboardShotSegmentLink = {
  id: string;
  localizationSegmentId: string;
  sourceSegmentId: string;
  usage: "visual" | "voiceover" | "onscreen" | "cta" | "context";
  ordinal: number;
  verbatimStart?: number;
  verbatimEnd?: number;
};

export type CommerceStoryboardShotProductReference = {
  id: string;
  productReferenceId: string;
  sourcePackId: string;
  sourcePackItemId: string;
  role: "primary" | "detail" | "logo" | "usage" | "context";
  ordinal: number;
  required: boolean;
  artifactId: string;
  mediaFileId: string;
  contentHash: string;
  previewUrl?: string;
};

export type CommerceStoryboardShot = {
  id: string;
  storyboardPlanId: string;
  scriptUnitId: string;
  scriptUnitGenerationId: string;
  revision: number;
  shotOrdinal: number;
  title: string;
  durationSeconds: number;
  startTick: number;
  endTick: number;
  salesBeat: string;
  visualAction: string;
  shotPurpose: string;
  composition: string;
  camera: JsonRecord;
  voiceoverText: string;
  onscreenText: string;
  targetLanguage: string;
  soundEffects: JsonValue;
  musicCue: string;
  creativeDirection: JsonRecord;
  estimatedVoiceoverTicks: number;
  voiceoverOverflowTicks: number;
  timingAdvisoryLevel: "" | "none" | "info" | "warning" | "critical";
  recommendedRequestDurationSeconds: number;
  eligibleRouteSetHash: string;
  requiredProductFeatures: string[];
  reviewStatus: string;
  manualOverride: boolean;
  staleState: string;
  imagePrompt?: string;
  imagePromptStatus: string;
  imageStatus: string;
  imageArtifactId?: string;
  imagePreviewUrl?: string;
  videoPrompt?: string;
  videoPromptStatus: string;
  videoRenderPlanId?: string;
  videoRenderPlanStatus?: string;
  videoStatus: string;
  videoArtifactId?: string;
  videoPreviewUrl?: string;
  imageErrorCode?: string;
  imageErrorMessage?: string;
  videoErrorCode?: string;
  videoErrorMessage?: string;
  segmentLinks: CommerceStoryboardShotSegmentLink[];
  productReferences: CommerceStoryboardShotProductReference[];
  editedBy?: string;
  editedAt?: string;
};

export type CommerceStoryboardPlanDetail = {
  plan: CommerceStoryboardPlan;
  shots: CommerceStoryboardShot[];
};

export type CommerceUnitGenerationIdentity = {
  organizationId: string;
  projectId: string;
  projectGenerationId: string;
  videoProductionBindingId: string;
  videoProductionBindingRevision: number;
  videoProfileSnapshotHash: string;
  commerceWorkflowBindingId: string;
  commerceWorkflowBindingRevision: number;
  commerceConfigurationHash: string;
  productId: string;
  scriptUnitId: string;
  scriptUnitRevision: number;
  scriptUnitGenerationId: string;
  scriptUnitGenerationNo: number;
  unitConfigurationHash: string;
};

export type CommerceProductionRunType = "storyboard_plan" | "reference_images" | "video_prompts" | "shot_videos" | "final_compose";
export type CommerceProductionRunStatus = "queued" | "running" | "partially_succeeded" | "succeeded" | "failed" | "cancelling" | "cancelled";
export type CommerceProductionItemStatus = "queued" | "running" | "succeeded" | "failed_retryable" | "failed_terminal" | "cancelled" | "discarded" | "skipped";

export type CommerceVideoBatchRequest = {
  planId: string;
  expectedPlanRevision: number;
  expectedUnitGenerationId: string;
  shotIds: string[];
  force?: boolean;
  concurrency?: number;
  resolution?: string;
};

export type CommerceProductionRun = {
  id: string;
  identity: CommerceUnitGenerationIdentity;
  workflowRunId?: string;
  runType: CommerceProductionRunType;
  status: CommerceProductionRunStatus;
  payloadHash: string;
  inputSnapshot: JsonRecord;
  revision: number;
  totalItems: number;
  completedItems: number;
  failedItems: number;
  cancelledItems: number;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  errorCode?: string;
  errorMessage?: string;
};

export type CommerceProductionRunItem = {
  id: string;
  runId: string;
  identity: CommerceUnitGenerationIdentity;
  subject: {
    type: "plan_phase" | "candidate_shot" | "storyboard_shot" | "final_compose";
    key: string;
    storyboardShotId?: string;
    inputHash: string;
  };
  status: CommerceProductionItemStatus;
  currentAttempt: number;
  outputSnapshot: JsonRecord;
  outputArtifactId?: string;
  outputMediaFileId?: string;
  outputStoryboardPlanId?: string;
  outputVideoPromptPlanId?: string;
  outputVideoRenderPlanId?: string;
  outputFinalVideoVersionId?: string;
  providerRequestId?: string;
  providerCallId?: string;
  providerAsyncTaskId?: string;
  errorCode?: string;
  errorMessage?: string;
  retryable: boolean;
  startedAt?: string;
  completedAt?: string;
};

export type CommerceProductionRunDetail = {
  run: CommerceProductionRun;
  items: CommerceProductionRunItem[];
};

export type CommerceScriptUnitBatchStage = "storyboard" | "reference_images" | "video_prompts" | "shot_videos" | "final_compose";
export type CommerceScriptUnitBatchStatus = "queued" | "running" | "partially_succeeded" | "succeeded" | "failed" | "cancelling" | "cancelled";

export type CommerceScriptUnitBatchAdvanceItem = {
  scriptUnitId: string;
  expectedUnitGenerationId: string;
  planId?: string;
  expectedPlanRevision?: number;
  timelineId?: string;
  expectedTimelineRevision?: number;
  shotIds?: string[];
  force?: boolean;
  resolution?: string;
  title?: string;
};

export type CommerceScriptUnitBatchItem = {
  id: string;
  scriptUnitId: string;
  unitGenerationId: string;
  childRunId?: string;
  childWorkflowRunId?: string;
  ordinal: number;
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled" | "skipped";
  attemptGeneration: number;
  inputSnapshot: JsonRecord;
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  completedAt?: string;
};

export type CommerceScriptUnitBatch = {
  id: string;
  organizationId: string;
  projectId: string;
  projectGenerationId: string;
  targetStage: CommerceScriptUnitBatchStage;
  status: CommerceScriptUnitBatchStatus;
  maxConcurrency: number;
  retryOfCoordinatorId?: string;
  workflowRunId: string;
  totalItems: number;
  completedItems: number;
  failedItems: number;
  cancelledItems: number;
  revision: number;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  cancelledAt?: string;
  errorCode?: string;
  errorMessage?: string;
  items: CommerceScriptUnitBatchItem[];
};

export type CommerceProjectProductionStatus = {
  overall: {
    status: string;
    projectGenerationId?: string;
    commerceWorkflowBindingRevision: number;
    videoProductionBindingRevision: number;
    scriptUnitCount: number;
    completedScriptUnitCount: number;
    runningScriptUnitCount: number;
    failedScriptUnitCount: number;
    needsReviewScriptUnitCount: number;
  };
  product: {
    status: string;
    productVersionId?: string;
    referenceCount: number;
  };
  scriptUnitsRevision: number;
};

export type CommerceUnitProductionStatus = {
  scriptUnitId: string;
  unitNo: number;
  sortOrder: number;
  title: string;
  unitGenerationId?: string;
  unitGenerationNo: number;
  targetLanguage?: string;
  targetDurationSeconds: number;
  status: string;
  progress: number;
  nextAction: string;
  stages: {
    setup: { status: string; revision: number };
    language: { status: string; mode: string; sourceLanguage?: string; targetLanguage?: string; confidence?: number };
    script: { status: string; sourceVersion: number; localizationVersion: number };
    storyboard: { status: string; planId?: string; planRevision: number; shotCount: number };
    referenceImages: { total: number; succeeded: number; failed: number; running: number };
    videoPrompts: { total: number; approved: number; failed: number; running: number };
    shotVideos: { total: number; succeeded: number; failed: number; running: number };
    finalVideo: { status: string; timelineId?: string; finalVideoVersionId?: string };
  };
};

export type CommerceTimeline = {
  id: string;
  organizationId: string;
  projectId: string;
  projectGenerationId: string;
  scriptUnitId: string;
  unitGenerationId: string;
  workflowRunId?: string;
  revision: number;
  title: string;
  status: string;
  aspectRatio: string;
  resolution: string;
  timelineTimebase: number;
  fpsNumerator: number;
  fpsDenominator: number;
  metadata: JsonRecord;
  createdAt: string;
  updatedAt: string;
};

export type CommerceTimelineOverlay = {
  id: string;
  timelineId: string;
  timelineClipId?: string;
  storyboardShotId?: string;
  role: "onscreen_text" | "cta_end_card";
  ordinal: number;
  text: string;
  startTick: number;
  endTick: number;
  style: JsonRecord;
  contentHash: string;
};

export type CommerceFinalVideoVersion = FinalVideoVersion & {
  scriptUnitId: string;
  unitGenerationId: string;
};

export type CommerceTimelineDetail = {
  timeline: CommerceTimeline;
  clips: TimelineClipDetail[];
  overlays: CommerceTimelineOverlay[];
  finalVideoVersions: CommerceFinalVideoVersion[];
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

export type AgentImageAttachmentUpload = {
  attachmentId: string;
  uploadUrl: string;
  method: string;
  headers: Record<string, string | string[]>;
  expiresAt: string;
};

export type AgentImageAttachment = {
  id: string;
  projectId: string;
  fileName: string;
  mimeType: string;
  byteSize: number;
  width: number;
  height: number;
  contentHash: string;
  status: "pending" | "completed" | "abandoned";
  artifactId?: string;
  mediaFileId?: string;
  previewUrl?: string;
  createdAt: string;
  expiresAt: string;
  completedAt?: string;
};

export type AgentTaskImageAttachmentInput = {
  attachmentId: string;
  usage: "unspecified" | "product_common" | "script_custom" | "visual_reference";
};

export type AgentToolRisk = "read" | "draft" | "write" | "workflow" | "costed" | "destructive" | "admin" | string;

export type AgentPermissionMode = "require_approval" | "auto_approve" | "full_access";

export type AgentToolEffects = {
  maySpendProvider: boolean;
  startsWorkflow: boolean;
  writesProject: boolean;
  destructive: boolean;
};

export type AgentToolDescriptor = {
  name: string;
  label: string;
  description: string;
  risk: AgentToolRisk;
  permission?: string;
  permissions: string[];
  inputSchema: JsonRecord;
  requiresApproval: boolean;
  effects: AgentToolEffects;
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
  supportedInputLanguages: string[];
  supportedOutputLanguages: string[];
  supportedPromptLanguages: string[];
  supportedNativeAudioLanguages: string[];
  source: ProviderCapabilitySource;
  approvalStatus: ProviderCapabilityApprovalStatus;
};

export type ProviderCapabilitySource = "official" | "provider" | "preset" | "discovered" | "inferred" | "manual" | "unknown";

export type ProviderCapabilityApprovalStatus = "approved" | "inferred" | "rejected" | "unknown";

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
  defaultReasoningLevel?: string;
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
  supportedInputLanguages?: string[];
  supportedOutputLanguages?: string[];
  supportedPromptLanguages?: string[];
  supportedNativeAudioLanguages?: string[];
  capabilitySource?: ProviderCapabilitySource;
  capabilityApprovalStatus?: ProviderCapabilityApprovalStatus;
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

export type CreateSystemOrganizationMemberRequest =
  | { existingUserIdentifier: string }
  | {
      email: string;
      username: string;
      password: string;
      displayName?: string;
      avatarUrl?: string;
    };

export type UpdateSystemOrganizationMemberRequest = {
  email?: string;
  username?: string;
  password?: string;
  displayName?: string;
  avatarUrl?: string;
  status?: "active" | "disabled";
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
