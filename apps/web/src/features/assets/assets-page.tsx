"use client";

import { useMemo, useRef, useState } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Check, ExternalLink, Image as ImageIcon, MapPin, Package, RefreshCw, Save, Star, Trash2, Upload, User, Wand2, X } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { artifactTypeLabel, assetReferenceTypeLabel, assetTypeLabel, requirementTypeLabel, statusLabel } from "@/lib/labels";
import type { Artifact, AssetReference, CanonicalAsset, Script, ShotAssetRequirement, WorkflowRun } from "@/lib/types";

type AssetDraft = {
  name: string;
  description: string;
  basePrompt: string;
  consistencyPrompt: string;
  negativePrompt: string;
  lockReference: boolean;
};

const emptyAssetDraft: AssetDraft = {
  name: "",
  description: "",
  basePrompt: "",
  consistencyPrompt: "",
  negativePrompt: "",
  lockReference: false,
};

export function AssetsPage({
  projectId,
  initialAssetId = "",
}: {
  projectId: string;
  initialAssetId?: string;
}) {
  const [selectedAssetId, setSelectedAssetId] = useState<string | null>(initialAssetId || null);
  const [selectedScriptId, setSelectedScriptId] = useState<string>("");
  const [assetDraftState, setAssetDraftState] = useState<{ assetId: string; draft: AssetDraft } | null>(null);
  const [referenceUploadFile, setReferenceUploadFile] = useState<File | null>(null);
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const invalidate = useInvalidateKeys();

  const { data: scripts = [] } = useApiQuery({
    key: qk.scripts(projectId),
    queryFn: (session) => studioApi.listScripts(session, projectId).then((response) => response.items || []),
  });
  const { data: workflowRuns = [] } = useApiQuery({
    key: qk.workflowRuns(projectId),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId).then((response) => response.items || []),
  });
  const { data: assets = [], isLoading } = useApiQuery({
    key: qk.assets(projectId),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId).then((response) => response.items || []),
  });
  const { data: requirements = [], isLoading: requirementsLoading } = useApiQuery({
    key: qk.requirements(projectId),
    queryFn: (session) => studioApi.listShotAssetRequirements(session, projectId).then((response) => response.items || []),
  });
  const { data: artifacts = [], isLoading: artifactsLoading } = useApiQuery({
    key: qk.artifacts(projectId),
    queryFn: (session) => studioApi.listArtifacts(session, projectId).then((response) => response.items || []),
  });

  const selectedAsset = useMemo(() => assets.find((asset) => asset.id === selectedAssetId) ?? assets[0] ?? null, [assets, selectedAssetId]);
  const assetDraft = useMemo(() => {
    if (!selectedAsset) {
      return emptyAssetDraft;
    }
    if (assetDraftState?.assetId === selectedAsset.id) {
      return assetDraftState.draft;
    }
    return draftFromAsset(selectedAsset);
  }, [assetDraftState, selectedAsset]);
  const selectedScript = useMemo(() => resolveSelectedScript(scripts, selectedScriptId), [scripts, selectedScriptId]);
  const latestAssetExtractionError = useMemo(
    () => workflowRuns.find((run) => workflowRunType(run) === "script_to_assets" && run.status === "failed" && (run.errorCode || run.errorMessage)) ?? null,
    [workflowRuns],
  );
  const { data: selectedReferences = [] } = useApiQuery({
    key: qk.assetReferences(projectId, selectedAsset?.id ?? ""),
    queryFn: (session) => studioApi.listAssetReferences(session, projectId, selectedAsset!.id, true).then((response) => response.items || []),
    enabled: !!selectedAsset,
  });

  const artifactPreviewById = useMemo(() => {
    const previews = new Map<string, string>();
    for (const artifact of artifacts) {
      if (artifact.previewUrl) {
        previews.set(artifact.id, artifact.previewUrl);
      }
    }
    return previews;
  }, [artifacts]);

  const analyzeAssetsMutation = useApiMutation({
    mutationFn: (session, scriptId: string) =>
      studioApi.analyzeScriptAssets(session, projectId, scriptId, { mergeExisting: true, generateImages: false }),
    onSuccess: () => {
      toast.success("资产提取工作流已启动");
      invalidate([qk.assets(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("提取失败：" + error.message),
  });

  const generateCardMutation = useApiMutation({
    mutationFn: (session, assetId: string) => studioApi.generateAssetCard(session, projectId, assetId, {}),
    onSuccess: () => {
      toast.success("资产卡片生成已启动");
      invalidate([qk.assets(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const generateImageMutation = useApiMutation({
    mutationFn: (session, assetId: string) => studioApi.generateAssetImage(session, projectId, assetId, {}),
    onSuccess: (_response, assetId) => {
      toast.success("资产图像生成已启动");
      invalidate([qk.assets(projectId), qk.assetReferences(projectId, assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const updateAssetMutation = useApiMutation({
    mutationFn: (session, payload: { assetId: string; body: AssetDraft }) =>
      studioApi.updateCanonicalAsset(session, projectId, payload.assetId, {
        name: payload.body.name,
        description: payload.body.description,
        basePrompt: payload.body.basePrompt,
        consistencyPrompt: payload.body.consistencyPrompt,
        negativePrompt: payload.body.negativePrompt,
        lockReference: payload.body.lockReference,
      }),
    onSuccess: () => {
      toast.success("资产已保存");
      invalidate([qk.assets(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const uploadReferenceMutation = useApiMutation({
    mutationFn: async (session, payload: { assetId: string; file: File; setPrimary: boolean }) => {
      const upload = await studioApi.createAssetReferenceUploadUrl(session, projectId, payload.assetId, {
        fileName: payload.file.name,
        mimeType: payload.file.type,
        expiresSeconds: 900,
      });
      const headers = normalizeUploadHeaders(upload.headers, payload.file.type);
      const uploadResponse = await fetch(upload.uploadUrl, {
        method: upload.method || "PUT",
        headers,
        body: payload.file,
      });
      if (!uploadResponse.ok) {
        throw new Error("参考图上传失败");
      }
      return studioApi.createAssetReference(session, projectId, payload.assetId, {
        title: payload.file.name,
        storageKey: upload.storageKey,
        mimeType: payload.file.type,
        referenceType: "uploaded",
        setPrimary: payload.setPrimary,
      });
    },
    onSuccess: (_response, payload) => {
      toast.success("参考图已上传");
      setReferenceUploadFile(null);
      if (uploadInputRef.current) {
        uploadInputRef.current.value = "";
      }
      invalidate([qk.assets(projectId), qk.assetReferences(projectId, payload.assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("上传失败：" + error.message),
  });

  const setPrimaryReferenceMutation = useApiMutation({
    mutationFn: (session, payload: { assetId: string; referenceId: string }) =>
      studioApi.setPrimaryAssetReference(session, projectId, payload.assetId, payload.referenceId),
    onSuccess: (_response, payload) => {
      toast.success("主图已更新");
      invalidate([qk.assets(projectId), qk.assetReferences(projectId, payload.assetId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("设置失败：" + error.message),
  });

  const deleteReferenceMutation = useApiMutation({
    mutationFn: (session, payload: { assetId: string; referenceId: string }) =>
      studioApi.deleteAssetReference(session, projectId, payload.assetId, payload.referenceId),
    onSuccess: (_response, payload) => {
      toast.success("参考图已解绑");
      invalidate([qk.assets(projectId), qk.assetReferences(projectId, payload.assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("解绑失败：" + error.message),
  });

  const generateRequirementMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.generateDerivedAssetImage(session, projectId, requirementId),
    onSuccess: () => {
      toast.success("派生图像生成已启动");
      invalidate([qk.requirements(projectId), qk.assets(projectId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const reviewRequirementMutation = useApiMutation({
    mutationFn: (session, payload: { requirementId: string; reviewStatus: "approved" | "needs_edit" }) =>
      studioApi.reviewShotAssetRequirement(session, projectId, payload.requirementId, { reviewStatus: payload.reviewStatus }),
    onSuccess: () => {
      invalidate([qk.requirements(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const getAssetIcon = (type: string) => {
    switch (type) {
      case "character":
        return User;
      case "scene":
        return MapPin;
      case "prop":
        return Package;
      default:
        return ImageIcon;
    }
  };

  return (
    <Surface>
      <SectionTitle title="资产管理" description="管理角色、场景、道具和镜头派生素材" />

      <div className="flex flex-wrap items-end gap-3 border-b p-4">
        <div className="min-w-64 flex-1">
          <div className="mb-2 text-sm font-medium">剧本来源</div>
          <Select value={selectedScript?.id ?? ""} onValueChange={setSelectedScriptId}>
            <SelectTrigger>
              <SelectValue placeholder="选择剧本" />
            </SelectTrigger>
            <SelectContent>
              {scripts.map((script) => (
                <SelectItem key={script.id} value={script.id}>
                  {script.title}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          onClick={() => selectedScript && analyzeAssetsMutation.mutate(selectedScript.id)}
          disabled={!selectedScript || analyzeAssetsMutation.isPending}
        >
          <Wand2 className="h-4 w-4" />
          提取资产
        </Button>
      </div>
      {latestAssetExtractionError ? (
        <div className="mx-4 mt-4 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="destructive">提取失败</Badge>
            <span className="text-sm font-medium">{latestAssetExtractionError.errorCode || "ASSET_EXTRACTION_FAILED"}</span>
          </div>
          <p className="mt-2 text-sm text-muted-foreground">{latestAssetExtractionError.errorMessage || "资产提取未完成，请重新提取或检查供应商调用日志。"}</p>
        </div>
      ) : null}

      <Tabs defaultValue="assets" className="p-4">
        <TabsList>
          <TabsTrigger value="assets">
            核心资产
            <Badge variant="secondary" className="ml-2">{assets.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="requirements">
            镜头需求
            <Badge variant="secondary" className="ml-2">{requirements.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="vault">
            媒体库
            <Badge variant="secondary" className="ml-2">{artifacts.length}</Badge>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="assets" className="space-y-4">
          {isLoading && <Skeleton className="h-64" />}

          {!isLoading && assets.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <ImageIcon className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无资产</p>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {assets.map((asset) => {
              const Icon = getAssetIcon(asset.assetType);
              const previewUrl = assetPreviewUrl(asset, selectedAsset?.id === asset.id ? selectedReferences : undefined, artifactPreviewById);
              return (
                <button
                  key={asset.id}
                  onClick={() => setSelectedAssetId(asset.id)}
                  className={cn(
                    "group relative overflow-hidden rounded-lg border bg-card p-4 text-left transition hover:shadow-md",
                    selectedAsset?.id === asset.id && "ring-2 ring-primary",
                  )}
                >
                  <div className="mb-3 aspect-square overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <img alt={asset.name} className="h-full w-full object-cover" src={previewUrl} />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <Icon className="h-8 w-8 text-muted-foreground" />
                      </div>
                    )}
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-start justify-between gap-2">
                      <h3 className="font-medium leading-tight">{asset.name}</h3>
                      <Badge variant="outline">{assetTypeLabel(asset.assetType)}</Badge>
                    </div>
                    {asset.description && <p className="line-clamp-2 text-xs text-muted-foreground">{asset.description}</p>}
                    <div className="flex gap-1.5 pt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateCardMutation.mutate(asset.id);
                        }}
                        disabled={generateCardMutation.isPending}
                      >
                        <Wand2 className="mr-1 h-3 w-3" />
                        卡片
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateImageMutation.mutate(asset.id);
                        }}
                        disabled={generateImageMutation.isPending}
                      >
                        <ImageIcon className="mr-1 h-3 w-3" />
                        图像
                      </Button>
                    </div>
                  </div>
                </button>
              );
            })}
          </div>

          {selectedAsset && (
            <AssetDetailPanel
              asset={selectedAsset}
              draft={assetDraft}
              references={selectedReferences}
              uploadFile={referenceUploadFile}
              uploadInputRef={uploadInputRef}
              onDraftChange={(patch) =>
                setAssetDraftState((current) => ({
                  assetId: selectedAsset.id,
                  draft: { ...(current?.assetId === selectedAsset.id ? current.draft : draftFromAsset(selectedAsset)), ...patch },
                }))
              }
              onFileChange={setReferenceUploadFile}
              onSave={() => updateAssetMutation.mutate({ assetId: selectedAsset.id, body: assetDraft })}
              onGenerateCard={() => generateCardMutation.mutate(selectedAsset.id)}
              onGenerateImage={() => generateImageMutation.mutate(selectedAsset.id)}
              onUpload={(setPrimary) => {
                if (!referenceUploadFile) {
                  toast.error("请选择参考图文件");
                  return;
                }
                uploadReferenceMutation.mutate({ assetId: selectedAsset.id, file: referenceUploadFile, setPrimary });
              }}
              onSetPrimary={(referenceId) => setPrimaryReferenceMutation.mutate({ assetId: selectedAsset.id, referenceId })}
              onDeleteReference={(referenceId) => deleteReferenceMutation.mutate({ assetId: selectedAsset.id, referenceId })}
              isSaving={updateAssetMutation.isPending}
              isGeneratingCard={generateCardMutation.isPending}
              isGeneratingImage={generateImageMutation.isPending}
              isUploading={uploadReferenceMutation.isPending}
              isSettingPrimary={setPrimaryReferenceMutation.isPending}
              isDeletingReference={deleteReferenceMutation.isPending}
            />
          )}
        </TabsContent>

        <TabsContent value="requirements" className="space-y-3">
          {requirementsLoading && <Skeleton className="h-48" />}
          {!requirementsLoading && requirements.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无镜头资产需求</p>
            </div>
          )}
          <div className="grid gap-3">
            {requirements.map((requirement) => {
              const previewUrl = requirementPreviewUrl(requirement, artifactPreviewById);
              return (
                <div key={requirement.id} className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[160px_1fr]">
                  <div className="aspect-video overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <img alt={requirement.assetName || requirement.assetId} className="h-full w-full object-cover" src={previewUrl} />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <ImageIcon className="h-6 w-6 text-muted-foreground" />
                      </div>
                    )}
                  </div>
                  <div className="grid gap-2">
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div>
                        <div className="font-medium">{requirement.assetName || requirement.assetId}</div>
                        <div className="text-xs text-muted-foreground">
                          {assetTypeLabel(requirement.assetType || requirement.asset?.assetType)} · {requirementTypeLabel(requirement.requirementType)}
                        </div>
                      </div>
                      <div className="flex gap-2">
                        <Badge variant="outline">{statusLabel(requirement.status)}</Badge>
                        <Badge variant="secondary">{statusLabel(requirement.reviewStatus || "pending")}</Badge>
                      </div>
                    </div>
                    <p className="line-clamp-2 text-sm text-muted-foreground">{requirement.prompt || requirement.action || requirement.roleInShot || "未设置描述"}</p>
                    <div className="flex flex-wrap gap-2">
                      <Button size="sm" variant="outline" onClick={() => reviewRequirementMutation.mutate({ requirementId: requirement.id, reviewStatus: "approved" })}>
                        <Check className="mr-1 h-3.5 w-3.5" />
                        确认
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => reviewRequirementMutation.mutate({ requirementId: requirement.id, reviewStatus: "needs_edit" })}>
                        <X className="mr-1 h-3.5 w-3.5" />
                        需修改
                      </Button>
                      <Button size="sm" onClick={() => generateRequirementMutation.mutate(requirement.id)} disabled={generateRequirementMutation.isPending}>
                        <RefreshCw className="mr-1 h-3.5 w-3.5" />
                        生成图像
                      </Button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="vault" className="space-y-3">
          {artifactsLoading && <Skeleton className="h-48" />}
          {!artifactsLoading && artifacts.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无媒体文件</p>
            </div>
          )}
          <ArtifactGrid artifacts={artifacts} />
        </TabsContent>
      </Tabs>
    </Surface>
  );
}

function ArtifactGrid({ artifacts }: { artifacts: Artifact[] }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {artifacts.map((artifact) => (
        <div key={artifact.id} className="overflow-hidden rounded-lg border">
          <ArtifactPreview artifact={artifact} />
          <div className="grid gap-2 p-3">
            <div className="flex items-center justify-between gap-2">
              <Badge variant="outline">{artifactTypeLabel(artifact.type)}</Badge>
              {artifact.previewUrl ? (
                <a className="inline-flex items-center gap-1 text-sm text-primary" href={artifact.previewUrl} rel="noreferrer" target="_blank">
                  打开
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              ) : null}
            </div>
            <p className="truncate text-xs text-muted-foreground">{formatArtifactSummary(artifact)}</p>
          </div>
        </div>
      ))}
    </div>
  );
}

function AssetDetailPanel({
  asset,
  draft,
  references,
  uploadFile,
  uploadInputRef,
  onDraftChange,
  onFileChange,
  onSave,
  onGenerateCard,
  onGenerateImage,
  onUpload,
  onSetPrimary,
  onDeleteReference,
  isSaving,
  isGeneratingCard,
  isGeneratingImage,
  isUploading,
  isSettingPrimary,
  isDeletingReference,
}: {
  asset: CanonicalAsset;
  draft: AssetDraft;
  references: AssetReference[];
  uploadFile: File | null;
  uploadInputRef: { current: HTMLInputElement | null };
  onDraftChange: (patch: Partial<AssetDraft>) => void;
  onFileChange: (file: File | null) => void;
  onSave: () => void;
  onGenerateCard: () => void;
  onGenerateImage: () => void;
  onUpload: (setPrimary: boolean) => void;
  onSetPrimary: (referenceId: string) => void;
  onDeleteReference: (referenceId: string) => void;
  isSaving: boolean;
  isGeneratingCard: boolean;
  isGeneratingImage: boolean;
  isUploading: boolean;
  isSettingPrimary: boolean;
  isDeletingReference: boolean;
}) {
  const selectedPreview = assetPreviewUrl(asset, references, new Map());
  const canSave = draft.name.trim() !== "" && draft.description.trim() !== "";

  return (
    <div className="grid gap-4 rounded-lg border bg-background p-4 lg:grid-cols-[minmax(0,1fr)_360px]">
      <div className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold">{asset.name}</h2>
              <Badge variant="outline">{assetTypeLabel(asset.assetType)}</Badge>
              <Badge variant="secondary">{statusLabel(asset.status)}</Badge>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">出现分场 {asset.sceneCount ?? asset.sceneLinks?.length ?? 0} 个，参考图 {references.length} 张</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={onGenerateCard} disabled={isGeneratingCard}>
              <Wand2 className="h-4 w-4" />
              重生成卡片
            </Button>
            <Button variant="outline" onClick={onGenerateImage} disabled={isGeneratingImage}>
              <ImageIcon className="h-4 w-4" />
              重生成图像
            </Button>
            <Button onClick={onSave} disabled={isSaving || !canSave}>
              <Save className="h-4 w-4" />
              保存资产
            </Button>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="asset-name">名称</Label>
            <Input id="asset-name" value={draft.name} onChange={(event) => onDraftChange({ name: event.target.value })} />
          </div>
          <div className="flex items-end gap-3 rounded-lg border p-3">
            <Switch checked={draft.lockReference} onCheckedChange={(checked) => onDraftChange({ lockReference: checked })} />
            <div>
              <div className="text-sm font-medium">锁定当前参考图</div>
              <div className="text-xs text-muted-foreground">后续重生成时优先保持当前主图身份</div>
            </div>
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="asset-description">描述</Label>
          <Textarea id="asset-description" className="min-h-24" value={draft.description} onChange={(event) => onDraftChange({ description: event.target.value })} />
        </div>

        <div className="grid gap-3">
          <PromptField
            id="asset-base-prompt"
            label="基础提示词"
            value={draft.basePrompt}
            onChange={(value) => onDraftChange({ basePrompt: value })}
          />
          <PromptField
            id="asset-consistency-prompt"
            label="一致性提示词"
            value={draft.consistencyPrompt}
            onChange={(value) => onDraftChange({ consistencyPrompt: value })}
          />
          <PromptField
            id="asset-negative-prompt"
            label="负向提示词"
            value={draft.negativePrompt}
            onChange={(value) => onDraftChange({ negativePrompt: value })}
          />
        </div>

        <div className="space-y-2">
          <div className="text-sm font-medium">来源分场</div>
          {asset.sceneLinks?.length ? (
            <div className="grid gap-2">
              {asset.sceneLinks.map((link) => (
                <div key={link.scriptSceneId} className="rounded-md border p-3 text-sm">
                  <div className="font-medium">第 {link.sceneNo} 场 · {link.title}</div>
                  <div className="mt-1 text-xs text-muted-foreground">{link.location || link.usageNote || "已关联剧本场景"}</div>
                </div>
              ))}
            </div>
          ) : (
            <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无场景关联</div>
          )}
        </div>
      </div>

      <div className="space-y-4">
        <div className="overflow-hidden rounded-lg border bg-muted">
          {selectedPreview ? (
            <img alt={asset.name} className="aspect-square w-full object-cover" src={selectedPreview} />
          ) : (
            <div className="grid aspect-square place-items-center">
              <ImageIcon className="h-8 w-8 text-muted-foreground" />
            </div>
          )}
        </div>

        <div className="rounded-lg border p-3">
          <div className="mb-2 text-sm font-medium">上传参考图</div>
          <div className="grid gap-2">
            <Input
              ref={uploadInputRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              onChange={(event) => onFileChange(event.target.files?.[0] ?? null)}
            />
            <div className="text-xs text-muted-foreground">{uploadFile ? uploadFile.name : "未选择文件"}</div>
            <div className="flex gap-2">
              <Button className="flex-1" variant="outline" onClick={() => onUpload(false)} disabled={isUploading || !uploadFile}>
                <Upload className="h-4 w-4" />
                上传
              </Button>
              <Button className="flex-1" onClick={() => onUpload(true)} disabled={isUploading || !uploadFile}>
                <Star className="h-4 w-4" />
                上传并设主图
              </Button>
            </div>
          </div>
        </div>

        <div className="space-y-2">
          <div className="text-sm font-medium">参考图</div>
          {references.length === 0 && <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无参考图</div>}
          <div className="grid gap-2">
            {references.map((reference) => (
              <div key={reference.id} className="grid gap-2 rounded-lg border p-2">
                <div className="flex gap-3">
                  <div className="h-20 w-20 shrink-0 overflow-hidden rounded-md bg-muted">
                    {reference.previewUrl ? (
                      <img alt={reference.title || "参考图"} className="h-full w-full object-cover" src={reference.previewUrl} />
                    ) : (
                      <div className="grid h-full place-items-center">
                        <ImageIcon className="h-5 w-5 text-muted-foreground" />
                      </div>
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge variant={reference.isPrimary ? "default" : "outline"}>{reference.isPrimary ? "主图" : assetReferenceTypeLabel(reference.referenceType)}</Badge>
                      <Badge variant="secondary">{statusLabel(reference.status)}</Badge>
                    </div>
                    <div className="mt-1 truncate text-sm font-medium">{reference.title || "参考图"}</div>
                    {reference.prompt && <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{reference.prompt}</p>}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    className="flex-1"
                    size="sm"
                    variant="outline"
                    onClick={() => onSetPrimary(reference.id)}
                    disabled={reference.isPrimary || isSettingPrimary}
                  >
                    <Star className="h-3.5 w-3.5" />
                    设为主图
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => onDeleteReference(reference.id)}
                    disabled={isDeletingReference}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    解绑
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function PromptField({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Textarea id={id} className="min-h-24" value={value} onChange={(event) => onChange(event.target.value)} />
    </div>
  );
}

function resolveSelectedScript(scripts: Script[], selectedScriptId: string) {
  return scripts.find((script) => script.id === selectedScriptId)
    ?? scripts.find((script) => script.status === "active")
    ?? scripts[0]
    ?? null;
}

function workflowRunType(run: WorkflowRun) {
  const value = run.input?.workflowType;
  return typeof value === "string" ? value : "";
}

function draftFromAsset(asset: CanonicalAsset): AssetDraft {
  return {
    name: asset.name ?? "",
    description: asset.description ?? "",
    basePrompt: asset.basePrompt ?? "",
    consistencyPrompt: asset.consistencyPrompt ?? "",
    negativePrompt: asset.negativePrompt ?? "",
    lockReference: Boolean(asset.lockReference),
  };
}

function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  if (artifact.previewUrl && artifact.mimeType?.startsWith("video/")) {
    return <video className="aspect-video w-full bg-black object-cover" controls src={artifact.previewUrl} />;
  }
  if (artifact.previewUrl) {
    return <img alt={artifactTypeLabel(artifact.type)} className="aspect-video w-full bg-muted object-cover" src={artifact.previewUrl} />;
  }
  return (
    <div className="grid aspect-video place-items-center bg-muted">
      <ImageIcon className="h-6 w-6 text-muted-foreground" />
    </div>
  );
}

function formatArtifactSummary(artifact: Artifact) {
  const mimeType = artifact.mimeType || "媒体文件";
  if (!artifact.createdAt) {
    return mimeType;
  }
  return `${mimeType} · ${formatDateTime(artifact.createdAt)}`;
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未记录时间";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function assetPreviewUrl(asset: CanonicalAsset, references: CanonicalAsset["references"], artifacts: Map<string, string>) {
  const primaryReference = references?.find((reference) => reference.isPrimary && reference.previewUrl) ?? references?.find((reference) => reference.previewUrl);
  return primaryReference?.previewUrl
    ?? idPreview(asset.primaryReferenceArtifactId, artifacts)
    ?? idPreview(asset.referenceArtifactId, artifacts)
    ?? "";
}

function requirementPreviewUrl(requirement: ShotAssetRequirement, artifacts: Map<string, string>) {
  return idPreview(requirement.derivedArtifactId, artifacts)
    ?? idPreview(requirement.asset?.primaryReferenceArtifactId, artifacts)
    ?? idPreview(requirement.asset?.referenceArtifactId, artifacts)
    ?? "";
}

function idPreview(id: string | undefined | null, artifacts: Map<string, string>) {
  return id ? artifacts.get(id) : undefined;
}

function normalizeUploadHeaders(headers: Record<string, string | string[]> | undefined, mimeType: string) {
  const normalized: Record<string, string> = { "Content-Type": mimeType };
  for (const [key, value] of Object.entries(headers ?? {})) {
    normalized[key] = Array.isArray(value) ? value.join(",") : value;
  }
  return normalized;
}
