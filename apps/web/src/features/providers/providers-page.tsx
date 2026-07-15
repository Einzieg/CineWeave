"use client";

import { useMemo, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import type {
  JsonRecord,
  JsonValue,
  ProviderAccount,
  ProviderCatalogEntry,
  ProviderCatalogModelTemplate,
  ProviderModel,
  ProviderModelCapability,
  ModelProfile,
  ModelProfileBinding,
  UpdateModelProfileBindingRequest,
} from "@/lib/types";
import { AppShell, SectionTitle, Surface } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  CheckCircle2,
  Edit2,
  Key,
  Layers3,
  Loader2,
  Plus,
  RefreshCw,
  Save,
  Sparkles,
  Trash2,
  X,
  XCircle,
  Zap,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { providerKeyLabel, reasoningLevelLabel, taskTypeLabel } from "@/lib/labels";

type AccountDialogMode = "create" | "edit";
type ModelDialogMode = "create" | "edit";
type ModelDraft = ProviderCatalogModelTemplate & { source: "catalog" | "custom" };

type ProviderModelWithAccount = ProviderModel & {
  accountId: string;
  accountName: string;
  providerLabel: string;
};

type BusinessProfileSlot = {
  profileKey: string;
  name: string;
  purpose: string;
  description: string;
  modalities: string[];
  taskTypes: string[];
};

type BusinessModelBindingDraft = {
  modelId: string;
  priority: string;
  weight: string;
  enabled: boolean;
  reasoningLevel: string;
};

type AccountForm = {
  name: string;
  baseUrl: string;
  authType: string;
  status: string;
  apiKey: string;
  setup: Record<string, string>;
  configText: string;
};

type CapabilityTruth = "unknown" | "true" | "false";

type VideoVariantForm = {
  variantKey: string;
  modelFamily: string;
  taskTypesText: string;
  referenceModesText: string;
  nativeAudioRequested: "any" | "true" | "false";
  durationMode: "continuous_range" | "discrete" | "fixed" | "source_duration";
  minDurationSeconds: string;
  maxDurationSeconds: string;
  durationStepSeconds: string;
  durationValuesText: string;
  resolutionsText: string;
  aspectRatiosText: string;
  frameRateMode: "fixed" | "selectable" | "unknown";
  frameRatesText: string;
  promptLanguagesText: string;
  nativeAudioSupport: CapabilityTruth;
  nativeAudioCanDisable: CapabilityTruth;
  supportsDialogue: CapabilityTruth;
  supportsVoiceover: CapabilityTruth;
  supportsAmbientSound: CapabilityTruth;
  supportsMusic: CapabilityTruth;
  supportsLipSync: CapabilityTruth;
  dialogueLanguagesText: string;
  audioTrackSeparable: boolean;
  supportsExtension: boolean;
  supportsFirstFrame: boolean;
  supportsLastFrame: boolean;
  supportsVideoReference: boolean;
  requestModesText: string;
  source: string;
  sourceUrl: string;
  capabilityVersion: string;
};

type ModelForm = {
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
  supportsAsyncTask: boolean;
  supportsStreaming: boolean;
  streamTerminalMode: "done_marker" | "finish_reason" | "done_or_finish_reason";
  supportsReasoning: boolean;
  supportsReasoningLevels: boolean;
  reasoningLevelsText: string;
  supportsMultimodalInput: boolean;
  maxInputTokens: string;
  maxOutputTokens: string;
  supportedInputTypesText: string;
  supportedOutputTypesText: string;
  promptMaxLength: string;
  promptLengthUnit: string;
  supportsReferenceImages: boolean;
  supportsImageEdit: boolean;
  maxReferenceImages: string;
  imageRequestModesText: string;
  imageAspectRatiosText: string;
  imageResolutionsText: string;
  imageQualityTiersText: string;
  imageResponseFormatsText: string;
  minDurationSeconds: string;
  maxDurationSeconds: string;
  durationsText: string;
  supportsFirstFrame: boolean;
  supportsLastFrame: boolean;
  supportsVideoReference: boolean;
  maxReferenceVideos: string;
  videoRequestModesText: string;
  videoAspectRatiosText: string;
  videoResolutionsText: string;
  videoOutputFormatsText: string;
  videoVariants: VideoVariantForm[];
  supportsTTS: boolean;
  supportsTranscription: boolean;
  audioVoicesText: string;
  audioLanguagesText: string;
  audioInputFormatsText: string;
  audioOutputFormatsText: string;
  audioRequestModesText: string;
  maxTTSCharacters: string;
  maxAudioDurationSeconds: string;
  taskTypesText: string;
  inputLimitsText: string;
  outputLimitsText: string;
  qualityTiersText: string;
  providerOptionsSchemaText: string;
  pricingPolicyText: string;
};

const emptyAccountForm: AccountForm = {
  name: "",
  baseUrl: "",
  authType: "bearer",
  status: "active",
  apiKey: "",
  setup: {},
  configText: "{}",
};

const defaultTaskTypesByModality: Record<string, string[]> = {
  text: ["text.generate", "text.stream"],
  image: ["image.generate"],
  video: ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
  audio: ["audio.tts", "audio.transcribe"],
  multimodal: ["text.generate", "text.stream", "image.generate", "video.create_task", "video.poll_task", "audio.tts", "audio.transcribe"],
};

const businessProfileSlots: BusinessProfileSlot[] = [
  {
    profileKey: "script_agent_default",
    name: "脚本/事件 Agent 默认模型",
    purpose: "script",
    description: "用于原文事件提取、改编计划、剧本生成、分场解析和审阅修复。",
    modalities: ["text", "multimodal"],
    taskTypes: ["text.generate", "text.stream"],
  },
  {
    profileKey: "image_generation_default",
    name: "图片生成默认模型",
    purpose: "image",
    description: "用于资产卡片、镜头参考图和分镜图片生成。",
    modalities: ["image", "multimodal"],
    taskTypes: ["image.generate"],
  },
  {
    profileKey: "video_generation_default",
    name: "视频生成默认模型",
    purpose: "video",
    description: "用于镜头视频任务创建、轮询、取消和最终视频生产链路。",
    modalities: ["video", "multimodal"],
    taskTypes: ["video.text_to_video", "video.image_to_video", "video.create_task", "video.poll_task", "video.cancel_task"],
  },
  {
    profileKey: "tts_generation_default",
    name: "角色配音默认模型",
    purpose: "audio_tts",
    description: "用于角色对白、旁白和解说的语音合成。",
    modalities: ["audio", "multimodal"],
    taskTypes: ["audio.tts"],
  },
  {
    profileKey: "audio_transcription_default",
    name: "音轨识别与审核默认模型",
    purpose: "audio_transcription",
    description: "用于原生音轨转写、对白覆盖率和说话人轮次审核。",
    modalities: ["audio", "multimodal"],
    taskTypes: ["audio.transcribe"],
  },
];

const routingStrategyOptions = [
  { value: "priority", label: "按优先级" },
  { value: "priority_with_fallback", label: "优先级 + 降级" },
  { value: "weighted", label: "按权重" },
  { value: "cost_optimized", label: "成本优先" },
  { value: "latency_optimized", label: "延迟优先" },
];

const modalityOptions = [
  { value: "text", label: "文本" },
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
  { value: "audio", label: "音频" },
  { value: "multimodal", label: "多模态" },
];

const statusOptions = [
  { value: "active", label: "启用" },
  { value: "disabled", label: "停用" },
];

const authTypeOptions = [
  { value: "bearer", label: "Bearer" },
  { value: "api_key", label: "API Key" },
  { value: "basic", label: "Basic" },
  { value: "none", label: "无认证" },
];

const providerCatalogRank: Record<string, number> = {
  openai_compatible_custom: 0,
  openrouter: 1,
  ollama: 2,
  google_gemini: 3,
  alibaba_dashscope: 4,
  zhipu_glm: 5,
  baidu_qianfan: 6,
  xunfei_spark: 7,
  minimax: 8,
};

function compareProviderCatalogEntries(a: ProviderCatalogEntry, b: ProviderCatalogEntry) {
  const rankA = providerCatalogRank[a.providerKey] ?? 100;
  const rankB = providerCatalogRank[b.providerKey] ?? 100;
  if (rankA !== rankB) {
    return rankA - rankB;
  }
  return a.displayName.localeCompare(b.displayName, "zh-Hans");
}

export function ProvidersPage() {
  const [selectedCatalogKey, setSelectedCatalogKey] = useState<string | null>(null);
  const [accountDialogMode, setAccountDialogMode] = useState<AccountDialogMode>("create");
  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState<ProviderAccount | null>(null);
  const [accountForm, setAccountForm] = useState<AccountForm>(emptyAccountForm);
  const [selectedTemplateModelKeys, setSelectedTemplateModelKeys] = useState<string[]>([]);
  const [pendingCustomModels, setPendingCustomModels] = useState<ModelDraft[]>([]);
  const [customModelName, setCustomModelName] = useState("");
  const [customModelModality, setCustomModelModality] = useState("text");
  const [modelsDialogOpen, setModelsDialogOpen] = useState(false);
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(null);
  const [modelDialogMode, setModelDialogMode] = useState<ModelDialogMode>("create");
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ProviderModel | null>(null);
  const [accountToDelete, setAccountToDelete] = useState<ProviderAccount | null>(null);
  const [modelForm, setModelForm] = useState<ModelForm>(emptyModelForm("text"));
  const [businessBindingDrafts, setBusinessBindingDrafts] = useState<Record<string, BusinessModelBindingDraft>>({});
  const invalidate = useInvalidateKeys();
  const lastDialogInnerPointerDownAtRef = useRef(0);
  const portaledControlOpenUntilRef = useRef(0);

  const { data: catalogData } = useApiQuery({
    key: qk.providerCatalog(),
    queryFn: (session) => studioApi.listProviderCatalog(session),
  });
  const catalogEntries = useMemo(() => [...((catalogData?.items || []) as ProviderCatalogEntry[])].sort(compareProviderCatalogEntries), [catalogData?.items]);

  const { data: accountsData, isLoading: accountsLoading } = useApiQuery({
    key: qk.providerAccounts(),
    queryFn: (session) => studioApi.listProviderAccounts(session),
  });
  const accounts = useMemo(
    () => ((accountsData?.items || []) as ProviderAccount[]).filter((account) => account.status !== "disabled"),
    [accountsData?.items],
  );

  const { data: profiles = [], isLoading: profilesLoading } = useApiQuery({
    key: qk.modelProfiles(),
    queryFn: (session) => studioApi.listModelProfiles(session).then((response) => response.items || []),
  });

  const selectedAccount = accounts.find((account) => account.id === selectedAccountId) || null;
  const selectedCatalogEntry = catalogEntries.find((entry) => entry.providerKey === selectedCatalogKey) || null;
  const selectedAccountCatalog = catalogEntries.find((entry) => entry.providerKey === selectedAccount?.connectorKey) || null;
  const catalogNameByKey = useMemo(
    () => new Map(catalogEntries.map((entry) => [entry.providerKey, entry.displayName || entry.name || providerKeyLabel(entry.providerKey)])),
    [catalogEntries],
  );
  const accountIdsKey = useMemo(() => accounts.map((account) => account.id).sort().join(","), [accounts]);
  const setupFields = catalogSetupFields(selectedCatalogEntry);
  const dialogModelTemplates = selectedCatalogEntry?.modelTemplates || [];
  const selectedCreateModelDrafts = [
    ...dialogModelTemplates
      .filter((template) => selectedTemplateModelKeys.includes(template.modelKey))
      .map((template) => ({ ...template, source: "catalog" as const })),
    ...pendingCustomModels,
  ];
  const modelTemplates = selectedAccountCatalog?.modelTemplates || [];

  const { data: modelsData, isLoading: modelsLoading } = useApiQuery({
    key: qk.providerModels(selectedAccountId || "none"),
    queryFn: (session) => studioApi.listProviderModels(session, selectedAccountId!).then((response) => response.items || []),
    enabled: !!selectedAccountId,
  });
  const models = useMemo(
    () => ((modelsData || []) as ProviderModel[]).filter((model) => model.status !== "disabled"),
    [modelsData],
  );
  const groupedModels = useMemo(() => groupModelsByModality(models), [models]);

  const { data: allProviderModels = [], isLoading: allProviderModelsLoading } = useApiQuery({
    key: qk.providerModelsAll(accountIdsKey || "none"),
    queryFn: async (session) => {
      const batches = await Promise.all(
        accounts.map(async (account) => {
          const response = await studioApi.listProviderModels(session, account.id);
          const providerLabel = catalogNameByKey.get(account.connectorKey || "") || providerKeyLabel(account.connectorKey || account.connectorId);
          return (response.items || []).map((model) => ({
            ...model,
            accountId: account.id,
            accountName: account.name,
            providerLabel,
          }));
        }),
      );
      return batches.flat() as ProviderModelWithAccount[];
    },
    enabled: accounts.length > 0,
  });

  const activeProviderModels = useMemo(
    () => allProviderModels.filter((model) => model.status !== "disabled"),
    [allProviderModels],
  );
  const modelById = useMemo(() => new Map(activeProviderModels.map((model) => [model.id, model])), [activeProviderModels]);
  const profileByKey = useMemo(() => new Map(profiles.map((profile) => [profile.profileKey, profile])), [profiles]);

  const createAccountMutation = useApiMutation({
    mutationFn: (session, data: { providerKey: string; body: JsonRecord }) =>
      studioApi.installProviderCatalogEntry(session, data.providerKey, data.body),
    onSuccess: (result) => {
      toast.success("供应商账号已创建");
      invalidate([qk.providerAccounts(), qk.providerCatalog(), qk.modelProfiles(), qk.providerModels(result.account.id), qk.providerModelsAll(accountIdsKey || "none")]);
    },
    onError: (error) => toast.error("创建失败：" + error.message),
  });

  const updateAccountMutation = useApiMutation({
    mutationFn: async (session, data: { accountId: string; body: JsonRecord; apiKey?: string }) => {
      const account = await studioApi.updateProviderAccount(session, data.accountId, data.body);
      if (data.apiKey?.trim()) {
        return studioApi.rotateProviderCredential(session, data.accountId, {
          credentialKey: "default",
          credential: { apiKey: data.apiKey.trim() },
        });
      }
      return account;
    },
    onSuccess: () => {
      toast.success("供应商账号已保存");
      invalidate([qk.providerAccounts(), qk.providerCatalog(), qk.modelProfiles(), qk.providerModelsAll(accountIdsKey || "none")]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const createProfileMutation = useApiMutation({
    mutationFn: (session, data: { slot: BusinessProfileSlot }) =>
      studioApi.createModelProfile(session, {
        profileKey: data.slot.profileKey,
        name: data.slot.name,
        purpose: data.slot.purpose,
        routingStrategy: "priority_with_fallback",
        fallbackStrategy: defaultFallbackStrategy(),
      }),
    onSuccess: () => {
      toast.success("业务模型配置已创建");
      invalidate([qk.modelProfiles()]);
    },
    onError: (error) => toast.error("创建业务模型配置失败：" + error.message),
  });

  const updateProfileMutation = useApiMutation({
    mutationFn: (session, data: { profile: ModelProfile; body: JsonRecord }) =>
      studioApi.updateModelProfile(session, data.profile.id, data.body),
    onSuccess: () => {
      toast.success("业务模型配置已更新");
      invalidate([qk.modelProfiles()]);
    },
    onError: (error) => toast.error("更新业务模型配置失败：" + error.message),
  });

  const createProfileBindingMutation = useApiMutation({
    mutationFn: async (
      session,
      data: {
        slot: BusinessProfileSlot;
        profile?: ModelProfile;
        providerModelId: string;
        priority: number;
        weight: number;
        enabled: boolean;
        reasoningLevel: string;
      },
    ) => {
      let profile = data.profile;
      if (!profile) {
        profile = await studioApi.createModelProfile(session, {
          profileKey: data.slot.profileKey,
          name: data.slot.name,
          purpose: data.slot.purpose,
          routingStrategy: "priority_with_fallback",
          fallbackStrategy: defaultFallbackStrategy(),
        });
      }
      return studioApi.createModelProfileBinding(session, profile.id, {
        providerModelId: data.providerModelId,
        priority: data.priority,
        weight: data.weight,
        enabled: data.enabled,
        runtimeOptions: data.reasoningLevel ? { reasoningLevel: data.reasoningLevel } : {},
      });
    },
    onSuccess: (_profile, data) => {
      toast.success("业务模型绑定已保存");
      setBusinessBindingDrafts((current) => ({
        ...current,
        [data.slot.profileKey]: defaultBusinessBindingDraft(),
      }));
      invalidate([qk.modelProfiles()]);
    },
    onError: (error) => toast.error("保存业务模型绑定失败：" + error.message),
  });

  const updateProfileBindingMutation = useApiMutation({
    mutationFn: (
      session,
      data: {
        profileId: string;
        bindingId: string;
        body: UpdateModelProfileBindingRequest;
      },
    ) => studioApi.updateModelProfileBinding(session, data.profileId, data.bindingId, data.body),
    onSuccess: () => {
      toast.success("业务模型绑定已更新");
      invalidate([qk.modelProfiles()]);
    },
    onError: (error) => toast.error("更新业务模型绑定失败：" + error.message),
  });

  const deleteProfileBindingMutation = useApiMutation({
    mutationFn: (session, data: { profileId: string; bindingId: string }) =>
      studioApi.deleteModelProfileBinding(session, data.profileId, data.bindingId),
    onSuccess: () => {
      toast.success("业务模型绑定已删除");
      invalidate([qk.modelProfiles()]);
    },
    onError: (error) => toast.error("删除业务模型绑定失败：" + error.message),
  });

  const deleteAccountMutation = useApiMutation({
    mutationFn: (session, accountId: string) => studioApi.deleteProviderAccount(session, accountId),
    onSuccess: (_result, accountId) => {
      toast.success("供应商已删除");
      invalidate([qk.providerAccounts(), qk.modelProfiles(), qk.providerModelsAll(accountIdsKey || "none")]);
      if (selectedAccountId === accountId) {
        setSelectedAccountId(null);
        setModelsDialogOpen(false);
        setModelDialogOpen(false);
      }
      setAccountToDelete(null);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const discoverModelsMutation = useApiMutation({
    mutationFn: (session, accountId: string) => studioApi.discoverProviderModels(session, accountId, {}),
    onSuccess: (result, accountId) => {
      toast.success(`已同步 ${result.models?.length || 0} 个远程模型`);
      invalidate([qk.providerModels(accountId), qk.providerModelsAll(accountIdsKey || "none"), qk.modelProfiles()]);
    },
    onError: (error) => toast.error("模型发现失败：" + error.message),
  });

  const quickCreateModelMutation = useApiMutation({
    mutationFn: (session, data: { accountId: string; model: ModelDraft | ProviderCatalogModelTemplate }) =>
      studioApi.createProviderModel(session, data.accountId, modelCreateBody(data.model)),
    onSuccess: (_result, data) => {
      invalidate([qk.providerModels(data.accountId), qk.providerModelsAll(accountIdsKey || "none"), qk.modelProfiles()]);
    },
    onError: (error) => toast.error("模型添加失败：" + error.message),
  });

  const saveModelMutation = useApiMutation({
    mutationFn: (session, data: { accountId: string; modelId?: string; body: JsonRecord }) => {
      if (data.modelId) {
        return studioApi.updateProviderModel(session, data.modelId, data.body);
      }
      return studioApi.createProviderModel(session, data.accountId, data.body);
    },
    onSuccess: () => {
      toast.success(modelDialogMode === "edit" ? "模型已保存" : "模型已添加");
      if (selectedAccountId) {
        invalidate([qk.providerModels(selectedAccountId), qk.providerModelsAll(accountIdsKey || "none")]);
      }
      setModelDialogOpen(false);
      setEditingModel(null);
    },
    onError: (error) => toast.error("模型保存失败：" + error.message),
  });

  const deleteModelMutation = useApiMutation({
    mutationFn: (session, modelId: string) => studioApi.deleteProviderModel(session, modelId),
    onSuccess: (_result, modelId) => {
      toast.success("模型已删除");
      if (selectedAccountId) {
        invalidate([qk.providerModels(selectedAccountId), qk.providerModelsAll(accountIdsKey || "none"), qk.modelProfiles()]);
      } else {
        invalidate([qk.modelProfiles()]);
      }
      if (editingModel?.id === modelId) {
        setEditingModel(null);
        setModelDialogOpen(false);
      }
    },
    onError: (error) => toast.error("模型删除失败：" + error.message),
  });

  const testModelMutation = useApiMutation({
    mutationFn: (session, modelId: string) =>
      studioApi.testProviderModel(session, modelId, {
        testType: "connection_test",
        input: { messages: [{ role: "user", content: "ping" }], maxTokens: 16 },
      }),
    onSuccess: (result) => {
      if (result.status === "succeeded") {
        toast.success("模型测试成功");
      } else {
        toast.error(result.errorMessage || "模型测试失败");
      }
    },
    onError: (error) => toast.error("模型测试失败：" + error.message),
  });

  function openCreateAccountDialog() {
    const preferred = catalogEntries.find((entry) => entry.providerKey === "openai_compatible_custom") || catalogEntries[0] || null;
    setAccountDialogMode("create");
    setEditingAccount(null);
    setSelectedAccountId(null);
    setSelectedCatalogKey(preferred?.providerKey || null);
    setAccountForm(accountFormFromCatalog(preferred));
    setSelectedTemplateModelKeys((preferred?.modelTemplates || []).map((template) => template.modelKey));
    setPendingCustomModels([]);
    setCustomModelName("");
    setCustomModelModality("text");
    setAccountDialogOpen(true);
  }

  function openEditAccountDialog(account: ProviderAccount) {
    setAccountDialogMode("edit");
    setEditingAccount(account);
    setSelectedAccountId(account.id);
    setSelectedCatalogKey(account.connectorKey || null);
    setAccountForm({
      name: account.name || "",
      baseUrl: account.baseUrl || "",
      authType: account.authType || "bearer",
      status: account.status || "active",
      apiKey: "",
      setup: {},
      configText: jsonText(account.config || {}),
    });
    setSelectedTemplateModelKeys([]);
    setPendingCustomModels([]);
    setCustomModelName("");
    setCustomModelModality("text");
    setAccountDialogOpen(true);
  }

  function closeAccountDialog() {
    setAccountDialogOpen(false);
    setEditingAccount(null);
    setSelectedCatalogKey(null);
    setAccountForm(emptyAccountForm);
    setSelectedTemplateModelKeys([]);
    setPendingCustomModels([]);
    setCustomModelName("");
    setCustomModelModality("text");
  }

  function trackPortaledControlOpen(open: boolean) {
    const now = Date.now();
    portaledControlOpenUntilRef.current = open ? Number.MAX_SAFE_INTEGER : now + 500;
  }

  function markDialogInnerPointerDown(event: ReactPointerEvent<HTMLElement>) {
    const target = event.target;
    if (!(target instanceof Element) || target.closest("[data-slot='dialog-close']")) {
      return;
    }
    lastDialogInnerPointerDownAtRef.current = Date.now();
  }

  function shouldIgnoreDialogCloseFromPortaledControl() {
    const now = Date.now();
    return now - lastDialogInnerPointerDownAtRef.current < 500 && now <= portaledControlOpenUntilRef.current;
  }

  function handleAccountDialogOpenChange(open: boolean) {
    if (open) {
      setAccountDialogOpen(true);
      return;
    }
    if (shouldIgnoreDialogCloseFromPortaledControl()) {
      return;
    }
    closeAccountDialog();
  }

  function handleModelsDialogOpenChange(open: boolean) {
    if (open) {
      setModelsDialogOpen(true);
      return;
    }
    if (shouldIgnoreDialogCloseFromPortaledControl()) {
      return;
    }
    setModelsDialogOpen(false);
  }

  function handleModelDialogOpenChange(open: boolean) {
    if (open) {
      setModelDialogOpen(true);
      return;
    }
    if (shouldIgnoreDialogCloseFromPortaledControl()) {
      return;
    }
    setModelDialogOpen(false);
  }

  function handleCatalogChange(providerKey: string) {
    const entry = catalogEntries.find((item) => item.providerKey === providerKey) || null;
    setSelectedCatalogKey(providerKey);
    setAccountForm((current) => ({
      ...current,
      baseUrl: entry?.defaultBaseUrl || "",
      authType: entry?.defaultAuthType || "bearer",
      setup: defaultSetupValues(entry),
      configText: jsonText(entry?.setupSchema?.defaultConfig || {}),
    }));
    setSelectedTemplateModelKeys((entry?.modelTemplates || []).map((template) => template.modelKey));
    setPendingCustomModels([]);
    setCustomModelName("");
    setCustomModelModality("text");
  }

  function buildCreateAccountPayload() {
    if (!selectedCatalogEntry) {
      toast.error("请选择供应商类型");
      return null;
    }
    if (!accountForm.name.trim() || (accountForm.authType !== "none" && !accountForm.apiKey.trim())) {
      toast.error("请填写账号名称和 API Key");
      return null;
    }
    const missing = setupFields.find((field) => field.required && !String(accountForm.setup[field.key] ?? "").trim());
    if (missing) {
      toast.error(`请填写${missing.label || missing.key}`);
      return null;
    }
    const config = parseJsonRecord(accountForm.configText, "账号配置");
    if (!config) {
      return null;
    }
    if (selectedCreateModelDrafts.length === 0) {
      toast.error("请至少保留一个模型");
      return null;
    }
    const setup = setupFields.reduce<Record<string, JsonValue>>((acc, field) => {
      const defaultValue = field.defaultValue ?? selectedCatalogEntry.setupSchema?.defaultConfig?.[field.key] ?? "";
      acc[field.key] = accountForm.setup[field.key] || String(defaultValue ?? "");
      return acc;
    }, {});
    return {
      providerKey: selectedCatalogEntry.providerKey,
      body: {
        name: accountForm.name.trim(),
        baseUrl: accountForm.baseUrl.trim() || selectedCatalogEntry.defaultBaseUrl || "",
        authType: accountForm.authType || selectedCatalogEntry.defaultAuthType || "bearer",
        apiKey: accountForm.apiKey.trim(),
        setup,
        config,
        models: selectedCreateModelDrafts.map(catalogInstallModelBody),
      } satisfies JsonRecord,
    };
  }

  function buildUpdateAccountPayload() {
    if (!editingAccount) {
      toast.error("请选择要编辑的供应商账号");
      return null;
    }
    if (!accountForm.name.trim()) {
      toast.error("请填写账号名称");
      return null;
    }
    const config = parseJsonRecord(accountForm.configText, "账号配置");
    if (!config) {
      return null;
    }
    return {
      accountId: editingAccount.id,
      body: {
        name: accountForm.name.trim(),
        baseUrl: accountForm.baseUrl.trim(),
        authType: accountForm.authType,
        status: accountForm.status,
        config,
      },
      apiKey: accountForm.apiKey,
    };
  }

  async function createAccountFromDialog() {
    const payload = buildCreateAccountPayload();
    if (!payload) {
      return null;
    }
    try {
      const result = await createAccountMutation.mutateAsync(payload);
      setAccountDialogMode("edit");
      setEditingAccount(result.account);
      setSelectedAccountId(result.account.id);
      setSelectedCatalogKey(result.account.connectorKey || payload.providerKey);
      setAccountForm((current) => ({
        ...current,
        name: result.account.name || current.name,
        baseUrl: result.account.baseUrl || current.baseUrl,
        authType: result.account.authType || current.authType,
        status: result.account.status || "active",
        apiKey: "",
        setup: {},
        configText: jsonText(result.account.config || {}),
      }));
      setSelectedTemplateModelKeys([]);
      setPendingCustomModels([]);
      return result.account;
    } catch {
      return null;
    }
  }

  async function ensureDialogAccount() {
    if (accountDialogMode === "edit" && editingAccount) {
      return editingAccount;
    }
    return createAccountFromDialog();
  }

  async function handleSaveAccount() {
    if (accountDialogMode === "create") {
      const account = await createAccountFromDialog();
      if (account) {
        closeAccountDialog();
      }
      return;
    }

    const payload = buildUpdateAccountPayload();
    if (!payload) {
      return;
    }
    try {
      await updateAccountMutation.mutateAsync(payload);
      closeAccountDialog();
    } catch {
      return;
    }
  }

  async function handleDiscoverModelsInAccountDialog() {
    const account = await ensureDialogAccount();
    if (!account) {
      return;
    }
    setSelectedAccountId(account.id);
    try {
      await discoverModelsMutation.mutateAsync(account.id);
    } catch {
      return;
    }
  }

  async function handleFillTemplateModels() {
    if (dialogModelTemplates.length === 0) {
      toast.error("当前供应商没有预设模型");
      return;
    }
    if (accountDialogMode === "create") {
      setSelectedTemplateModelKeys(dialogModelTemplates.map((template) => template.modelKey));
      toast.success("已填入预设模型");
      return;
    }
    if (!selectedAccountId) {
      toast.error("请选择供应商账号");
      return;
    }
    const existingKeys = new Set(models.map((model) => model.modelKey));
    const missingTemplates = dialogModelTemplates.filter((template) => !existingKeys.has(template.modelKey));
    if (missingTemplates.length === 0) {
      toast.success("预设模型已存在");
      return;
    }
    try {
      for (const template of missingTemplates) {
        await quickCreateModelMutation.mutateAsync({ accountId: selectedAccountId, model: template });
      }
      toast.success(`已添加 ${missingTemplates.length} 个预设模型`);
    } catch {
      return;
    }
  }

  async function handleAddCustomModel() {
    const modelKey = customModelName.trim();
    if (!modelKey) {
      toast.error("请填写自定义模型名称");
      return;
    }
    const model = customModelDraft(modelKey, customModelModality);
    if (accountDialogMode === "create") {
      const duplicate = selectedCreateModelDrafts.some((item) => item.modelKey === model.modelKey);
      if (duplicate) {
        toast.error("模型已存在");
        return;
      }
      setPendingCustomModels((current) => [...current, model]);
      setCustomModelName("");
      toast.success("模型已填入");
      return;
    }
    if (!selectedAccountId) {
      toast.error("请选择供应商账号");
      return;
    }
    if (models.some((item) => item.modelKey === model.modelKey)) {
      toast.error("模型已存在");
      return;
    }
    try {
      await quickCreateModelMutation.mutateAsync({ accountId: selectedAccountId, model });
      setCustomModelName("");
      toast.success("模型已添加");
    } catch {
      return;
    }
  }

  function handleRemoveCreateModel(modelKey: string) {
    setSelectedTemplateModelKeys((current) => current.filter((key) => key !== modelKey));
    setPendingCustomModels((current) => current.filter((model) => model.modelKey !== modelKey));
  }

  function openDetailedModelsFromAccountDialog() {
    const account = editingAccount;
    if (!account) {
      return;
    }
    closeAccountDialog();
    openModelsDialog(account);
  }

  function openModelsDialog(account: ProviderAccount) {
    setSelectedAccountId(account.id);
    setModelsDialogOpen(true);
  }

  function openCreateModelDialog(template?: ProviderCatalogModelTemplate) {
    setModelDialogMode("create");
    setEditingModel(null);
    setModelForm(template ? modelFormFromTemplate(template) : emptyModelForm("text"));
    setModelDialogOpen(true);
  }

  function openEditModelDialog(model: ProviderModel) {
    setModelDialogMode("edit");
    setEditingModel(model);
    setModelForm(modelFormFromModel(model));
    setModelDialogOpen(true);
  }

  function handleSaveModel() {
    if (!selectedAccountId) {
      toast.error("请选择供应商账号");
      return;
    }
    if (!modelForm.modelKey.trim() || !modelForm.displayName.trim() || !modelForm.modality.trim()) {
      toast.error("请填写模型 ID、名称和类型");
      return;
    }
    const taskTypes = taskTypesFromText(modelForm.taskTypesText);
    if (taskTypes.length === 0) {
      toast.error("请至少填写一个任务类型");
      return;
    }
    const capability = buildCapabilityFromModelForm(modelForm, taskTypes);
    if (!capability) {
      return;
    }
    saveModelMutation.mutate({
      accountId: selectedAccountId,
      modelId: editingModel?.id,
      body: {
        modelKey: modelForm.modelKey.trim(),
        displayName: modelForm.displayName.trim(),
        modality: modelForm.modality,
        status: modelForm.status,
        capabilities: capability,
      },
    });
  }

  function updateBusinessBindingDraft(profileKey: string, patch: Partial<BusinessModelBindingDraft>) {
    setBusinessBindingDrafts((current) => ({
      ...current,
      [profileKey]: {
        ...defaultBusinessBindingDraft(),
        ...(current[profileKey] || {}),
        ...patch,
      },
    }));
  }

  function handleCreateBusinessProfile(slot: BusinessProfileSlot) {
    if (profileByKey.has(slot.profileKey)) {
      toast.success("业务模型配置已存在");
      return;
    }
    createProfileMutation.mutate({ slot });
  }

  function handleSaveBusinessBinding(slot: BusinessProfileSlot, profile?: ModelProfile) {
    const draft = businessBindingDrafts[slot.profileKey] || defaultBusinessBindingDraft();
    if (!draft.modelId) {
      toast.error("请选择供应商模型");
      return;
    }
    const model = activeProviderModels.find((item) => item.id === draft.modelId);
    if (!model) {
      toast.error("选择的模型不可用");
      return;
    }
    if (!modelMatchesBusinessSlot(model, slot)) {
      toast.error("所选模型类型不匹配当前业务模型");
      return;
    }
    const priority = parseIntegerOrDefault(draft.priority, 100, "优先级");
    const weight = parseIntegerOrDefault(draft.weight, 100, "权重");
    if (priority === null || weight === null) {
      return;
    }
    createProfileBindingMutation.mutate({
      slot,
      profile,
      providerModelId: draft.modelId,
      priority,
      weight,
      enabled: draft.enabled,
      reasoningLevel: draft.reasoningLevel,
    });
  }

  function handleUpdateBusinessRouting(profile: ModelProfile, routingStrategy: string) {
    updateProfileMutation.mutate({
      profile,
      body: {
        profileKey: profile.profileKey,
        name: profile.name || profile.profileKey,
        purpose: profile.purpose || profile.profileKey,
        routingStrategy,
        fallbackStrategy: profile.fallbackStrategy || defaultFallbackStrategy(),
      },
    });
  }

  const accountDialogSaving = createAccountMutation.isPending || updateAccountMutation.isPending;
  const accountDialogModelActionPending =
    discoverModelsMutation.isPending || quickCreateModelMutation.isPending || createAccountMutation.isPending;
  const accountDialogModels = accountDialogMode === "create" ? selectedCreateModelDrafts : models;

  return (
    <AppShell active="providers" title="供应商中心" description="管理 AI 供应商账号与模型配置">
      <Surface>
        <SectionTitle title="供应商管理" description="配置 AI 模型供应商、账号凭证和模型路由策略" />

        <Tabs defaultValue="accounts" className="p-4">
          <TabsList>
            <TabsTrigger value="accounts">
              供应商账号
              <Badge variant="secondary" className="ml-2">{accounts.length}</Badge>
            </TabsTrigger>
            <TabsTrigger value="profiles">
              模型配置
              <Badge variant="secondary" className="ml-2">{profiles.length}</Badge>
            </TabsTrigger>
          </TabsList>

          <TabsContent value="accounts" className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <p className="text-sm text-muted-foreground">管理组织的供应商账号、访问密钥和可用模型</p>
              <Button onClick={openCreateAccountDialog}>
                <Plus className="h-4 w-4" />
                添加供应商
              </Button>
            </div>

            {accountsLoading && <Skeleton className="h-64" />}

            {!accountsLoading && accounts.length === 0 && (
              <div className="rounded-lg border border-dashed p-12 text-center">
                <Key className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
                <p className="mt-4 text-sm text-muted-foreground">暂无供应商账号</p>
                <p className="mt-1 text-xs text-muted-foreground">添加供应商后即可配置模型</p>
              </div>
            )}

            <div className="space-y-3">
              {accounts.map((account) => (
                <div
                  key={account.id}
                  className="rounded-lg border p-4 transition hover:bg-muted/50"
                  data-provider-account-id={account.id}
                  data-testid="provider-account-card"
                >
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Key className="h-4 w-4 text-muted-foreground" />
                        <span className="font-medium">{account.name}</span>
                        <Badge variant="outline">{catalogNameByKey.get(account.connectorKey || "") || providerKeyLabel(account.connectorKey || account.connectorId)}</Badge>
                        {account.status === "active" ? (
                          <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                        ) : (
                          <XCircle className="h-4 w-4 text-rose-500" />
                        )}
                      </div>
                      <div className="mt-2 grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
                        <span className="truncate">Base URL: {account.baseUrl || "未设置"}</span>
                        <span>认证类型: {account.authType || "bearer"}</span>
                        <span>状态: {statusLabel(account.status)}</span>
                        <span>密钥: {account.credentialPreview || "未保存"}</span>
                      </div>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        data-provider-account-id={account.id}
                        data-testid="provider-account-models"
                        onClick={() => openModelsDialog(account)}
                      >
                        <Layers3 className="h-3.5 w-3.5" />
                        管理模型
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        data-provider-account-id={account.id}
                        data-testid="provider-account-edit"
                        onClick={() => openEditAccountDialog(account)}
                      >
                        <Edit2 className="h-3.5 w-3.5" />
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        data-provider-account-id={account.id}
                        data-testid="provider-account-discover"
                        onClick={() => discoverModelsMutation.mutate(account.id)}
                        disabled={discoverModelsMutation.isPending}
                      >
                        <RefreshCw className={cn("h-3.5 w-3.5", discoverModelsMutation.isPending && "animate-spin")} />
                        发现模型
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        data-provider-account-id={account.id}
                        data-testid="provider-account-delete"
                        onClick={() => setAccountToDelete(account)}
                        disabled={deleteAccountMutation.isPending}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                        删除
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </TabsContent>

          <TabsContent value="profiles" className="space-y-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="space-y-1">
                <p className="text-sm text-muted-foreground">把业务链路使用的默认模型槽位绑定到具体供应商模型。</p>
                <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                  <Badge variant="outline">脚本 Agent</Badge>
                  <Badge variant="outline">图片生成</Badge>
                  <Badge variant="outline">视频生成</Badge>
                </div>
              </div>
              <div className="text-xs text-muted-foreground">
                可用模型 {activeProviderModels.length}
              </div>
            </div>

            {(profilesLoading || allProviderModelsLoading) && <Skeleton className="h-64" />}

            {!profilesLoading && !allProviderModelsLoading && activeProviderModels.length === 0 && (
              <div className="rounded-lg border border-dashed p-10 text-center">
                <Zap className="mx-auto h-10 w-10 text-muted-foreground opacity-50" />
                <p className="mt-3 text-sm text-muted-foreground">还没有可用供应商模型</p>
                <p className="mt-1 text-xs text-muted-foreground">请先在“供应商账号”中添加供应商、发现模型或手动添加模型。</p>
              </div>
            )}

            <div className="grid gap-4">
              {businessProfileSlots.map((slot) => {
                const profile = profileByKey.get(slot.profileKey);
                const draft = businessBindingDrafts[slot.profileKey] || defaultBusinessBindingDraft();
                const compatibleModels = activeProviderModels.filter((model) => modelMatchesBusinessSlot(model, slot));
                const selectedDraftModel = modelById.get(draft.modelId);
                const selectedDraftReasoningLevels = selectedDraftModel ? providerModelReasoningLevels(selectedDraftModel) : [];
                const bindings = profile?.bindings || [];
                return (
                  <div key={slot.profileKey} className="rounded-lg border p-4">
                    <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                      <div className="min-w-0 space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <h3 className="font-semibold">{profile?.name || slot.name}</h3>
                          <Badge variant="outline">{businessProfilePurposeLabel(slot.purpose)}</Badge>
                          <Badge variant={profile ? "default" : "secondary"}>{profile ? "已创建" : "未创建"}</Badge>
                        </div>
                        <div className="font-mono text-xs text-muted-foreground">{slot.profileKey}</div>
                        <p className="max-w-3xl text-sm text-muted-foreground">{slot.description}</p>
                        <div className="flex flex-wrap gap-1">
                          {slot.taskTypes.map((taskType) => (
                            <span key={taskType} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                              {taskTypeLabel(taskType)}
                            </span>
                          ))}
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        {profile ? (
                          <Select value={profile.routingStrategy || "priority_with_fallback"} onValueChange={(value) => handleUpdateBusinessRouting(profile, value)}>
                            <SelectTrigger className="w-44">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {routingStrategyOptions.map((option) => (
                                <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        ) : (
                          <Button size="sm" variant="outline" onClick={() => handleCreateBusinessProfile(slot)} disabled={createProfileMutation.isPending}>
                            <Plus className="h-3.5 w-3.5" />
                            创建配置
                          </Button>
                        )}
                      </div>
                    </div>

                    <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
                      <div className="space-y-2">
                        <div className="text-xs font-medium text-muted-foreground">当前绑定</div>
                        {bindings.length === 0 ? (
                          <div className="rounded-md border border-dashed p-6 text-sm text-muted-foreground">尚未绑定供应商模型</div>
                        ) : (
                          <div className="grid gap-2">
                            {bindings.map((binding) => (
                              <BusinessBindingRow
                                key={`${binding.id}:${binding.priority}:${binding.weight}:${binding.enabled}:${binding.runtimeOptions?.reasoningLevel || ""}`}
                                binding={binding}
                                model={modelById.get(binding.providerModelId)}
                                onDelete={() =>
                                  profile &&
                                  deleteProfileBindingMutation.mutate({
                                    profileId: profile.id,
                                    bindingId: binding.id,
                                  })
                                }
                                onUpdate={(body) =>
                                  profile
                                    ? updateProfileBindingMutation
                                        .mutateAsync({ profileId: profile.id, bindingId: binding.id, body })
                                        .then(() => undefined)
                                    : Promise.reject(new Error("业务模型配置不存在"))
                                }
                                deleting={
                                  deleteProfileBindingMutation.isPending &&
                                  deleteProfileBindingMutation.variables?.bindingId === binding.id
                                }
                              />
                            ))}
                          </div>
                        )}
                      </div>

                      <div className="space-y-3 rounded-md border bg-muted/30 p-3">
                        <div className="text-xs font-medium text-muted-foreground">添加或更新绑定</div>
                        <div className="space-y-1.5">
                          <Label>供应商模型</Label>
                          <Select
                            value={draft.modelId}
                            onValueChange={(value) => {
                              const selectedModel = modelById.get(value);
                              const levels = selectedModel ? providerModelReasoningLevels(selectedModel) : [];
                              updateBusinessBindingDraft(slot.profileKey, {
                                modelId: value,
                                reasoningLevel: levels.includes(draft.reasoningLevel) ? draft.reasoningLevel : "",
                              });
                            }}
                            onOpenChange={trackPortaledControlOpen}
                            disabled={compatibleModels.length === 0}
                          >
                            <SelectTrigger>
                              <SelectValue placeholder="选择模型" />
                            </SelectTrigger>
                            <SelectContent>
                              {compatibleModels.map((model) => (
                                <SelectItem key={model.id} value={model.id}>
                                  {model.displayName || model.modelKey} · {model.accountName}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                        {selectedDraftReasoningLevels.length > 0 && (
                          <div className="space-y-1.5">
                            <Label>默认思考等级</Label>
                            <Select
                              value={draft.reasoningLevel || "__provider_default__"}
                              onValueChange={(value) => updateBusinessBindingDraft(slot.profileKey, {
                                reasoningLevel: value === "__provider_default__" ? "" : value,
                              })}
                            >
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="__provider_default__">供应商默认</SelectItem>
                                {selectedDraftReasoningLevels.map((level) => (
                                  <SelectItem key={level} value={level}>{reasoningLevelLabel(level)}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>
                        )}
                        <div className="grid grid-cols-2 gap-3">
                          <div className="space-y-1.5">
                            <Label>优先级</Label>
                            <Input
                              inputMode="numeric"
                              value={draft.priority}
                              onChange={(event) => updateBusinessBindingDraft(slot.profileKey, { priority: event.target.value })}
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label>权重</Label>
                            <Input
                              inputMode="numeric"
                              value={draft.weight}
                              onChange={(event) => updateBusinessBindingDraft(slot.profileKey, { weight: event.target.value })}
                            />
                          </div>
                        </div>
                        <div className="flex items-center justify-between gap-3 rounded-md border bg-background p-3">
                          <Label>启用绑定</Label>
                          <Switch
                            checked={draft.enabled}
                            onCheckedChange={(checked) => updateBusinessBindingDraft(slot.profileKey, { enabled: checked })}
                          />
                        </div>
                        <Button
                          className="w-full"
                          onClick={() => handleSaveBusinessBinding(slot, profile)}
                          disabled={createProfileBindingMutation.isPending || compatibleModels.length === 0}
                        >
                          保存绑定
                        </Button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </TabsContent>
        </Tabs>
      </Surface>

      <Dialog open={accountDialogOpen} onOpenChange={handleAccountDialogOpenChange} modal={true}>
        <DialogContent
          className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-3xl"
          onInteractOutside={preventDialogCloseFromPortaledControl}
          onPointerDownCapture={markDialogInnerPointerDown}
        >
          <DialogHeader>
            <DialogTitle>{accountDialogMode === "create" ? "添加供应商" : "编辑供应商"}</DialogTitle>
            <DialogDescription>
              {accountDialogMode === "create" ? "创建供应商账号并安装模型预设" : "修改账号连接信息、状态和密钥"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {accountDialogMode === "create" && (
              <div className="space-y-1.5">
                <Label>供应商类型</Label>
                <Select value={selectedCatalogKey || ""} onValueChange={handleCatalogChange} onOpenChange={trackPortaledControlOpen}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择供应商类型" />
                  </SelectTrigger>
                  <SelectContent>
                    {catalogEntries.map((entry) => (
                      <SelectItem key={entry.providerKey} value={entry.providerKey}>
                        {entry.displayName || entry.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            <div className="grid gap-3 md:grid-cols-2">
              <div className="space-y-1.5">
                <Label>账号名称</Label>
                <Input
                  value={accountForm.name}
                  onChange={(event) => setAccountForm({ ...accountForm, name: event.target.value })}
                  placeholder="例如：火山方舟主账号"
                />
              </div>
              <div className="space-y-1.5">
                <Label>认证类型</Label>
                <Select
                  value={accountForm.authType}
                  onValueChange={(value) => setAccountForm({ ...accountForm, authType: value })}
                  onOpenChange={trackPortaledControlOpen}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {authTypeOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label>Base URL</Label>
              <Input
                value={accountForm.baseUrl}
                onChange={(event) => setAccountForm({ ...accountForm, baseUrl: event.target.value })}
                placeholder="https://ark.cn-beijing.volces.com"
              />
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              <div className="space-y-1.5">
                <Label>API Key</Label>
                <Input
                  type="password"
                  value={accountForm.apiKey}
                  onChange={(event) => setAccountForm({ ...accountForm, apiKey: event.target.value })}
                  placeholder={accountDialogMode === "edit" ? "留空则不修改" : "sk-..."}
                />
              </div>
              {accountDialogMode === "edit" && (
                <div className="space-y-1.5">
                  <Label>状态</Label>
                  <Select
                    value={accountForm.status}
                    onValueChange={(value) => setAccountForm({ ...accountForm, status: value })}
                    onOpenChange={trackPortaledControlOpen}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {statusOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>

            {accountDialogMode === "create" && setupFields.length > 0 && (
              <div className="grid gap-3 md:grid-cols-2">
                {setupFields.map((field) => (
                  <div key={field.key} className="space-y-1.5">
                    <Label>{field.label || field.key}</Label>
                    <Input
                      value={accountForm.setup[field.key] ?? ""}
                      onChange={(event) =>
                        setAccountForm({
                          ...accountForm,
                          setup: { ...accountForm.setup, [field.key]: event.target.value },
                        })
                      }
                      required={field.required}
                    />
                  </div>
                ))}
              </div>
            )}

            <div className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <Label>模型</Label>
                <Badge variant="secondary">{accountDialogModels.length}</Badge>
              </div>
              <div className="min-h-20 rounded-lg bg-muted/70 p-2">
                {accountDialogMode === "edit" && modelsLoading ? (
                  <Skeleton className="h-14" />
                ) : accountDialogModels.length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {accountDialogMode === "create"
                      ? selectedCreateModelDrafts.map((model) => (
                          <span
                            key={model.modelKey}
                            className="inline-flex h-7 max-w-full items-center gap-1 rounded-full border bg-background px-2 text-xs"
                          >
                            <span className="truncate">{model.displayName || model.modelKey}</span>
                            <span className="text-muted-foreground">{modalityLabel(model.modality)}</span>
                            <button
                              type="button"
                              className="rounded-full text-muted-foreground transition hover:text-destructive"
                              aria-label={`删除 ${model.displayName || model.modelKey}`}
                              onClick={() => handleRemoveCreateModel(model.modelKey)}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </span>
                        ))
                      : models.map((model) => (
                          <span
                            key={model.id}
                            className="inline-flex h-7 max-w-full items-center gap-1 rounded-full border bg-background px-2 text-xs"
                          >
                            <span className="truncate">{model.displayName || model.modelKey}</span>
                            <span className="text-muted-foreground">{modalityLabel(model.modality)}</span>
                            <button
                              type="button"
                              className="rounded-full text-muted-foreground transition hover:text-destructive"
                              aria-label={`删除 ${model.displayName || model.modelKey}`}
                              onClick={() => deleteModelMutation.mutate(model.id)}
                              disabled={deleteModelMutation.isPending}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </span>
                        ))}
                  </div>
                ) : (
                  <div className="flex min-h-14 items-center justify-center text-sm text-muted-foreground">暂无模型</div>
                )}
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  size="xs"
                  variant="secondary"
                  onClick={handleFillTemplateModels}
                  disabled={accountDialogModelActionPending || dialogModelTemplates.length === 0}
                >
                  <Plus className="h-3 w-3" />
                  填入相关模型
                </Button>
                <Button
                  size="xs"
                  variant="outline"
                  data-testid="provider-account-dialog-discover"
                  onClick={handleDiscoverModelsInAccountDialog}
                  disabled={accountDialogModelActionPending || accountDialogSaving}
                >
                  <RefreshCw className={cn("h-3 w-3", discoverModelsMutation.isPending && "animate-spin")} />
                  获取模型列表
                </Button>
                {accountDialogMode === "edit" && (
                  <Button size="xs" variant="outline" onClick={openDetailedModelsFromAccountDialog}>
                    <Layers3 className="h-3 w-3" />
                    更多
                  </Button>
                )}
              </div>

              <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_9rem_auto]">
                <Input
                  value={customModelName}
                  onChange={(event) => setCustomModelName(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      void handleAddCustomModel();
                    }
                  }}
                  placeholder="输入自定义模型名称"
                />
                <Select value={customModelModality} onValueChange={setCustomModelModality} onOpenChange={trackPortaledControlOpen}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {modalityOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button onClick={handleAddCustomModel} disabled={quickCreateModelMutation.isPending}>
                  填入
                </Button>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label>账号配置 JSON</Label>
              <Textarea
                className="min-h-44 font-mono text-xs"
                spellCheck={false}
                value={accountForm.configText}
                onChange={(event) => setAccountForm({ ...accountForm, configText: event.target.value })}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeAccountDialog}>取消</Button>
            <Button onClick={handleSaveAccount} disabled={accountDialogSaving}>
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={modelsDialogOpen} onOpenChange={handleModelsDialogOpenChange}>
        <DialogContent
          className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-4xl"
          onInteractOutside={preventDialogCloseFromPortaledControl}
          onPointerDownCapture={markDialogInnerPointerDown}
        >
          <DialogHeader>
            <DialogTitle>{selectedAccount?.name || "账号模型"}</DialogTitle>
            <DialogDescription>管理当前供应商账号下的可用模型</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap gap-2">
                {selectedAccountCatalog && <Badge variant="outline">{selectedAccountCatalog.displayName}</Badge>}
                {selectedAccount?.baseUrl && <Badge variant="secondary">{selectedAccount.baseUrl}</Badge>}
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  data-testid="provider-models-discover"
                  onClick={() => selectedAccountId && discoverModelsMutation.mutate(selectedAccountId)}
                  disabled={discoverModelsMutation.isPending || !selectedAccountId}
                >
                  <RefreshCw className={cn("h-3.5 w-3.5", discoverModelsMutation.isPending && "animate-spin")} />
                  发现模型
                </Button>
                <Button size="sm" onClick={() => openCreateModelDialog()}>
                  <Plus className="h-3.5 w-3.5" />
                  添加模型
                </Button>
              </div>
            </div>

            {modelTemplates.length > 0 && (
              <div className="rounded-lg border p-3">
                <div className="mb-2 text-xs font-medium text-muted-foreground">从预设添加</div>
                <div className="flex flex-wrap gap-2">
                  {modelTemplates.map((template) => (
                    <Button key={template.modelKey} size="xs" variant="outline" onClick={() => openCreateModelDialog(template)}>
                      {template.displayName}
                    </Button>
                  ))}
                </div>
              </div>
            )}

            {modelsLoading && <Skeleton className="h-48" />}
            {!modelsLoading && models.length === 0 && (
              <div className="rounded-lg border border-dashed p-10 text-center">
                <Sparkles className="mx-auto h-10 w-10 text-muted-foreground opacity-50" />
                <p className="mt-3 text-sm text-muted-foreground">暂无模型</p>
              </div>
            )}

            <div className="space-y-4">
              {groupedModels.map((group) => (
                <div key={group.modality} className="space-y-2">
                  <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                    <span>{modalityLabel(group.modality)}</span>
                    <div className="h-px flex-1 bg-border" />
                    <span>{group.models.length}</span>
                  </div>
                  {group.models.map((model) => (
                    <div key={model.id} className="rounded-lg border p-3" data-provider-model-id={model.id} data-testid="provider-model-card">
                      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-medium">{model.displayName}</span>
                            <Badge variant="outline">{modalityLabel(model.modality)}</Badge>
                            <Badge variant={model.status === "active" ? "default" : "secondary"}>{statusLabel(model.status)}</Badge>
                          </div>
                          <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{model.modelKey}</div>
                          <div className="mt-2 flex flex-wrap gap-1">
                            {modelTaskTypes(model).map((taskType) => (
                              <span key={taskType} className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                                {taskTypeLabel(taskType)}
                              </span>
                            ))}
                          </div>
                          {modelCapabilityLabels(model).length > 0 && (
                            <div className="mt-2 flex flex-wrap gap-1">
                              {modelCapabilityLabels(model).map((label) => (
                                <span key={label} className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] text-primary">
                                  {label}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                        <div className="flex flex-wrap gap-2">
                          {model.modality === "text" && (
                            <Button
                              size="sm"
                              variant="outline"
                              data-provider-model-id={model.id}
                              data-testid="provider-model-test"
                              onClick={() => testModelMutation.mutate(model.id)}
                              disabled={testModelMutation.isPending}
                            >
                              <Zap className="h-3.5 w-3.5" />
                              测试
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            data-provider-model-id={model.id}
                            data-testid="provider-model-edit"
                            onClick={() => openEditModelDialog(model)}
                          >
                            <Edit2 className="h-3.5 w-3.5" />
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant="destructive"
                            data-provider-model-id={model.id}
                            data-testid="provider-model-delete"
                            onClick={() => deleteModelMutation.mutate(model.id)}
                            disabled={deleteModelMutation.isPending}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            删除
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              ))}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={modelDialogOpen} onOpenChange={handleModelDialogOpenChange}>
        <DialogContent
          className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-2xl"
          onInteractOutside={preventDialogCloseFromPortaledControl}
          onPointerDownCapture={markDialogInnerPointerDown}
        >
          <DialogHeader>
            <DialogTitle>{modelDialogMode === "edit" ? "编辑模型" : "添加模型"}</DialogTitle>
            <DialogDescription>配置模型 ID、类型、任务能力和计费元数据</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid gap-3 md:grid-cols-2">
              <div className="space-y-1.5">
                <Label>模型 ID</Label>
                <Input value={modelForm.modelKey} onChange={(event) => setModelForm({ ...modelForm, modelKey: event.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>显示名称</Label>
                <Input value={modelForm.displayName} onChange={(event) => setModelForm({ ...modelForm, displayName: event.target.value })} />
              </div>
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <div className="space-y-1.5">
                <Label>模型类型</Label>
                <Select
                  value={modelForm.modality}
                  onOpenChange={trackPortaledControlOpen}
                  onValueChange={(value) => {
                    const taskTypes = defaultTaskTypesByModality[value] || [];
                    setModelForm({
                      ...modelForm,
                      modality: value,
                      supportsAsyncTask: defaultSupportsAsyncTask(value),
                      ...defaultCapabilityFormFields(value, taskTypes),
                      taskTypesText: taskTypes.join("\n") || modelForm.taskTypesText,
                    });
                  }}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {modalityOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>状态</Label>
                <Select
                  value={modelForm.status}
                  onValueChange={(value) => setModelForm({ ...modelForm, status: value })}
                  onOpenChange={trackPortaledControlOpen}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {statusOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label>任务类型</Label>
              <Textarea
                className="min-h-24 font-mono text-xs"
                spellCheck={false}
                value={modelForm.taskTypesText}
                onChange={(event) => setModelForm({ ...modelForm, taskTypesText: event.target.value })}
              />
            </div>
            <div className="flex items-center justify-between gap-3 rounded-md border p-3">
              <Label htmlFor="provider-model-supports-async-task">支持异步任务</Label>
              <Switch
                id="provider-model-supports-async-task"
                checked={modelForm.supportsAsyncTask}
                onCheckedChange={(checked) => setModelForm({ ...modelForm, supportsAsyncTask: checked })}
              />
            </div>
            <ModelCapabilityFields modelForm={modelForm} setModelForm={setModelForm} />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setModelDialogOpen(false)}>取消</Button>
            <Button onClick={handleSaveModel} disabled={saveModelMutation.isPending}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!accountToDelete} onOpenChange={(open) => !open && setAccountToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 className="h-5 w-5 text-destructive" />
            </AlertDialogMedia>
            <AlertDialogTitle>删除供应商</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该供应商账号会从列表移除，相关模型不再参与生产链路。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteAccountMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteAccountMutation.isPending}
              onClick={() => accountToDelete && deleteAccountMutation.mutate(accountToDelete.id)}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </AppShell>
  );
}

function ModelCapabilityFields({ modelForm, setModelForm }: { modelForm: ModelForm; setModelForm: (value: ModelForm) => void }) {
  const update = (patch: Partial<ModelForm>) => setModelForm({ ...modelForm, ...patch });
  const isText = modelForm.modality === "text" || modelForm.modality === "multimodal";
  const isImage = modelForm.modality === "image" || modelForm.modality === "multimodal";
  const isVideo = modelForm.modality === "video" || modelForm.modality === "multimodal";
  const isAudio = modelForm.modality === "audio" || modelForm.modality === "multimodal";
  return (
    <div className="space-y-4 rounded-lg border p-4">
      <div className="grid gap-3 md:grid-cols-2">
        <LabeledInput label="Prompt 最大长度" value={modelForm.promptMaxLength} onChange={(value) => update({ promptMaxLength: value })} />
        <div className="space-y-1.5">
          <Label>Prompt 长度单位</Label>
          <Select value={modelForm.promptLengthUnit} onValueChange={(value) => update({ promptLengthUnit: value })}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="characters">Unicode 字符</SelectItem>
              <SelectItem value="utf8_bytes">UTF-8 字节</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {isText && (
        <div className="space-y-3">
          <div className="grid gap-3 md:grid-cols-2">
            <LabeledInput label="最大输入 Token" value={modelForm.maxInputTokens} onChange={(value) => update({ maxInputTokens: value })} />
            <LabeledInput label="最大输出 Token" value={modelForm.maxOutputTokens} onChange={(value) => update({ maxOutputTokens: value })} />
            <LabeledListInput label="支持输入类型" value={modelForm.supportedInputTypesText} onChange={(value) => update({ supportedInputTypesText: value })} />
            <LabeledListInput label="支持输出类型" value={modelForm.supportedOutputTypesText} onChange={(value) => update({ supportedOutputTypesText: value })} />
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <SwitchField label="支持流式输出" checked={modelForm.supportsStreaming} onChange={(checked) => update({ supportsStreaming: checked })} />
            {modelForm.supportsStreaming && (
              <div className="space-y-1.5">
                <Label>流式完成判定</Label>
                <Select
                  value={modelForm.streamTerminalMode}
                  onValueChange={(value) => update({ streamTerminalMode: value as ModelForm["streamTerminalMode"] })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="done_or_finish_reason">完成标记或结束原因</SelectItem>
                    <SelectItem value="done_marker">完成标记</SelectItem>
                    <SelectItem value="finish_reason">结束原因</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}
            <SwitchField
              label="支持思考"
              checked={modelForm.supportsReasoning}
              onChange={(checked) => update({
                supportsReasoning: checked,
                supportsReasoningLevels: checked ? modelForm.supportsReasoningLevels : false,
              })}
            />
            <SwitchField
              label="支持思考等级"
              checked={modelForm.supportsReasoningLevels}
              onChange={(checked) => update({
                supportsReasoning: checked || modelForm.supportsReasoning,
                supportsReasoningLevels: checked,
                reasoningLevelsText: checked && !modelForm.reasoningLevelsText.trim()
                  ? listText(["low", "medium", "high"])
                  : modelForm.reasoningLevelsText,
              })}
            />
            {modelForm.supportsReasoningLevels && (
              <LabeledListInput
                label="可用思考等级"
                value={modelForm.reasoningLevelsText}
                onChange={(value) => update({ reasoningLevelsText: value })}
              />
            )}
            <SwitchField label="支持多模态输入" checked={modelForm.supportsMultimodalInput} onChange={(checked) => update({ supportsMultimodalInput: checked })} />
          </div>
        </div>
      )}

      {isImage && (
        <div className="space-y-3">
          <div className="grid gap-3 md:grid-cols-2">
            <LabeledInput label="参考图数量上限" value={modelForm.maxReferenceImages} onChange={(value) => update({ maxReferenceImages: value })} />
            <LabeledListInput label="图片请求方式" value={modelForm.imageRequestModesText} onChange={(value) => update({ imageRequestModesText: value })} />
            <LabeledListInput label="图片比例" value={modelForm.imageAspectRatiosText} onChange={(value) => update({ imageAspectRatiosText: value })} />
            <LabeledListInput label="图片清晰度" value={modelForm.imageResolutionsText} onChange={(value) => update({ imageResolutionsText: value })} />
            <LabeledListInput
              label="图片质量档位"
              value={modelForm.imageQualityTiersText}
              onChange={(value) => update({ imageQualityTiersText: value })}
            />
            <LabeledListInput label="图片输出格式" value={modelForm.imageResponseFormatsText} onChange={(value) => update({ imageResponseFormatsText: value })} />
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <SwitchField label="支持参考图" checked={modelForm.supportsReferenceImages} onChange={(checked) => update({ supportsReferenceImages: checked })} />
            <SwitchField label="支持图片编辑" checked={modelForm.supportsImageEdit} onChange={(checked) => update({ supportsImageEdit: checked })} />
          </div>
        </div>
      )}

      {isVideo && (
        <div className="space-y-3">
          <div className="grid gap-3 md:grid-cols-2">
            <LabeledInput label="参考图数量上限" value={modelForm.maxReferenceImages} onChange={(value) => update({ maxReferenceImages: value })} />
            <LabeledInput label="参考视频数量上限" value={modelForm.maxReferenceVideos} onChange={(value) => update({ maxReferenceVideos: value })} />
            <LabeledListInput label="视频输出格式" value={modelForm.videoOutputFormatsText} onChange={(value) => update({ videoOutputFormatsText: value })} />
          </div>
          <VideoVariantEditor
            variants={modelForm.videoVariants}
            taskTypes={taskTypesFromText(modelForm.taskTypesText)}
            onChange={(videoVariants) => update({ videoVariants })}
          />
        </div>
      )}

      {isAudio && (
        <div className="space-y-3">
          <div className="grid gap-3 md:grid-cols-2">
            <LabeledListInput label="可用声音" value={modelForm.audioVoicesText} onChange={(value) => update({ audioVoicesText: value })} />
            <LabeledListInput label="支持语言" value={modelForm.audioLanguagesText} onChange={(value) => update({ audioLanguagesText: value })} />
            <LabeledListInput label="音频输入格式" value={modelForm.audioInputFormatsText} onChange={(value) => update({ audioInputFormatsText: value })} />
            <LabeledListInput label="音频输出格式" value={modelForm.audioOutputFormatsText} onChange={(value) => update({ audioOutputFormatsText: value })} />
            <LabeledListInput label="请求方式" value={modelForm.audioRequestModesText} onChange={(value) => update({ audioRequestModesText: value })} />
            <LabeledInput label="单次合成文本上限" value={modelForm.maxTTSCharacters} onChange={(value) => update({ maxTTSCharacters: value })} />
            <LabeledInput label="单次识别音频上限（秒）" value={modelForm.maxAudioDurationSeconds} onChange={(value) => update({ maxAudioDurationSeconds: value })} />
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <SwitchField label="支持语音合成" checked={modelForm.supportsTTS} onChange={(checked) => update({ supportsTTS: checked })} />
            <SwitchField label="支持语音识别" checked={modelForm.supportsTranscription} onChange={(checked) => update({ supportsTranscription: checked })} />
          </div>
        </div>
      )}
    </div>
  );
}

function VideoVariantEditor({
  variants,
  taskTypes,
  onChange,
}: {
  variants: VideoVariantForm[];
  taskTypes: string[];
  onChange: (variants: VideoVariantForm[]) => void;
}) {
  const updateVariant = (index: number, patch: Partial<VideoVariantForm>) => {
    onChange(variants.map((variant, variantIndex) => (variantIndex === index ? { ...variant, ...patch } : variant)));
  };
  return (
    <div className="space-y-3 border-t pt-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="font-medium">视频能力矩阵</div>
          <div className="text-xs text-muted-foreground">每个能力组合独立匹配，运行时不会跨组合拼接。</div>
        </div>
        <Button type="button" size="sm" variant="outline" onClick={() => onChange([...variants, emptyVideoVariant(variants.length + 1, taskTypes)])}>
          <Plus className="h-3.5 w-3.5" />
          添加能力
        </Button>
      </div>
      {variants.map((variant, index) => (
        <div key={`${variant.variantKey}-${index}`} className="space-y-4 rounded-md border bg-muted/20 p-3">
          <div className="flex items-end gap-3">
            <div className="grid min-w-0 flex-1 gap-3 md:grid-cols-2">
              <LabeledInput label="能力标识" value={variant.variantKey} onChange={(value) => updateVariant(index, { variantKey: value })} />
              <LabeledInput label="模型家族" value={variant.modelFamily} onChange={(value) => updateVariant(index, { modelFamily: value })} />
            </div>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label="删除能力"
              title="删除能力"
              disabled={variants.length === 1}
              onClick={() => onChange(variants.filter((_, variantIndex) => variantIndex !== index))}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <LabeledListInput label="任务类型" value={variant.taskTypesText} onChange={(value) => updateVariant(index, { taskTypesText: value })} />
            <LabeledListInput label="参考模式" value={variant.referenceModesText} onChange={(value) => updateVariant(index, { referenceModesText: value })} />
            <LabeledListInput label="画面比例" value={variant.aspectRatiosText} onChange={(value) => updateVariant(index, { aspectRatiosText: value })} />
            <LabeledListInput label="清晰度" value={variant.resolutionsText} onChange={(value) => updateVariant(index, { resolutionsText: value })} />
            <LabeledListInput label="请求方式" value={variant.requestModesText} onChange={(value) => updateVariant(index, { requestModesText: value })} />
            <LabeledListInput label="Prompt 语言" value={variant.promptLanguagesText} onChange={(value) => updateVariant(index, { promptLanguagesText: value })} />
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <LabeledSelect
              label="时长规则"
              value={variant.durationMode}
              options={[
                { value: "continuous_range", label: "连续范围" },
                { value: "discrete", label: "离散秒数" },
                { value: "fixed", label: "固定秒数" },
                { value: "source_duration", label: "跟随源视频" },
              ]}
              onChange={(value) => updateVariant(index, { durationMode: value as VideoVariantForm["durationMode"] })}
            />
            {variant.durationMode === "continuous_range" ? (
              <>
                <LabeledInput label="最短秒数" value={variant.minDurationSeconds} onChange={(value) => updateVariant(index, { minDurationSeconds: value })} />
                <LabeledInput label="最长秒数" value={variant.maxDurationSeconds} onChange={(value) => updateVariant(index, { maxDurationSeconds: value })} />
                <LabeledInput label="步进秒数" value={variant.durationStepSeconds} onChange={(value) => updateVariant(index, { durationStepSeconds: value })} />
              </>
            ) : variant.durationMode !== "source_duration" ? (
              <LabeledListInput label="可用秒数" value={variant.durationValuesText} onChange={(value) => updateVariant(index, { durationValuesText: value })} />
            ) : null}
            <LabeledSelect
              label="帧率规则"
              value={variant.frameRateMode}
              options={[
                { value: "unknown", label: "未知，生成后探测" },
                { value: "fixed", label: "固定帧率" },
                { value: "selectable", label: "可选帧率" },
              ]}
              onChange={(value) => updateVariant(index, { frameRateMode: value as VideoVariantForm["frameRateMode"] })}
            />
            {variant.frameRateMode !== "unknown" ? (
              <LabeledListInput label="帧率" value={variant.frameRatesText} onChange={(value) => updateVariant(index, { frameRatesText: value })} />
            ) : null}
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <LabeledSelect
              label="请求原生音频时"
              value={variant.nativeAudioRequested}
              options={[
                { value: "any", label: "均可匹配" },
                { value: "true", label: "仅请求音频" },
                { value: "false", label: "仅不请求音频" },
              ]}
              onChange={(value) => updateVariant(index, { nativeAudioRequested: value as VideoVariantForm["nativeAudioRequested"] })}
            />
            <CapabilityTruthSelect label="原生音频" value={variant.nativeAudioSupport} onChange={(value) => updateVariant(index, { nativeAudioSupport: value })} />
            <CapabilityTruthSelect label="可关闭原生音频" value={variant.nativeAudioCanDisable} onChange={(value) => updateVariant(index, { nativeAudioCanDisable: value })} />
            <CapabilityTruthSelect label="生成对白" value={variant.supportsDialogue} onChange={(value) => updateVariant(index, { supportsDialogue: value })} />
            <CapabilityTruthSelect label="生成旁白" value={variant.supportsVoiceover} onChange={(value) => updateVariant(index, { supportsVoiceover: value })} />
            <CapabilityTruthSelect label="生成环境声" value={variant.supportsAmbientSound} onChange={(value) => updateVariant(index, { supportsAmbientSound: value })} />
            <CapabilityTruthSelect label="生成音乐" value={variant.supportsMusic} onChange={(value) => updateVariant(index, { supportsMusic: value })} />
            <CapabilityTruthSelect label="口型同步" value={variant.supportsLipSync} onChange={(value) => updateVariant(index, { supportsLipSync: value })} />
            <LabeledListInput label="对白语言" value={variant.dialogueLanguagesText} onChange={(value) => updateVariant(index, { dialogueLanguagesText: value })} />
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <SwitchField label="支持延长任务" checked={variant.supportsExtension} onChange={(checked) => updateVariant(index, { supportsExtension: checked })} />
            <SwitchField label="支持首帧续接" checked={variant.supportsFirstFrame} onChange={(checked) => updateVariant(index, { supportsFirstFrame: checked })} />
            <SwitchField label="支持尾帧续接" checked={variant.supportsLastFrame} onChange={(checked) => updateVariant(index, { supportsLastFrame: checked })} />
            <SwitchField label="支持视频参考" checked={variant.supportsVideoReference} onChange={(checked) => updateVariant(index, { supportsVideoReference: checked })} />
            <SwitchField label="音轨可独立分离" checked={variant.audioTrackSeparable} onChange={(checked) => updateVariant(index, { audioTrackSeparable: checked })} />
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <LabeledInput label="能力来源" value={variant.source} onChange={(value) => updateVariant(index, { source: value })} />
            <LabeledInput label="来源地址" value={variant.sourceUrl} onChange={(value) => updateVariant(index, { sourceUrl: value })} />
            <LabeledInput label="能力版本" value={variant.capabilityVersion} onChange={(value) => updateVariant(index, { capabilityVersion: value })} />
          </div>
        </div>
      ))}
    </div>
  );
}

function CapabilityTruthSelect({ label, value, onChange }: { label: string; value: CapabilityTruth; onChange: (value: CapabilityTruth) => void }) {
  return (
    <LabeledSelect
      label={label}
      value={value}
      options={[
        { value: "unknown", label: "未知" },
        { value: "true", label: "支持" },
        { value: "false", label: "不支持" },
      ]}
      onChange={(next) => onChange(next as CapabilityTruth)}
    />
  );
}

function LabeledSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger><SelectValue /></SelectTrigger>
        <SelectContent>
          {options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>
  );
}

function LabeledInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Input value={value} onChange={(event) => onChange(event.target.value)} />
    </div>
  );
}

function LabeledListInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Textarea className="min-h-16 text-sm" value={value} onChange={(event) => onChange(event.target.value)} />
    </div>
  );
}

function SwitchField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border p-3">
      <Label>{label}</Label>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

function BusinessBindingRow({
  binding,
  model,
  onDelete,
  onUpdate,
  deleting,
}: {
  binding: ModelProfileBinding;
  model?: ProviderModelWithAccount;
  onDelete: () => void;
  onUpdate: (body: UpdateModelProfileBindingRequest) => Promise<void>;
  deleting: boolean;
}) {
  const [priority, setPriority] = useState(String(binding.priority));
  const [weight, setWeight] = useState(String(binding.weight));
  const [enabled, setEnabled] = useState(binding.enabled);
  const [reasoningLevel, setReasoningLevel] = useState(binding.runtimeOptions?.reasoningLevel || "");
  const [saving, setSaving] = useState(false);
  const reasoningLevels = model ? providerModelReasoningLevels(model) : [];

  const priorityValue = parseNonNegativeBindingInteger(priority);
  const weightValue = parseNonNegativeBindingInteger(weight);
  const valuesDirty = priorityValue !== null && weightValue !== null && (
    priorityValue !== binding.priority ||
    weightValue !== binding.weight ||
    reasoningLevel !== (binding.runtimeOptions?.reasoningLevel || "")
  );
  const busy = saving || deleting;

  async function saveRoutingValues() {
    if (priorityValue === null || weightValue === null) {
      toast.error("优先顺序和权重必须是大于或等于 0 的整数");
      return;
    }
    setSaving(true);
    try {
      await onUpdate({
        priority: priorityValue,
        weight: weightValue,
        runtimeOptions: reasoningLevel ? { reasoningLevel } : {},
      });
    } catch {
      // The mutation reports the normalized API error.
    } finally {
      setSaving(false);
    }
  }

  async function changeEnabled(next: boolean) {
    const previous = enabled;
    setEnabled(next);
    setSaving(true);
    try {
      await onUpdate({ enabled: next });
    } catch {
      setEnabled(previous);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-md border p-3">
      <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{model?.displayName || model?.modelKey || "模型不可用"}</span>
            {model ? <Badge variant="outline">{modalityLabel(model.modality)}</Badge> : <Badge variant="secondary">未找到</Badge>}
            <Badge variant={enabled ? "default" : "secondary"}>{enabled ? "启用" : "停用"}</Badge>
          </div>
          <div className="truncate font-mono text-xs text-muted-foreground">{model?.modelKey || binding.providerModelId}</div>
          {model ? (
            <div className="flex flex-wrap gap-1 text-xs text-muted-foreground">
              <span>{model.providerLabel}</span>
              <span>·</span>
              <span>{model.accountName}</span>
            </div>
          ) : null}
        </div>
        <Button size="sm" variant="destructive" onClick={onDelete} disabled={busy}>
          <Trash2 className="h-3.5 w-3.5" />
          删除
        </Button>
      </div>

      <div className={cn(
        "mt-3 grid gap-3 border-t pt-3 sm:grid-cols-2 xl:items-end",
        reasoningLevels.length > 0
          ? "xl:grid-cols-[minmax(110px,0.8fr)_minmax(110px,0.8fr)_minmax(150px,1fr)_minmax(180px,1.2fr)_auto]"
          : "xl:grid-cols-[minmax(120px,1fr)_minmax(120px,1fr)_minmax(180px,1.2fr)_auto]",
      )}>
        <div className="space-y-1.5">
          <Label htmlFor={`binding-priority-${binding.id}`}>优先顺序</Label>
          <Input
            id={`binding-priority-${binding.id}`}
            type="number"
            inputMode="numeric"
            min={0}
            step={1}
            value={priority}
            onChange={(event) => setPriority(event.target.value)}
            disabled={busy}
          />
          <div className="text-xs text-muted-foreground">数字越小越优先</div>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`binding-weight-${binding.id}`}>权重</Label>
          <Input
            id={`binding-weight-${binding.id}`}
            type="number"
            inputMode="numeric"
            min={0}
            step={1}
            value={weight}
            onChange={(event) => setWeight(event.target.value)}
            disabled={busy}
          />
          <div className="text-xs text-muted-foreground">同级时数字越大越优先</div>
        </div>
        {reasoningLevels.length > 0 && (
          <div className="space-y-1.5">
            <Label htmlFor={`binding-reasoning-${binding.id}`}>默认思考等级</Label>
            <Select
              value={reasoningLevel || "__provider_default__"}
              onValueChange={(value) => setReasoningLevel(value === "__provider_default__" ? "" : value)}
              disabled={busy}
            >
              <SelectTrigger id={`binding-reasoning-${binding.id}`}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__provider_default__">供应商默认</SelectItem>
                {reasoningLevels.map((level) => (
                  <SelectItem key={level} value={level}>{reasoningLevelLabel(level)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="text-xs text-muted-foreground">用于该业务模型的所有文本请求</div>
          </div>
        )}
        <div className="flex h-16 items-center justify-between gap-3 rounded-md border px-3">
          <div className="min-w-0">
            <Label htmlFor={`binding-enabled-${binding.id}`}>启用路由</Label>
            <div className="mt-1 text-xs text-muted-foreground">{enabled ? "已开启" : "已关闭"}</div>
          </div>
          <Switch
            id={`binding-enabled-${binding.id}`}
            checked={enabled}
            onCheckedChange={(checked) => void changeEnabled(checked)}
            disabled={busy}
          />
        </div>
        <Button size="sm" onClick={() => void saveRoutingValues()} disabled={busy || !valuesDirty || priorityValue === null || weightValue === null}>
          {saving ? <Loader2 className="animate-spin" /> : <Save />}
          保存
        </Button>
      </div>
    </div>
  );
}

function parseNonNegativeBindingInteger(value: string) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) {
    return null;
  }
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function accountFormFromCatalog(entry: ProviderCatalogEntry | null): AccountForm {
  return {
    ...emptyAccountForm,
    name: entry?.displayName || "",
    baseUrl: entry?.defaultBaseUrl || "",
    authType: entry?.defaultAuthType || "bearer",
    setup: defaultSetupValues(entry),
    configText: jsonText(entry?.setupSchema?.defaultConfig || {}),
  };
}

function emptyModelForm(modality: string): ModelForm {
  const defaults = defaultCapabilityFormFields(modality, defaultTaskTypesByModality[modality] || []);
  return {
    modelKey: "",
    displayName: "",
    modality,
    status: "active",
    supportsAsyncTask: defaultSupportsAsyncTask(modality),
    ...defaults,
    taskTypesText: (defaultTaskTypesByModality[modality] || []).join("\n"),
    inputLimitsText: "{}",
    outputLimitsText: "{}",
    qualityTiersText: "[]",
    providerOptionsSchemaText: "{}",
    pricingPolicyText: "{}",
  };
}

function customModelDraft(modelKey: string, modality: string): ModelDraft {
  const normalizedModality = modality || inferModelModality(modelKey);
  return {
    source: "custom",
    modelKey,
    displayName: modelKey,
    modality: normalizedModality,
    taskTypes: defaultTaskTypesByModality[normalizedModality] || [],
    inputLimits: {},
    outputLimits: {},
    qualityTiers: [],
    providerOptionsSchema: {},
    pricingPolicy: {},
  };
}

function defaultCapabilityFormFields(modality: string, taskTypes: string[]) {
  const isText = modality === "text" || modality === "multimodal";
  const isImage = modality === "image" || modality === "multimodal";
  const isVideo = modality === "video" || modality === "multimodal";
  const isAudio = modality === "audio" || modality === "multimodal";
  return {
    supportsStreaming: taskTypes.includes("text.stream"),
    streamTerminalMode: "done_or_finish_reason" as const,
    supportsReasoning: false,
    supportsReasoningLevels: false,
    reasoningLevelsText: "",
    supportsMultimodalInput: modality === "multimodal",
    maxInputTokens: "",
    maxOutputTokens: "",
    supportedInputTypesText: listText(isText && modality === "multimodal" ? ["text", "image"] : ["text"]),
    supportedOutputTypesText: listText(isVideo ? ["video"] : isImage ? ["image"] : ["text"]),
    promptMaxLength: "",
    promptLengthUnit: "characters",
    supportsReferenceImages: false,
    supportsImageEdit: false,
    maxReferenceImages: "",
    imageRequestModesText: listText(["images.generate"]),
    imageAspectRatiosText: listText(["1:1", "16:9", "9:16"]),
    imageResolutionsText: listText(["1024x1024"]),
    imageQualityTiersText: listText(["standard", "hd"]),
    imageResponseFormatsText: listText(["url", "b64_json"]),
    minDurationSeconds: "",
    maxDurationSeconds: "",
    durationsText: listText(["5", "10"]),
    supportsFirstFrame: false,
    supportsLastFrame: false,
    supportsVideoReference: false,
    maxReferenceVideos: "",
    videoRequestModesText: listText(["async_create", "poll", "cancel"]),
    videoAspectRatiosText: listText(["16:9", "9:16", "1:1"]),
    videoResolutionsText: listText(["720p", "1080p"]),
    videoOutputFormatsText: listText(["video"]),
    videoVariants: isVideo ? [emptyVideoVariant(1, taskTypes)] : [],
    supportsTTS: taskTypes.includes("audio.tts"),
    supportsTranscription: taskTypes.includes("audio.transcribe"),
    audioVoicesText: "",
    audioLanguagesText: listText(["zh-CN", "en"]),
    audioInputFormatsText: listText(["mp3", "wav", "m4a", "webm"]),
    audioOutputFormatsText: listText(["mp3", "wav", "aac", "flac", "opus"]),
    audioRequestModesText: listText(isAudio ? ["audio.speech", "audio.transcriptions"] : []),
    maxTTSCharacters: "",
    maxAudioDurationSeconds: "",
  };
}

function emptyVideoVariant(index: number, taskTypes: string[]): VideoVariantForm {
  return {
    variantKey: `variant_${index}`,
    modelFamily: "",
    taskTypesText: listText(taskTypes.filter((taskType) => taskType.startsWith("video."))),
    referenceModesText: listText(["none", "first_frame"]),
    nativeAudioRequested: "any",
    durationMode: "discrete",
    minDurationSeconds: "",
    maxDurationSeconds: "",
    durationStepSeconds: "",
    durationValuesText: listText(["5", "10"]),
    resolutionsText: listText(["720p", "1080p"]),
    aspectRatiosText: listText(["16:9", "9:16", "1:1"]),
    frameRateMode: "unknown",
    frameRatesText: "",
    promptLanguagesText: listText(["zh-CN", "en"]),
    nativeAudioSupport: "unknown",
    nativeAudioCanDisable: "unknown",
    supportsDialogue: "unknown",
    supportsVoiceover: "unknown",
    supportsAmbientSound: "unknown",
    supportsMusic: "unknown",
    supportsLipSync: "unknown",
    dialogueLanguagesText: listText(["zh-CN"]),
    audioTrackSeparable: false,
    supportsExtension: false,
    supportsFirstFrame: true,
    supportsLastFrame: false,
    supportsVideoReference: false,
    requestModesText: listText(["async_create", "poll", "cancel"]),
    source: "user",
    sourceUrl: "",
    capabilityVersion: "1",
  };
}

function capabilityFormFieldsFromValues(
  modality: string,
  taskTypes: string[],
  inputLimits: JsonRecord,
  outputLimits: JsonRecord,
  qualityTiers: JsonValue,
  providerOptionsSchema: JsonRecord,
) {
  const defaults = defaultCapabilityFormFields(modality, taskTypes);
  const xCapabilities = isPlainRecord(providerOptionsSchema.xCapabilities) ? providerOptionsSchema.xCapabilities : {};
  const supportedInputTypes = arrayFromValue(xCapabilities.supportedInputTypes).length
    ? arrayFromValue(xCapabilities.supportedInputTypes)
    : arrayFromValue(inputLimits.inputTypes);
  const supportedOutputTypes = arrayFromValue(xCapabilities.supportedOutputTypes).length
    ? arrayFromValue(xCapabilities.supportedOutputTypes)
    : arrayFromValue(outputLimits.outputTypes);
  const responseFormats = arrayFromValue(xCapabilities.responseFormats).length
    ? arrayFromValue(xCapabilities.responseFormats)
    : arrayFromValue(outputLimits.responseFormats);
  const supportedResolutions = arrayFromValue(xCapabilities.supportedResolutions);
  const imageQualityTiers = arrayFromValue(xCapabilities.quality).filter((value) => value !== "auto").length
    ? arrayFromValue(xCapabilities.quality).filter((value) => value !== "auto")
    : arrayFromValue(qualityTiers);
  const videoVariants = videoVariantFormsFromCapabilities(xCapabilities, taskTypes);
  return {
    ...defaults,
    supportsStreaming: booleanFromValue(xCapabilities.supportsStreaming, defaults.supportsStreaming),
    streamTerminalMode: streamTerminalModeFromValue(xCapabilities.streamTerminalMode),
    supportsReasoning: booleanFromValue(xCapabilities.supportsReasoning, defaults.supportsReasoning),
    supportsReasoningLevels: booleanFromValue(xCapabilities.supportsReasoningLevels, defaults.supportsReasoningLevels),
    reasoningLevelsText: listText(arrayFromValue(xCapabilities.reasoningLevels)),
    supportsMultimodalInput: booleanFromValue(xCapabilities.supportsMultimodalInput, defaults.supportsMultimodalInput),
    maxInputTokens: textFromValue(inputLimits.maxTokens),
    maxOutputTokens: textFromValue(outputLimits.maxTokens),
    supportedInputTypesText: listText(supportedInputTypes.length ? supportedInputTypes : splitList(defaults.supportedInputTypesText)),
    supportedOutputTypesText: listText(supportedOutputTypes.length ? supportedOutputTypes : splitList(defaults.supportedOutputTypesText)),
    promptMaxLength: textFromValue(inputLimits.promptMaxLength),
    promptLengthUnit: textFromValue(inputLimits.promptLengthUnit ?? xCapabilities.promptLengthUnit) || defaults.promptLengthUnit,
    supportsReferenceImages: booleanFromValue(
      xCapabilities.supportsReferenceImages ?? xCapabilities.supportsReferences,
      defaults.supportsReferenceImages,
    ),
    supportsImageEdit: splitList(listText(arrayFromValue(xCapabilities.requestModes))).includes("images.edit"),
    maxReferenceImages: textFromValue(xCapabilities.maxReferenceImages ?? inputLimits.maxReferenceImages),
    imageRequestModesText: listText(arrayFromValue(xCapabilities.requestModes).length ? arrayFromValue(xCapabilities.requestModes) : splitList(defaults.imageRequestModesText)),
    imageAspectRatiosText: listText(arrayFromValue(xCapabilities.supportedAspectRatios).length ? arrayFromValue(xCapabilities.supportedAspectRatios) : splitList(defaults.imageAspectRatiosText)),
    imageResolutionsText: listText(supportedResolutions.length ? supportedResolutions : splitList(defaults.imageResolutionsText)),
    imageQualityTiersText: listText(
      imageQualityTiers.length ? imageQualityTiers : splitList(defaults.imageQualityTiersText),
    ),
    imageResponseFormatsText: listText(responseFormats.length ? responseFormats : splitList(defaults.imageResponseFormatsText)),
    minDurationSeconds: textFromValue(xCapabilities.minDurationSeconds),
    maxDurationSeconds: textFromValue(xCapabilities.maxDurationSeconds),
    durationsText: listText(arrayFromValue(xCapabilities.durations).length ? arrayFromValue(xCapabilities.durations) : splitList(defaults.durationsText)),
    supportsFirstFrame: booleanFromValue(xCapabilities.supportsFirstFrame, defaults.supportsFirstFrame),
    supportsLastFrame: booleanFromValue(xCapabilities.supportsLastFrame, defaults.supportsLastFrame),
    supportsVideoReference: booleanFromValue(xCapabilities.supportsVideoReference, defaults.supportsVideoReference),
    maxReferenceVideos: textFromValue(xCapabilities.maxReferenceVideos),
    videoRequestModesText: listText(arrayFromValue(xCapabilities.requestModes).length ? arrayFromValue(xCapabilities.requestModes) : splitList(defaults.videoRequestModesText)),
    videoAspectRatiosText: listText(arrayFromValue(xCapabilities.supportedAspectRatios).length ? arrayFromValue(xCapabilities.supportedAspectRatios) : splitList(defaults.videoAspectRatiosText)),
    videoResolutionsText: listText(supportedResolutions.length ? supportedResolutions : splitList(defaults.videoResolutionsText)),
    videoOutputFormatsText: listText(supportedOutputTypes.length ? supportedOutputTypes : splitList(defaults.videoOutputFormatsText)),
    videoVariants: videoVariants.length > 0 ? videoVariants : defaults.videoVariants,
    supportsTTS: booleanFromValue(xCapabilities.supportsTTS, defaults.supportsTTS),
    supportsTranscription: booleanFromValue(xCapabilities.supportsTranscription, defaults.supportsTranscription),
    audioVoicesText: listText(arrayFromValue(xCapabilities.audioVoices).length ? arrayFromValue(xCapabilities.audioVoices) : splitList(defaults.audioVoicesText)),
    audioLanguagesText: listText(arrayFromValue(xCapabilities.audioLanguages).length ? arrayFromValue(xCapabilities.audioLanguages) : splitList(defaults.audioLanguagesText)),
    audioInputFormatsText: listText(arrayFromValue(xCapabilities.audioInputFormats).length ? arrayFromValue(xCapabilities.audioInputFormats) : splitList(defaults.audioInputFormatsText)),
    audioOutputFormatsText: listText(arrayFromValue(xCapabilities.audioResponseFormats).length ? arrayFromValue(xCapabilities.audioResponseFormats) : splitList(defaults.audioOutputFormatsText)),
    audioRequestModesText: listText(arrayFromValue(xCapabilities.audioRequestModes).length ? arrayFromValue(xCapabilities.audioRequestModes) : splitList(defaults.audioRequestModesText)),
    maxTTSCharacters: textFromValue(xCapabilities.maxTTSCharacters ?? inputLimits.maxTTSCharacters),
    maxAudioDurationSeconds: textFromValue(xCapabilities.maxAudioDurationSeconds ?? inputLimits.maxAudioDurationSeconds),
  };
}

function videoVariantFormsFromCapabilities(xCapabilities: JsonRecord, taskTypes: string[]): VideoVariantForm[] {
  const rawVariants = Array.isArray(xCapabilities.videoGenerationVariants)
    ? xCapabilities.videoGenerationVariants.filter(isRecord)
    : [];
  if (rawVariants.length === 0) {
    const fallback = emptyVideoVariant(1, taskTypes);
    const durations = arrayFromValue(xCapabilities.durations);
    const minDuration = textFromValue(xCapabilities.minDurationSeconds);
    const maxDuration = textFromValue(xCapabilities.maxDurationSeconds);
    fallback.durationMode = durations.length === 1 ? "fixed" : durations.length > 1 ? "discrete" : minDuration && maxDuration ? "continuous_range" : "discrete";
    fallback.durationValuesText = durations.length > 0 ? listText(durations) : fallback.durationValuesText;
    fallback.minDurationSeconds = minDuration;
    fallback.maxDurationSeconds = maxDuration;
    fallback.resolutionsText = listText(arrayFromValue(xCapabilities.supportedResolutions));
    fallback.aspectRatiosText = listText(arrayFromValue(xCapabilities.supportedAspectRatios));
    fallback.requestModesText = listText(arrayFromValue(xCapabilities.requestModes));
    fallback.supportsFirstFrame = booleanFromValue(xCapabilities.supportsFirstFrame, false);
    fallback.supportsLastFrame = booleanFromValue(xCapabilities.supportsLastFrame, false);
    fallback.supportsVideoReference = booleanFromValue(xCapabilities.supportsVideoReference, false);
    fallback.referenceModesText = listText([
      "none",
      ...(booleanFromValue(xCapabilities.supportsReferenceImages, false) || fallback.supportsFirstFrame ? ["first_frame"] : []),
      ...(fallback.supportsLastFrame ? ["last_frame"] : []),
      ...(fallback.supportsVideoReference ? ["video_reference"] : []),
    ]);
    fallback.source = "derived";
    return [fallback];
  }
  return rawVariants.map((raw, index) => {
    const form = emptyVideoVariant(index + 1, taskTypes);
    const when = isPlainRecord(raw.when) ? raw.when : {};
    const duration = isPlainRecord(raw.duration) ? raw.duration : {};
    const frameRate = isPlainRecord(raw.frameRate) ? raw.frameRate : {};
    const nativeAudio = isPlainRecord(raw.nativeAudio) ? raw.nativeAudio : {};
    const continuation = isPlainRecord(raw.continuation) ? raw.continuation : {};
    return {
      ...form,
      variantKey: textFromValue(raw.variantKey) || form.variantKey,
      modelFamily: textFromValue(raw.modelFamily),
      taskTypesText: listText(arrayFromValue(when.taskTypes)),
      referenceModesText: listText(arrayFromValue(when.referenceModes)),
      nativeAudioRequested: capabilityTruthFromValue(when.nativeAudioRequested, "any"),
      durationMode: videoDurationModeFromValue(duration.mode),
      minDurationSeconds: textFromValue(duration.minSeconds),
      maxDurationSeconds: textFromValue(duration.maxSeconds),
      durationStepSeconds: textFromValue(duration.stepSeconds),
      durationValuesText: listText(arrayFromValue(duration.values)),
      resolutionsText: listText(arrayFromValue(raw.resolutions)),
      aspectRatiosText: listText(arrayFromValue(raw.aspectRatios)),
      frameRateMode: videoFrameRateModeFromValue(frameRate.mode),
      frameRatesText: listText(arrayFromValue(frameRate.values)),
      promptLanguagesText: listText(arrayFromValue(raw.supportedPromptLanguages)),
      nativeAudioSupport: capabilityTruthFromValue(nativeAudio.support),
      nativeAudioCanDisable: capabilityTruthFromValue(nativeAudio.canDisable),
      supportsDialogue: capabilityTruthFromValue(nativeAudio.supportsDialogue),
      supportsVoiceover: capabilityTruthFromValue(nativeAudio.supportsVoiceover),
      supportsAmbientSound: capabilityTruthFromValue(nativeAudio.supportsAmbientSound),
      supportsMusic: capabilityTruthFromValue(nativeAudio.supportsMusic),
      supportsLipSync: capabilityTruthFromValue(nativeAudio.supportsLipSync),
      dialogueLanguagesText: listText(arrayFromValue(nativeAudio.supportedDialogueLanguages)),
      audioTrackSeparable: booleanFromValue(nativeAudio.audioTrackSeparable, false),
      supportsExtension: booleanFromValue(continuation.supportsExtension, false),
      supportsFirstFrame: booleanFromValue(continuation.supportsFirstFrame, false),
      supportsLastFrame: booleanFromValue(continuation.supportsLastFrame, false),
      supportsVideoReference: booleanFromValue(continuation.supportsVideoReference, false),
      requestModesText: listText(arrayFromValue(raw.requestModes)),
      source: textFromValue(raw.source) || "user",
      sourceUrl: textFromValue(raw.sourceUrl),
      capabilityVersion: textFromValue(raw.capabilityVersion) || "1",
    };
  });
}

function videoDurationModeFromValue(value: JsonValue | undefined): VideoVariantForm["durationMode"] {
  const mode = textFromValue(value);
  return mode === "continuous_range" || mode === "fixed" || mode === "source_duration" ? mode : "discrete";
}

function videoFrameRateModeFromValue(value: JsonValue | undefined): VideoVariantForm["frameRateMode"] {
  const mode = textFromValue(value);
  return mode === "fixed" || mode === "selectable" ? mode : "unknown";
}

function capabilityTruthFromValue<T extends "unknown" | "any" = "unknown">(
  value: JsonValue | undefined,
  empty: T = "unknown" as T,
): "true" | "false" | T {
  if (value === true || value === "true") {
    return "true" as const;
  }
  if (value === false || value === "false") {
    return "false" as const;
  }
  return empty;
}

function catalogInstallModelBody(model: ProviderCatalogModelTemplate | ModelDraft): JsonRecord {
  const modality = model.modality || inferModelModality(model.modelKey);
  return {
    modelKey: model.modelKey,
    displayName: model.displayName || model.modelKey,
    modality,
    taskTypes: model.taskTypes?.length ? model.taskTypes : defaultTaskTypesByModality[modality] || [],
    inputLimits: model.inputLimits || {},
    outputLimits: model.outputLimits || {},
    qualityTiers: model.qualityTiers || [],
    providerOptionsSchema: model.providerOptionsSchema || {},
    pricingPolicy: model.pricingPolicy || {},
  };
}

function modelCreateBody(model: ProviderCatalogModelTemplate | ModelDraft): JsonRecord {
  const installBody = catalogInstallModelBody(model);
  return {
    modelKey: String(installBody.modelKey),
    displayName: String(installBody.displayName),
    modality: String(installBody.modality),
    status: "active",
    capabilities: {
      taskTypes: installBody.taskTypes,
      inputLimits: installBody.inputLimits,
      outputLimits: installBody.outputLimits,
      qualityTiers: installBody.qualityTiers,
      providerOptionsSchema: installBody.providerOptionsSchema,
      pricingPolicy: installBody.pricingPolicy,
    },
  };
}

function inferModelModality(modelKey: string) {
  const normalized = modelKey.toLowerCase();
  if (normalized.includes("video") || normalized.includes("seedance") || normalized.includes("kling")) {
    return "video";
  }
  if (normalized.includes("image") || normalized.includes("imagine") || normalized.includes("seedream")) {
    return "image";
  }
  if (normalized.includes("whisper") || normalized.includes("tts") || normalized.includes("speech") || normalized.includes("audio")) {
    return "audio";
  }
  return "text";
}

function modelFormFromTemplate(template: ProviderCatalogModelTemplate): ModelForm {
  const taskTypes = template.taskTypes.length ? template.taskTypes : defaultTaskTypesByModality[template.modality] || [];
  const capabilityFields = capabilityFormFieldsFromValues(
    template.modality,
    taskTypes,
    template.inputLimits || {},
    template.outputLimits || {},
    template.qualityTiers || [],
    template.providerOptionsSchema || {},
  );
  return {
    modelKey: template.modelKey,
    displayName: template.displayName,
    modality: template.modality,
    status: "active",
    supportsAsyncTask: readSupportsAsyncTask(template.providerOptionsSchema, taskTypes, template.modality),
    ...capabilityFields,
    taskTypesText: taskTypes.join("\n"),
    inputLimitsText: jsonText(template.inputLimits || {}),
    outputLimitsText: jsonText(template.outputLimits || {}),
    qualityTiersText: jsonText(template.qualityTiers || []),
    providerOptionsSchemaText: jsonText(template.providerOptionsSchema || {}),
    pricingPolicyText: jsonText(template.pricingPolicy || {}),
  };
}

function modelFormFromModel(model: ProviderModel): ModelForm {
  const capability = model.capabilities?.[0];
  const taskTypes = modelTaskTypes(model);
  const capabilityFields = capabilityFormFieldsFromValues(
    model.modality,
    taskTypes,
    capability?.inputLimits || {},
    capability?.outputLimits || {},
    capability?.qualityTiers || [],
    capability?.providerOptionsSchema || {},
  );
  return {
    modelKey: model.modelKey,
    displayName: model.displayName,
    modality: model.modality,
    status: model.status,
    supportsAsyncTask: readSupportsAsyncTask(capability?.providerOptionsSchema, taskTypes, model.modality),
    ...capabilityFields,
    taskTypesText: taskTypes.join("\n"),
    inputLimitsText: jsonText(capability?.inputLimits || {}),
    outputLimitsText: jsonText(capability?.outputLimits || {}),
    qualityTiersText: jsonText(capability?.qualityTiers || []),
    providerOptionsSchemaText: jsonText(capability?.providerOptionsSchema || {}),
    pricingPolicyText: jsonText(capability?.pricingPolicy || {}),
  };
}

function catalogSetupFields(entry: ProviderCatalogEntry | null) {
  return Array.isArray(entry?.setupSchema?.fields) ? entry.setupSchema.fields : [];
}

function defaultSetupValues(entry: ProviderCatalogEntry | null) {
  return catalogSetupFields(entry).reduce<Record<string, string>>((acc, field) => {
    const defaultValue = field.defaultValue ?? entry?.setupSchema?.defaultConfig?.[field.key] ?? "";
    acc[field.key] = String(defaultValue ?? "");
    return acc;
  }, {});
}

function jsonText(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function parseJsonRecord(text: string, label: string): JsonRecord | null {
  const value = parseJsonValue(text, label);
  if (value === undefined) {
    return null;
  }
  if (!isRecord(value)) {
    toast.error(`${label}必须是 JSON 对象`);
    return null;
  }
  return value as JsonRecord;
}

function parseJsonValue(text: string, label: string): JsonValue | undefined {
  try {
    return JSON.parse(text.trim() || "{}") as JsonValue;
  } catch {
    toast.error(`${label}不是有效 JSON`);
    return undefined;
  }
}

function buildCapabilityFromModelForm(modelForm: ModelForm, taskTypes: string[]) {
  const inputLimits = safeJsonRecord(modelForm.inputLimitsText);
  const outputLimits = safeJsonRecord(modelForm.outputLimitsText);
  const providerOptionsSchema = safeJsonRecord(modelForm.providerOptionsSchemaText);
  const pricingPolicy = safeJsonRecord(modelForm.pricingPolicyText);
  const qualityTiersFromState = safeJsonValue(modelForm.qualityTiersText);
  if (!inputLimits || !outputLimits || !providerOptionsSchema || !pricingPolicy || qualityTiersFromState === undefined) {
    toast.error("模型能力配置无效");
    return null;
  }

  const xCapabilities = isPlainRecord(providerOptionsSchema.xCapabilities) ? { ...providerOptionsSchema.xCapabilities } : {};
  const supportedInputTypes = splitList(modelForm.supportedInputTypesText);
  const supportedOutputTypes = splitList(modelForm.supportedOutputTypesText);
  const textInputTokens = parseOptionalNumber(modelForm.maxInputTokens, "最大输入 Token");
  const textOutputTokens = parseOptionalNumber(modelForm.maxOutputTokens, "最大输出 Token");
  const promptMaxLength = parseOptionalNumber(modelForm.promptMaxLength, "Prompt 最大长度");
  const maxReferenceImages = parseOptionalNumber(modelForm.maxReferenceImages, "参考图数量上限");
  const maxReferenceVideos = parseOptionalNumber(modelForm.maxReferenceVideos, "参考视频数量上限");
  const maxTTSCharacters = parseOptionalNumber(modelForm.maxTTSCharacters, "单次合成文本上限");
  const maxAudioDurationSeconds = parseOptionalNumber(modelForm.maxAudioDurationSeconds, "单次识别音频上限");
  if (
    textInputTokens === null ||
    textOutputTokens === null ||
    promptMaxLength === null ||
    maxReferenceImages === null ||
    maxReferenceVideos === null ||
    maxTTSCharacters === null ||
    maxAudioDurationSeconds === null
  ) {
    return null;
  }

  if (supportedInputTypes.length > 0) {
    inputLimits.inputTypes = supportedInputTypes;
    xCapabilities.supportedInputTypes = supportedInputTypes;
  }
  if (supportedOutputTypes.length > 0) {
    outputLimits.outputTypes = supportedOutputTypes;
    xCapabilities.supportedOutputTypes = supportedOutputTypes;
  }
  if (textInputTokens !== undefined) {
    inputLimits.maxTokens = textInputTokens;
  }
  if (textOutputTokens !== undefined) {
    outputLimits.maxTokens = textOutputTokens;
  }
  if (promptMaxLength !== undefined) {
    inputLimits.promptMaxLength = promptMaxLength;
    inputLimits.promptLengthUnit = modelForm.promptLengthUnit;
    xCapabilities.promptMaxLength = promptMaxLength;
    xCapabilities.promptLengthUnit = modelForm.promptLengthUnit;
  } else {
    delete inputLimits.promptMaxLength;
    delete inputLimits.promptLengthUnit;
    delete xCapabilities.promptMaxLength;
    delete xCapabilities.promptLengthUnit;
  }

  xCapabilities.supportsAsyncTask = modelForm.supportsAsyncTask;
  xCapabilities.supportsStreaming = modelForm.supportsStreaming;
  if (modelForm.supportsStreaming) {
    xCapabilities.streamTerminalMode = modelForm.streamTerminalMode;
  } else {
    delete xCapabilities.streamTerminalMode;
  }
  xCapabilities.supportsReasoning = modelForm.supportsReasoning;
  xCapabilities.supportsReasoningLevels = modelForm.supportsReasoningLevels;
  const reasoningLevels = splitList(modelForm.reasoningLevelsText);
  if (modelForm.supportsReasoningLevels) {
    if (reasoningLevels.length === 0) {
      toast.error("支持思考等级时必须填写至少一个可用等级");
      return null;
    }
    xCapabilities.reasoningLevels = reasoningLevels;
  } else {
    delete xCapabilities.reasoningLevels;
  }
  xCapabilities.supportsMultimodalInput = modelForm.supportsMultimodalInput;

  let qualityTiers = Array.isArray(qualityTiersFromState) ? qualityTiersFromState : [];
  if (modelForm.modality === "image" || modelForm.modality === "multimodal") {
    const requestModes = splitList(modelForm.imageRequestModesText);
    if (modelForm.supportsImageEdit && !requestModes.includes("images.edit")) {
      requestModes.push("images.edit");
    }
    const imageAspectRatios = splitList(modelForm.imageAspectRatiosText);
    const imageResolutions = splitList(modelForm.imageResolutionsText);
    const imageQualityTiers = splitList(modelForm.imageQualityTiersText);
    const imageResponseFormats = splitList(modelForm.imageResponseFormatsText);
    xCapabilities.supportsReferences = modelForm.supportsReferenceImages;
    xCapabilities.supportsReferenceImages = modelForm.supportsReferenceImages;
    xCapabilities.requestModes = requestModes;
    if (maxReferenceImages !== undefined) {
      inputLimits.maxReferenceImages = maxReferenceImages;
      xCapabilities.maxReferenceImages = maxReferenceImages;
    }
    if (imageAspectRatios.length > 0) {
      xCapabilities.supportedAspectRatios = imageAspectRatios;
    }
    if (imageResolutions.length > 0) {
      xCapabilities.supportedResolutions = imageResolutions;
    }
    if (imageQualityTiers.length > 0) {
      const supportsAutoQuality = arrayFromValue(xCapabilities.quality).includes("auto");
      xCapabilities.quality = supportsAutoQuality
        ? uniqueStrings(["auto", ...imageQualityTiers])
        : imageQualityTiers;
      qualityTiers = imageQualityTiers;
    }
    if (imageResponseFormats.length > 0) {
      outputLimits.responseFormats = imageResponseFormats;
      xCapabilities.responseFormats = imageResponseFormats;
    }
  }

  if (modelForm.modality === "video" || modelForm.modality === "multimodal") {
    const videoVariants = buildVideoGenerationVariants(modelForm.videoVariants);
    if (!videoVariants) {
      return null;
    }
    const videoRequestModes = uniqueStrings(videoVariants.flatMap((variant) => variant.requestModes as string[]));
    const videoAspectRatios = uniqueStrings(videoVariants.flatMap((variant) => variant.aspectRatios as string[]));
    const videoResolutions = uniqueStrings(videoVariants.flatMap((variant) => variant.resolutions as string[]));
    const videoOutputFormats = splitList(modelForm.videoOutputFormatsText);
    const referenceModes = uniqueStrings(videoVariants.flatMap((variant) => {
      const when = variant.when as JsonRecord;
      return Array.isArray(when.referenceModes) ? when.referenceModes.map(String) : [];
    }));
    const continuations = videoVariants.map((variant) => variant.continuation as JsonRecord);
    xCapabilities.videoGenerationVariants = videoVariants;
    xCapabilities.requestModes = videoRequestModes;
    xCapabilities.supportsReferenceImages = referenceModes.includes("first_frame");
    xCapabilities.supportsFirstFrame = continuations.some((item) => item.supportsFirstFrame === true);
    xCapabilities.supportsLastFrame = continuations.some((item) => item.supportsLastFrame === true);
    xCapabilities.supportsVideoReference = continuations.some((item) => item.supportsVideoReference === true);
    delete xCapabilities.minDurationSeconds;
    delete xCapabilities.maxDurationSeconds;
    delete xCapabilities.durations;
    if (maxReferenceImages !== undefined) {
      inputLimits.maxReferenceImages = maxReferenceImages;
      xCapabilities.maxReferenceImages = maxReferenceImages;
    }
    if (maxReferenceVideos !== undefined) {
      inputLimits.maxReferenceVideos = maxReferenceVideos;
      xCapabilities.maxReferenceVideos = maxReferenceVideos;
    }
    if (videoAspectRatios.length > 0) {
      xCapabilities.supportedAspectRatios = videoAspectRatios;
    }
    if (videoResolutions.length > 0) {
      xCapabilities.supportedResolutions = videoResolutions;
      qualityTiers = videoResolutions;
    }
    if (videoOutputFormats.length > 0) {
      outputLimits.outputTypes = videoOutputFormats;
      xCapabilities.supportedOutputTypes = videoOutputFormats;
    }
  }

  if (modelForm.modality === "audio" || modelForm.modality === "multimodal") {
    const audioVoices = splitList(modelForm.audioVoicesText);
    const audioLanguages = splitList(modelForm.audioLanguagesText);
    const audioInputFormats = splitList(modelForm.audioInputFormatsText);
    const audioOutputFormats = splitList(modelForm.audioOutputFormatsText);
    const audioRequestModes = splitList(modelForm.audioRequestModesText);
    xCapabilities.supportsTTS = modelForm.supportsTTS;
    xCapabilities.supportsTranscription = modelForm.supportsTranscription;
    xCapabilities.audioVoices = audioVoices;
    xCapabilities.audioLanguages = audioLanguages;
    xCapabilities.audioInputFormats = audioInputFormats;
    xCapabilities.audioResponseFormats = audioOutputFormats;
    xCapabilities.audioRequestModes = audioRequestModes;
    if (maxTTSCharacters !== undefined) {
      inputLimits.maxTTSCharacters = maxTTSCharacters;
      xCapabilities.maxTTSCharacters = maxTTSCharacters;
    }
    if (maxAudioDurationSeconds !== undefined) {
      inputLimits.maxAudioDurationSeconds = maxAudioDurationSeconds;
      xCapabilities.maxAudioDurationSeconds = maxAudioDurationSeconds;
    }
    if (audioInputFormats.length > 0) {
      inputLimits.audioFormats = audioInputFormats;
    }
    if (audioOutputFormats.length > 0) {
      outputLimits.audioFormats = audioOutputFormats;
    }
  }

  return {
    taskTypes,
    inputLimits,
    outputLimits,
    qualityTiers,
    providerOptionsSchema: {
      ...providerOptionsSchema,
      xCapabilities,
    },
    pricingPolicy,
  };
}

function buildVideoGenerationVariants(forms: VideoVariantForm[]): JsonRecord[] | null {
  if (forms.length === 0) {
    toast.error("请至少配置一个视频能力组合");
    return null;
  }
  const keys = new Set<string>();
  const variants: JsonRecord[] = [];
  for (const [index, form] of forms.entries()) {
    const label = `视频能力 ${index + 1}`;
    const variantKey = form.variantKey.trim();
    if (!variantKey) {
      toast.error(`${label}缺少能力标识`);
      return null;
    }
    if (keys.has(variantKey)) {
      toast.error(`视频能力标识 ${variantKey} 重复`);
      return null;
    }
    keys.add(variantKey);
    const duration: JsonRecord = { mode: form.durationMode };
    if (form.durationMode === "continuous_range") {
      const minSeconds = parseOptionalNumber(form.minDurationSeconds, `${label}最短秒数`);
      const maxSeconds = parseOptionalNumber(form.maxDurationSeconds, `${label}最长秒数`);
      const stepSeconds = parseOptionalNumber(form.durationStepSeconds, `${label}步进秒数`);
      if (minSeconds === null || maxSeconds === null || stepSeconds === null) {
        return null;
      }
      if (minSeconds === undefined || maxSeconds === undefined || minSeconds <= 0 || maxSeconds < minSeconds || (stepSeconds ?? 0) < 0) {
        toast.error(`${label}的连续时长范围无效`);
        return null;
      }
      duration.minSeconds = minSeconds;
      duration.maxSeconds = maxSeconds;
      if (stepSeconds !== undefined) {
        duration.stepSeconds = stepSeconds;
      }
    } else if (form.durationMode === "discrete" || form.durationMode === "fixed") {
      const values = parseNumberList(form.durationValuesText, `${label}可用秒数`);
      if (!values || values.length === 0 || values.some((value) => value <= 0)) {
        toast.error(`${label}需要正数时长`);
        return null;
      }
      if (form.durationMode === "fixed" && values.length !== 1) {
        toast.error(`${label}的固定时长只能填写一个值`);
        return null;
      }
      duration.values = uniqueNumbers(values);
    }
    const frameRates = parseNumberList(form.frameRatesText, `${label}帧率`);
    if (frameRates === null) {
      return null;
    }
    if (form.frameRateMode !== "unknown" && (frameRates.length === 0 || frameRates.some((value) => value <= 0))) {
      toast.error(`${label}需要有效帧率`);
      return null;
    }
    if (form.frameRateMode === "fixed" && frameRates.length !== 1) {
      toast.error(`${label}的固定帧率只能填写一个值`);
      return null;
    }
    const when: JsonRecord = {
      taskTypes: splitList(form.taskTypesText),
      referenceModes: splitList(form.referenceModesText),
    };
    const nativeAudioRequested = truthToOptionalBoolean(form.nativeAudioRequested);
    if (nativeAudioRequested !== undefined) {
      when.nativeAudioRequested = nativeAudioRequested;
    }
    const nativeAudio: JsonRecord = {
      support: form.nativeAudioSupport,
      supportedDialogueLanguages: splitList(form.dialogueLanguagesText),
      audioTrackSeparable: form.audioTrackSeparable,
    };
    for (const [key, value] of [
      ["canDisable", form.nativeAudioCanDisable],
      ["supportsDialogue", form.supportsDialogue],
      ["supportsVoiceover", form.supportsVoiceover],
      ["supportsAmbientSound", form.supportsAmbientSound],
      ["supportsMusic", form.supportsMusic],
      ["supportsLipSync", form.supportsLipSync],
    ] as Array<[string, CapabilityTruth]>) {
      const resolved = truthToOptionalBoolean(value);
      if (resolved !== undefined) {
        nativeAudio[key] = resolved;
      }
    }
    variants.push({
      variantKey,
      ...(form.modelFamily.trim() ? { modelFamily: form.modelFamily.trim() } : {}),
      when,
      duration,
      resolutions: splitList(form.resolutionsText),
      aspectRatios: splitList(form.aspectRatiosText),
      frameRate: { mode: form.frameRateMode, values: uniqueNumbers(frameRates) },
      supportedPromptLanguages: splitList(form.promptLanguagesText),
      nativeAudio,
      continuation: {
        supportsExtension: form.supportsExtension,
        supportsFirstFrame: form.supportsFirstFrame,
        supportsLastFrame: form.supportsLastFrame,
        supportsVideoReference: form.supportsVideoReference,
      },
      requestModes: splitList(form.requestModesText),
      source: form.source.trim() || "user",
      ...(form.sourceUrl.trim() ? { sourceUrl: form.sourceUrl.trim() } : {}),
      capabilityVersion: form.capabilityVersion.trim() || "1",
    });
  }
  return variants;
}

function truthToOptionalBoolean(value: string) {
  return value === "true" ? true : value === "false" ? false : undefined;
}

function uniqueStrings(values: string[]) {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean)));
}

function uniqueNumbers(values: number[]) {
  return Array.from(new Set(values)).sort((left, right) => left - right);
}

function safeJsonRecord(text: string): JsonRecord | null {
  const value = safeJsonValue(text);
  return value !== undefined && isRecord(value) ? value : null;
}

function safeJsonValue(text: string): JsonValue | undefined {
  try {
    return JSON.parse(text.trim() || "{}") as JsonValue;
  } catch {
    return undefined;
  }
}

function isRecord(value: JsonValue): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function taskTypesFromText(text: string) {
  return text
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function splitList(text: string) {
  return text
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function listText(values: unknown[]) {
  return values.map((value) => String(value).trim()).filter(Boolean).join("\n");
}

function arrayFromValue(value: JsonValue | undefined): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map(String).filter(Boolean);
}

function textFromValue(value: JsonValue | undefined) {
  return value === undefined || value === null ? "" : String(value);
}

function booleanFromValue(value: JsonValue | undefined, fallback: boolean) {
  return typeof value === "boolean" ? value : fallback;
}

function streamTerminalModeFromValue(value: JsonValue | undefined): ModelForm["streamTerminalMode"] {
  return value === "done_marker" || value === "finish_reason" || value === "done_or_finish_reason"
    ? value
    : "done_or_finish_reason";
}

function parseOptionalNumber(text: string, label: string) {
  const trimmed = text.trim();
  if (!trimmed) {
    return undefined;
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value)) {
    toast.error(`${label}必须是数字`);
    return null;
  }
  return value;
}

function parseNumberList(text: string, label: string) {
  const values = splitList(text);
  const numbers: number[] = [];
  for (const value of values) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) {
      toast.error(`${label}必须是数字`);
      return null;
    }
    numbers.push(parsed);
  }
  return numbers;
}

function defaultBusinessBindingDraft(): BusinessModelBindingDraft {
  return {
    modelId: "",
    priority: "100",
    weight: "100",
    enabled: true,
    reasoningLevel: "",
  };
}

function defaultFallbackStrategy(): JsonRecord {
  return {
    enabled: true,
    maxAttempts: 2,
    fallbackOn: ["PROVIDER_RATE_LIMITED", "UPSTREAM_TIMEOUT", "UPSTREAM_INTERNAL_ERROR"],
    stopOn: ["INVALID_REQUEST", "AUTHENTICATION_FAILED"],
  };
}

function parseIntegerOrDefault(text: string, fallback: number, label: string) {
  const trimmed = text.trim();
  if (!trimmed) {
    return fallback;
  }
  const value = Number(trimmed);
  if (!Number.isInteger(value) || value < 0) {
    toast.error(`${label}必须是非负整数`);
    return null;
  }
  return value;
}

function businessProfilePurposeLabel(purpose: string) {
  switch (purpose) {
    case "script":
      return "脚本/事件";
    case "image":
      return "图片";
    case "video":
      return "视频";
    default:
      return purpose;
  }
}

function modelMatchesBusinessSlot(model: ProviderModel, slot: BusinessProfileSlot) {
  if (!slot.modalities.includes(model.modality)) {
    return false;
  }
  const modelTasks = modelTaskTypes(model);
  if (modelTasks.length === 0) {
    return true;
  }
  const slotTaskFamilies = new Set(slot.taskTypes.map(taskTypeFamily));
  return (
    slot.taskTypes.some((taskType) => modelTasks.includes(taskType)) ||
    modelTasks.some((taskType) => slotTaskFamilies.has(taskTypeFamily(taskType)))
  );
}

function taskTypeFamily(taskType: string) {
  return taskType.split(".")[0] || taskType;
}

function defaultSupportsAsyncTask(modality: string) {
  return inferSupportsAsyncTask(defaultTaskTypesByModality[modality] || [], {});
}

function readSupportsAsyncTask(providerOptionsSchema: JsonRecord | undefined, taskTypes: string[], modality: string) {
  const xCapabilities = isPlainRecord(providerOptionsSchema?.xCapabilities) ? providerOptionsSchema.xCapabilities : {};
  if (typeof xCapabilities.supportsAsyncTask === "boolean") {
    return xCapabilities.supportsAsyncTask;
  }
  return inferSupportsAsyncTask(taskTypes.length > 0 ? taskTypes : defaultTaskTypesByModality[modality] || [], xCapabilities);
}

function inferSupportsAsyncTask(taskTypes: string[], xCapabilities: JsonRecord) {
  if (taskTypes.some((taskType) => /\.(create_task|poll_task|cancel_task)$/.test(taskType.trim()))) {
    return true;
  }
  const requestModes = Array.isArray(xCapabilities.requestModes) ? xCapabilities.requestModes.map(String) : [];
  return requestModes.some((mode) => {
    const normalized = mode.trim().toLowerCase();
    return normalized.includes("async") || normalized === "poll" || normalized === "async_poll";
  });
}

const portaledControlSelectors = [
  "[role='listbox']",
  "[data-radix-select-viewport]",
  "[data-radix-popper-content-wrapper]",
  "[data-radix-select-content]",
  "[data-slot='dialog-content']",
];

function preventDialogCloseFromPortaledControl(event: Event) {
  const sourceEvent = (event as Event & { detail?: { originalEvent?: Event } }).detail?.originalEvent;
  if (eventMatchesAnySelector(event, portaledControlSelectors) || (sourceEvent && eventMatchesAnySelector(sourceEvent, portaledControlSelectors))) {
    event.preventDefault();
  }
}

function eventMatchesAnySelector(event: Event, selectors: string[]) {
  const target = event.target;
  if (target instanceof Element && selectors.some((selector) => target.closest(selector))) {
    return true;
  }
  if (typeof event.composedPath !== "function") {
    return false;
  }
  return event.composedPath().some((item) => item instanceof Element && selectors.some((selector) => item.matches(selector) || item.closest(selector)));
}

function modelTaskTypes(model: ProviderModel) {
  const capability = model.capabilities?.[0] as ProviderModelCapability | undefined;
  const value = capability?.taskTypes;
  if (Array.isArray(value)) {
    return value.map(String);
  }
  if (typeof value === "string") {
    return taskTypesFromText(value);
  }
  return defaultTaskTypesByModality[model.modality] || [];
}

function providerModelReasoningLevels(model: ProviderModel) {
  const levels: string[] = [];
  const seen = new Set<string>();
  for (const capability of model.capabilities || []) {
    const options = capability.providerOptionsSchema;
    const xCapabilities = isPlainRecord(options?.xCapabilities) ? options.xCapabilities : {};
    for (const level of arrayFromValue(xCapabilities.reasoningLevels)) {
      const normalized = level.trim();
      const key = normalized.toLowerCase();
      if (!key || seen.has(key)) {
        continue;
      }
      seen.add(key);
      levels.push(normalized);
    }
  }
  return levels;
}

function modelCapabilityLabels(model: ProviderModel) {
  const capability = model.capabilities?.[0] as ProviderModelCapability | undefined;
  const options = capability?.providerOptionsSchema;
  const xCapabilities = isPlainRecord(options?.xCapabilities) ? options.xCapabilities : {};
  const labels: string[] = [];
  if (xCapabilities.supportsStreaming === true) {
    labels.push("流式");
  }
  if (xCapabilities.supportsReasoning === true) {
    const levels = Array.isArray(xCapabilities.reasoningLevels) ? xCapabilities.reasoningLevels.map(String).join("/") : "";
    labels.push(levels ? `思考 ${levels}` : "思考");
  }
  if (xCapabilities.supportsMultimodalInput === true) {
    labels.push("多模态输入");
  }
  if (xCapabilities.supportsAsyncTask === true) {
    labels.push("异步任务");
  }
  if (xCapabilities.supportsTTS === true) {
    labels.push("语音合成");
  }
  if (xCapabilities.supportsTranscription === true) {
    labels.push("语音识别");
  }
  if (xCapabilities.supportsReferences === true || xCapabilities.supportsReferenceImages === true) {
    labels.push("参考图");
  }
  if (xCapabilities.supportsFirstFrame === true) {
    labels.push("首帧");
  }
  if (xCapabilities.supportsLastFrame === true) {
    labels.push("尾帧");
  }
  if (xCapabilities.supportsVideoReference === true) {
    labels.push("视频参考");
  }
  if (Array.isArray(xCapabilities.videoGenerationVariants) && xCapabilities.videoGenerationVariants.length > 0) {
    labels.push(`${xCapabilities.videoGenerationVariants.length} 个视频能力`);
    const audioSupports = xCapabilities.videoGenerationVariants
      .filter(isRecord)
      .map((variant) => isPlainRecord(variant.nativeAudio) ? variant.nativeAudio.support : undefined);
    if (audioSupports.includes("true")) {
      labels.push("原生音频");
    } else if (audioSupports.includes("unknown")) {
      labels.push("原生音频待验证");
    }
  }
  if (Array.isArray(xCapabilities.requestModes) && xCapabilities.requestModes.length > 0) {
    labels.push(`请求 ${xCapabilities.requestModes.map(String).join("/")}`);
  }
  return labels;
}

function isPlainRecord(value: JsonValue | undefined): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function groupModelsByModality(models: ProviderModel[]) {
  const order = ["text", "image", "video", "audio", "multimodal"];
  return order
    .map((modality) => ({
      modality,
      models: models.filter((model) => model.modality === modality),
    }))
    .filter((group) => group.models.length > 0);
}

function modalityLabel(value: string) {
  return modalityOptions.find((option) => option.value === value)?.label || value;
}

function statusLabel(value?: string) {
  return statusOptions.find((option) => option.value === value)?.label || value || "未知";
}
