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
  Plus,
  RefreshCw,
  Sparkles,
  Trash2,
  X,
  XCircle,
  Zap,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

type AccountDialogMode = "create" | "edit";
type ModelDialogMode = "create" | "edit";
type ModelDraft = ProviderCatalogModelTemplate & { source: "catalog" | "custom" };

type AccountForm = {
  name: string;
  baseUrl: string;
  authType: string;
  status: string;
  apiKey: string;
  setup: Record<string, string>;
  configText: string;
};

type ModelForm = {
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
  supportsAsyncTask: boolean;
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
  multimodal: ["text.generate", "text.stream", "image.generate", "video.create_task", "video.poll_task"],
};

const modalityOptions = [
  { value: "text", label: "文本" },
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
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
  const [modelToDelete, setModelToDelete] = useState<ProviderModel | null>(null);
  const [modelForm, setModelForm] = useState<ModelForm>(emptyModelForm("text"));
  const invalidate = useInvalidateKeys();
  const lastDialogInnerPointerDownAtRef = useRef(0);
  const portaledControlOpenUntilRef = useRef(0);

  const { data: catalogData } = useApiQuery({
    key: qk.providerCatalog(),
    queryFn: (session) => studioApi.listProviderCatalog(session),
  });
  const catalogEntries = (catalogData?.items || []) as ProviderCatalogEntry[];

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

  const createAccountMutation = useApiMutation({
    mutationFn: (session, data: { providerKey: string; body: JsonRecord }) =>
      studioApi.installProviderCatalogEntry(session, data.providerKey, data.body),
    onSuccess: (result) => {
      toast.success("供应商账号已创建");
      invalidate([qk.providerAccounts(), qk.providerCatalog(), qk.modelProfiles(), qk.providerModels(result.account.id)]);
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
      invalidate([qk.providerAccounts(), qk.providerCatalog(), qk.modelProfiles()]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const deleteAccountMutation = useApiMutation({
    mutationFn: (session, accountId: string) => studioApi.deleteProviderAccount(session, accountId),
    onSuccess: (_result, accountId) => {
      toast.success("供应商已删除");
      invalidate([qk.providerAccounts(), qk.modelProfiles()]);
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
      invalidate([qk.providerModels(accountId), qk.modelProfiles()]);
    },
    onError: (error) => toast.error("模型发现失败：" + error.message),
  });

  const quickCreateModelMutation = useApiMutation({
    mutationFn: (session, data: { accountId: string; model: ModelDraft | ProviderCatalogModelTemplate }) =>
      studioApi.createProviderModel(session, data.accountId, modelCreateBody(data.model)),
    onSuccess: (_result, data) => {
      invalidate([qk.providerModels(data.accountId), qk.modelProfiles()]);
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
        invalidate([qk.providerModels(selectedAccountId)]);
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
        invalidate([qk.providerModels(selectedAccountId), qk.modelProfiles()]);
      } else {
        invalidate([qk.modelProfiles()]);
      }
      if (editingModel?.id === modelId) {
        setEditingModel(null);
        setModelDialogOpen(false);
      }
      setModelToDelete(null);
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
    const preferred = catalogEntries.find((entry) => entry.providerKey === "volcengine_ark") || catalogEntries[0] || null;
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
    const inputLimits = parseJsonRecord(modelForm.inputLimitsText, "输入限制");
    const outputLimits = parseJsonRecord(modelForm.outputLimitsText, "输出限制");
    const providerOptionsSchema = parseJsonRecord(modelForm.providerOptionsSchemaText, "供应商选项");
    const pricingPolicy = parseJsonRecord(modelForm.pricingPolicyText, "计费策略");
    const qualityTiers = parseJsonValue(modelForm.qualityTiersText, "质量档位");
    if (!inputLimits || !outputLimits || !providerOptionsSchema || !pricingPolicy || qualityTiers === undefined) {
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
        capabilities: {
          taskTypes,
          inputLimits,
          outputLimits,
          qualityTiers,
          providerOptionsSchema: withSupportsAsyncTask(providerOptionsSchema, modelForm.supportsAsyncTask),
          pricingPolicy,
        },
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
                        <Badge variant="outline">{account.connectorKey || account.connectorId}</Badge>
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
            <p className="text-sm text-muted-foreground">配置业务模型档案，支持路由策略、降级和成本优化</p>
            {profilesLoading && <Skeleton className="h-64" />}
            {!profilesLoading && profiles.length === 0 && (
              <div className="rounded-lg border border-dashed p-12 text-center">
                <Zap className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
                <p className="mt-4 text-sm text-muted-foreground">暂无模型配置</p>
                <p className="mt-1 text-xs text-muted-foreground">模型配置用于业务模块绑定</p>
              </div>
            )}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {profiles.map((profile) => (
                <div key={profile.id} className="rounded-lg border p-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="font-medium">{profile.profileKey}</div>
                      <div className="truncate text-xs text-muted-foreground">{profile.profileKey}</div>
                    </div>
                    <Badge variant="outline">{profile.purpose}</Badge>
                  </div>
                  <div className="mt-3 text-xs text-muted-foreground">绑定: {profile.bindings?.length ?? 0}</div>
                </div>
              ))}
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
                              onClick={() => setModelToDelete(model)}
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
                                {taskType}
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
                            onClick={() => setModelToDelete(model)}
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
                  onValueChange={(value) =>
                    setModelForm({
                      ...modelForm,
                      modality: value,
                      supportsAsyncTask: defaultSupportsAsyncTask(value),
                      taskTypesText: defaultTaskTypesByModality[value]?.join("\n") || modelForm.taskTypesText,
                    })
                  }
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
            <div className="grid gap-3 md:grid-cols-2">
              <JsonTextarea label="输入限制 JSON" value={modelForm.inputLimitsText} onChange={(value) => setModelForm({ ...modelForm, inputLimitsText: value })} />
              <JsonTextarea label="输出限制 JSON" value={modelForm.outputLimitsText} onChange={(value) => setModelForm({ ...modelForm, outputLimitsText: value })} />
              <JsonTextarea label="质量档位 JSON" value={modelForm.qualityTiersText} onChange={(value) => setModelForm({ ...modelForm, qualityTiersText: value })} />
              <JsonTextarea label="计费策略 JSON" value={modelForm.pricingPolicyText} onChange={(value) => setModelForm({ ...modelForm, pricingPolicyText: value })} />
            </div>
            <JsonTextarea
              label="供应商选项 JSON"
              value={modelForm.providerOptionsSchemaText}
              onChange={(value) => setModelForm({ ...modelForm, providerOptionsSchemaText: value })}
              large
            />
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

      <AlertDialog open={!!modelToDelete} onOpenChange={(open) => !open && setModelToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogMedia>
              <Trash2 className="h-5 w-5 text-destructive" />
            </AlertDialogMedia>
            <AlertDialogTitle>删除模型</AlertDialogTitle>
            <AlertDialogDescription>
              删除后该模型会从当前供应商移除，并停止用于模型配置路由。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteModelMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteModelMutation.isPending}
              onClick={() => modelToDelete && deleteModelMutation.mutate(modelToDelete.id)}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </AppShell>
  );
}

function JsonTextarea({ label, value, onChange, large = false }: { label: string; value: string; onChange: (value: string) => void; large?: boolean }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Textarea
        className={cn("font-mono text-xs", large ? "min-h-36" : "min-h-24")}
        spellCheck={false}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
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
  return {
    modelKey: "",
    displayName: "",
    modality,
    status: "active",
    supportsAsyncTask: defaultSupportsAsyncTask(modality),
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
  return "text";
}

function modelFormFromTemplate(template: ProviderCatalogModelTemplate): ModelForm {
  return {
    modelKey: template.modelKey,
    displayName: template.displayName,
    modality: template.modality,
    status: "active",
    supportsAsyncTask: readSupportsAsyncTask(template.providerOptionsSchema, template.taskTypes, template.modality),
    taskTypesText: template.taskTypes.join("\n"),
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
  return {
    modelKey: model.modelKey,
    displayName: model.displayName,
    modality: model.modality,
    status: model.status,
    supportsAsyncTask: readSupportsAsyncTask(capability?.providerOptionsSchema, taskTypes, model.modality),
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

function isRecord(value: JsonValue): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function taskTypesFromText(text: string) {
  return text
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
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

function withSupportsAsyncTask(providerOptionsSchema: JsonRecord, supportsAsyncTask: boolean): JsonRecord {
  const xCapabilities = isPlainRecord(providerOptionsSchema.xCapabilities) ? providerOptionsSchema.xCapabilities : {};
  return {
    ...providerOptionsSchema,
    xCapabilities: {
      ...xCapabilities,
      supportsAsyncTask,
    },
  };
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
  if (Array.isArray(xCapabilities.requestModes) && xCapabilities.requestModes.length > 0) {
    labels.push(`请求 ${xCapabilities.requestModes.map(String).join("/")}`);
  }
  return labels;
}

function isPlainRecord(value: JsonValue | undefined): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function groupModelsByModality(models: ProviderModel[]) {
  const order = ["text", "image", "video", "multimodal"];
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
