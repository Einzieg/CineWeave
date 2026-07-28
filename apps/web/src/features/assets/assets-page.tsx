"use client";

import { useQueryClient } from "@tanstack/react-query";
import NextImage from "next/image";
import { useMemo, useRef, useState } from "react";
import { orgScopedKey, useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { localizePlatformError } from "@/lib/error-localization";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Check, ExternalLink, Image as ImageIcon, ListChecks, Loader2, MapPin, Package, RefreshCw, Save, Star, Trash2, Upload, User, Wand2, X } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { artifactTypeLabel, assetReferenceTypeLabel, assetTypeLabel, requirementTypeLabel, statusLabel } from "@/lib/labels";
import { useUiStore } from "@/lib/stores/ui-store";
import { isActiveWorkflowStatus } from "@/lib/workflow-status";
import { useStudioSession } from "@/lib/session";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import type { Artifact, AssetReference, CanonicalAsset, JsonRecord, OutputImpact, ShotAssetRequirement, StudioSession, WorkflowRun } from "@/lib/types";

type AssetTypeFilter = "all" | "character" | "scene" | "prop";
type AssetGenerationFilter = "all" | "generated" | "missing";

type ImagePreview = {
  src: string;
  title: string;
  description?: string;
};

type AssetDraft = {
  name: string;
  description: string;
  basePrompt: string;
  consistencyPrompt: string;
  negativePrompt: string;
  lockReference: boolean;
};

type AssetDraftField = keyof AssetDraft;

type AssetDraftState = {
  assetId: string;
  baseRevision: number;
  base: AssetDraft;
  draft: AssetDraft;
};

type AssetDraftConflict = {
  assetId: string;
  latest: CanonicalAsset;
  conflictingFields: AssetDraftField[];
  changedFields: AssetDraftField[];
};

type SaveAssetDraftPayload = {
  assetId: string;
  base: AssetDraft;
  draft: AssetDraft;
  changedFields?: AssetDraftField[];
  overwrite?: boolean;
};

const assetDraftFields: AssetDraftField[] = ["name", "description", "basePrompt", "consistencyPrompt", "negativePrompt", "lockReference"];
const assetPreviewExpiresSeconds = 900;
const activeAssetListShape = { status: "active" as const };
const activeAssetPreviewShape = {
  status: "active" as const,
  includePreviewUrl: true,
  previewExpiresSeconds: assetPreviewExpiresSeconds,
};

const emptyAssetDraft: AssetDraft = {
  name: "",
  description: "",
  basePrompt: "",
  consistencyPrompt: "",
  negativePrompt: "",
  lockReference: false,
};

function withPendingId(ids: string[], id: string, pending: boolean) {
  if (pending) {
    return ids.includes(id) ? ids : [...ids, id];
  }
  return ids.filter((item) => item !== id);
}

export function AssetsPage({
  projectId,
  initialAssetId = "",
}: {
  projectId: string;
  initialAssetId?: string;
}) {
  const queryClient = useQueryClient();
  const { session } = useStudioSession();
  const [selectedAssetId, setSelectedAssetId] = useState<string | null>(initialAssetId || null);
  const [selectedAssetIds, setSelectedAssetIds] = useState<string[]>([]);
  const [assetTypeFilter, setAssetTypeFilter] = useState<AssetTypeFilter>("all");
  const [generationFilter, setGenerationFilter] = useState<AssetGenerationFilter>("all");
  const [assetDraftState, setAssetDraftState] = useState<AssetDraftState | null>(null);
  const [assetDraftConflict, setAssetDraftConflict] = useState<AssetDraftConflict | null>(null);
  const [assetToArchive, setAssetToArchive] = useState<CanonicalAsset | null>(null);
  const [imagePreview, setImagePreview] = useState<ImagePreview | null>(null);
  const [referenceUploadFile, setReferenceUploadFile] = useState<File | null>(null);
  const [generatingRequirementIds, setGeneratingRequirementIds] = useState<string[]>([]);
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const failedAssetPreviewUrlsRef = useRef(new Set<string>());
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);
  const setActivityOpen = useUiStore((state) => state.setActivityOpen);

  const { data: project } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });

  const { data: assetMetadata = [], isLoading } = useApiQuery({
    key: qk.assets(projectId, activeAssetListShape),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId, activeAssetListShape).then((response) => response.items || []),
  });
  const { data: assetPreviewProjection = [] } = useApiQuery({
    key: qk.assetPreviews(projectId, activeAssetPreviewShape),
    queryFn: (session) => studioApi.listCanonicalAssets(session, projectId, activeAssetPreviewShape).then((response) => response.items || []),
    staleTime: 10 * 60 * 1000,
  });
  const assets = useMemo(() => {
    const previewByAssetId = new Map(assetPreviewProjection.map((asset) => [asset.id, asset]));
    return assetMetadata.map((asset) => {
      const preview = previewByAssetId.get(asset.id);
      return preview?.references ? { ...asset, references: preview.references } : asset;
    });
  }, [assetMetadata, assetPreviewProjection]);
  const { data: requirements = [], isLoading: requirementsLoading } = useApiQuery({
    key: qk.requirements(projectId),
    queryFn: (session) => studioApi.listShotAssetRequirements(session, projectId).then((response) => response.items || []),
  });
  const { data: artifacts = [], isLoading: artifactsLoading } = useApiQuery({
    key: qk.artifacts(projectId),
    queryFn: (session) => studioApi.listArtifacts(session, projectId).then((response) => response.items || []),
  });
  const { data: workflowRuns = [] } = useApiQuery({
    key: qk.workflowRuns(projectId, { status: "active", limit: 100 }),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId, { status: "active", limit: 100 }).then((response) => response.items || []),
    refetchInterval: (query) =>
      pollingFallback && query.state.data?.some((run) => isActiveWorkflowStatus(run.status)) ? 5000 : false,
  });

  const selectedAssetFromList = useMemo(() => assets.find((asset) => asset.id === selectedAssetId) ?? null, [assets, selectedAssetId]);
  const { data: selectedAssetDetail } = useApiQuery({
    key: qk.asset(projectId, selectedAssetId ?? ""),
    queryFn: (activeSession) => studioApi.getCanonicalAsset(activeSession, projectId, selectedAssetId!),
    enabled: !!selectedAssetId,
  });
  const selectedAsset = selectedAssetDetail?.id === selectedAssetId ? selectedAssetDetail : selectedAssetFromList;
  const assetById = useMemo(() => new Map(assets.map((asset) => [asset.id, asset])), [assets]);
  const activeAssetBatchRuns = useMemo(
    () => workflowRuns.filter((run) => isAssetBatchRun(run) && isActiveWorkflowStatus(run.status)),
    [workflowRuns],
  );
  const activePromptAssetIds = useMemo(
    () => assetIdsForBatchRuns(activeAssetBatchRuns.filter((run) => run.workflowType === "batch_generate_asset_cards")),
    [activeAssetBatchRuns],
  );
  const activeImageAssetIds = useMemo(
    () => assetIdsForBatchRuns(activeAssetBatchRuns.filter((run) => run.workflowType === "batch_generate_asset_images")),
    [activeAssetBatchRuns],
  );
  const activeDerivedAssetRun = useMemo(
    () => workflowRuns.some((run) => run.workflowType === "batch_generate_derived_asset_images" && isActiveWorkflowStatus(run.status)),
    [workflowRuns],
  );
  const pendingRequirementIds = useMemo(
    () => requirements.filter((item) => (item.reviewStatus || "pending") === "pending").map((item) => item.id),
    [requirements],
  );
  const needsEditRequirementCount = useMemo(
    () => requirements.filter((item) => item.reviewStatus === "needs_edit").length,
    [requirements],
  );
  const approvedMissingRequirementIds = useMemo(
    () =>
      requirements
        .filter(
          (item) =>
            item.reviewStatus === "approved" &&
            item.status !== "image_running" &&
            item.status !== "skipped" &&
            !item.derivedArtifactId &&
            !item.derivedMediaFileId &&
            !item.derivedStorageKey,
        )
        .map((item) => item.id),
    [requirements],
  );
  const busyAssetIds = useMemo(() => new Set([...activePromptAssetIds, ...activeImageAssetIds]), [activeImageAssetIds, activePromptAssetIds]);
  const filteredAssets = useMemo(
    () =>
      assets.filter((asset) => {
        const typeMatched = assetTypeFilter === "all" || asset.assetType === assetTypeFilter;
        const hasGeneratedImage = assetHasGeneratedImage(asset);
        const generationMatched =
          generationFilter === "all" ||
          (generationFilter === "generated" && hasGeneratedImage) ||
          (generationFilter === "missing" && !hasGeneratedImage);
        return typeMatched && generationMatched;
      }),
    [assetTypeFilter, assets, generationFilter],
  );
  const selectedAssetIdSet = useMemo(() => new Set(selectedAssetIds), [selectedAssetIds]);
  const selectedAvailableAssetIds = useMemo(
    () => selectedAssetIds.filter((assetId) => !busyAssetIds.has(assetId)),
    [busyAssetIds, selectedAssetIds],
  );
  const selectedImageReadyAssetIds = useMemo(
    () => selectedAssetIds.filter((assetId) => {
      const asset = assetById.get(assetId);
      return asset ? assetPromptReady(asset) && asset.status !== "image_running" && asset.status !== "archived" && !busyAssetIds.has(assetId) : false;
    }),
    [assetById, busyAssetIds, selectedAssetIds],
  );
  const assetDraft = useMemo(() => {
    if (!selectedAsset) {
      return emptyAssetDraft;
    }
    if (assetDraftState?.assetId === selectedAsset.id) {
      return assetDraftState.draft;
    }
    return draftFromAsset(selectedAsset);
  }, [assetDraftState, selectedAsset]);
  const activeAssetDraftState = assetDraftState?.assetId === selectedAsset?.id ? assetDraftState : null;
  const changedAssetDraftFields = useMemo(
    () => (activeAssetDraftState ? assetDraftChangedFields(activeAssetDraftState.base, activeAssetDraftState.draft) : []),
    [activeAssetDraftState],
  );
  const activeAssetDraftConflict = assetDraftConflict?.assetId === selectedAsset?.id ? assetDraftConflict : null;
  const { data: selectedReferences = [] } = useApiQuery({
    key: qk.assetReferences(projectId, selectedAsset?.id ?? "", true, assetPreviewExpiresSeconds),
    queryFn: (session) => studioApi.listAssetReferences(session, projectId, selectedAsset!.id, true, assetPreviewExpiresSeconds).then((response) => response.items || []),
    enabled: !!selectedAsset,
  });
  const { data: archiveImpact, isLoading: archiveImpactLoading } = useApiQuery({
    key: qk.assetImpact(projectId, assetToArchive?.id ?? ""),
    queryFn: (session) => studioApi.getCanonicalAssetImpact(session, projectId, assetToArchive!.id),
    enabled: !!assetToArchive,
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

  const cacheCanonicalAsset = (asset: CanonicalAsset) => {
    queryClient.setQueryData<CanonicalAsset>(orgScopedKey(session.organizationId, qk.asset(projectId, asset.id)), (current) =>
      current ? { ...current, ...asset } : asset,
    );
    queryClient.setQueryData<CanonicalAsset[]>(orgScopedKey(session.organizationId, qk.assets(projectId, activeAssetListShape)), (current) =>
      current?.map((item) => (item.id === asset.id ? { ...item, ...asset } : item)),
    );
  };

  const refreshAssetPreviewOnce = (failedUrl: string) => {
    if (failedAssetPreviewUrlsRef.current.has(failedUrl)) {
      return;
    }
    failedAssetPreviewUrlsRef.current.add(failedUrl);
    void queryClient.invalidateQueries({
      queryKey: orgScopedKey(session.organizationId, qk.assetPreviews(projectId, activeAssetPreviewShape)),
      exact: true,
    });
  };

  const generateCardMutation = useApiMutation({
    mutationFn: (session, assetIds: string[]) =>
      studioApi.createAssetBatch(session, projectId, {
        operation: "generate_prompts",
        assetIds,
        maxConcurrency: 5,
        force: true,
        expectedProjectRevision: requireProjectRevision(project?.revision),
      }),
    onSuccess: (run) => {
      toast.success("资产提示词任务已进入队列");
      setActivityOpen(true);
      invalidate([qk.workflowRuns(projectId), qk.workflowNodes(run.id), qk.assetsRoot(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const generateImageMutation = useApiMutation({
    mutationFn: (session, assetIds: string[]) =>
      studioApi.createAssetBatch(session, projectId, {
        operation: "generate_images",
        assetIds,
        maxConcurrency: 5,
        force: true,
        expectedProjectRevision: requireProjectRevision(project?.revision),
      }),
    onSuccess: (run) => {
      toast.success("资产图片任务已进入队列");
      setActivityOpen(true);
      invalidate([qk.workflowRuns(projectId), qk.workflowNodes(run.id), qk.assetsRoot(projectId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const updateAssetMutation = useApiMutation({
    mutationFn: (activeSession, payload: SaveAssetDraftPayload) => saveCanonicalAssetDraft(activeSession, projectId, payload),
    onSuccess: (updated) => {
      cacheCanonicalAsset(updated);
      setAssetDraftState(null);
      setAssetDraftConflict(null);
      toast.success("资产已保存");
      invalidate([qk.assetsRoot(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => {
      if (error instanceof AssetDraftConflictError) {
        cacheCanonicalAsset(error.latest);
        setAssetDraftConflict({
          assetId: error.latest.id,
          latest: error.latest,
          conflictingFields: error.conflictingFields,
          changedFields: error.changedFields,
        });
        toast.warning("资产内容已被其他操作更新，请选择要保留的版本");
        return;
      }
      toast.error("保存失败：" + error.message);
    },
  });

  const archiveAssetMutation = useApiMutation({
    mutationFn: (session, asset: CanonicalAsset) => studioApi.deleteCanonicalAsset(session, projectId, asset.id, asset.revision),
    onSuccess: (_response, asset) => {
      toast.success("资产已归档");
      setAssetToArchive(null);
      if (selectedAssetId === asset.id) {
        setSelectedAssetId(null);
      }
      invalidate([qk.assetsRoot(projectId), qk.requirements(projectId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("归档失败：" + error.message),
  });

  const generatingCardAssetIds = useMemo(
    () => new Set([...activePromptAssetIds, ...(generateCardMutation.isPending ? generateCardMutation.variables ?? [] : [])]),
    [activePromptAssetIds, generateCardMutation.isPending, generateCardMutation.variables],
  );
  const generatingImageAssetIds = useMemo(
    () => new Set([...activeImageAssetIds, ...(generateImageMutation.isPending ? generateImageMutation.variables ?? [] : [])]),
    [activeImageAssetIds, generateImageMutation.isPending, generateImageMutation.variables],
  );

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
      invalidate([qk.assetsRoot(projectId), qk.assetReferencesRoot(projectId, payload.assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("上传失败：" + error.message),
  });

  const setPrimaryReferenceMutation = useApiMutation({
    mutationFn: (session, payload: { assetId: string; referenceId: string }) =>
      studioApi.setPrimaryAssetReference(session, projectId, payload.assetId, payload.referenceId),
    onSuccess: (_response, payload) => {
      toast.success("主图已更新");
      invalidate([qk.assetsRoot(projectId), qk.assetReferencesRoot(projectId, payload.assetId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("设置失败：" + error.message),
  });

  const deleteReferenceMutation = useApiMutation({
    mutationFn: (session, payload: { assetId: string; referenceId: string }) =>
      studioApi.deleteAssetReference(session, projectId, payload.assetId, payload.referenceId),
    onSuccess: (_response, payload) => {
      toast.success("参考图已解绑");
      invalidate([qk.assetsRoot(projectId), qk.assetReferencesRoot(projectId, payload.assetId), qk.artifacts(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("解绑失败：" + error.message),
  });

  const generateRequirementMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.generateDerivedAssetImage(session, projectId, requirementId),
    onSuccess: (result) => {
      toast.success("派生图像任务已创建");
      invalidate([
        qk.workflowRuns(projectId),
        qk.workflowDerivedAssetBatch(result.workflowRun.id),
        qk.requirements(projectId),
        qk.assetsRoot(projectId),
        qk.artifacts(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const generateAssetCard = (assetId: string) => {
    if (generatingCardAssetIds.has(assetId) || generatingImageAssetIds.has(assetId)) {
      return;
    }
    generateCardMutation.mutate([assetId]);
  };

  const generateAssetImage = (asset: CanonicalAsset, draft?: AssetDraft) => {
    if (generatingImageAssetIds.has(asset.id) || generatingCardAssetIds.has(asset.id)) {
      return;
    }
    void (async () => {
      try {
        const draftState = assetDraftState?.assetId === asset.id ? assetDraftState : null;
        const changedFields = draftState ? assetDraftChangedFields(draftState.base, draftState.draft) : [];
        if (draft && changedFields.length > 0) {
          await updateAssetMutation.mutateAsync({ assetId: asset.id, base: draftState!.base, draft, changedFields });
        }
        await generateImageMutation.mutateAsync([asset.id]);
      } catch {
        // Each mutation reports its own actionable error message.
      }
    })();
  };

  const generateRequirementImage = (requirementId: string) => {
    if (generatingRequirementIds.includes(requirementId)) {
      return;
    }
    setGeneratingRequirementIds((current) => withPendingId(current, requirementId, true));
    generateRequirementMutation.mutate(requirementId, {
      onSettled: () => setGeneratingRequirementIds((current) => withPendingId(current, requirementId, false)),
    });
  };

  const reviewRequirementMutation = useApiMutation({
    mutationFn: (session, payload: { requirementId: string; reviewStatus: "approved" | "needs_edit" }) =>
      studioApi.reviewShotAssetRequirement(session, projectId, payload.requirementId, { reviewStatus: payload.reviewStatus }),
    onSuccess: () => {
      invalidate([qk.requirements(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const batchReviewRequirementsMutation = useApiMutation({
    mutationFn: (session, requirementIds: string[]) =>
      studioApi.batchReviewShotAssetRequirements(session, projectId, {
        requirementIds,
        reviewStatus: "approved",
        note: "按结构化规则批量校验镜头资产需求",
      }),
    onSuccess: (result) => {
      if (result.blockedCount > 0) {
        toast.warning(`已确认 ${result.approvedCount} 个需求，${result.blockedCount} 个需修改`);
      } else {
        toast.success(`已确认 ${result.approvedCount} 个镜头资产需求`);
      }
      invalidate([qk.requirements(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("批量审核失败：" + error.message),
  });

  const generateDerivedAssetBatchMutation = useApiMutation({
    mutationFn: (session, requirementIds: string[]) =>
      studioApi.runProductionAction(session, projectId, {
        action: "generate_derived_asset_images",
        options: { requirementIds, maxConcurrency: 5, force: false },
      }),
    onSuccess: (run) => {
      toast.success("镜头衍生资产任务已进入队列");
      setActivityOpen(true);
      invalidate([
        qk.workflowRuns(projectId),
        qk.workflowNodes(run.workflowRunId),
        qk.workflowDerivedAssetBatch(run.workflowRunId),
        qk.requirements(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("创建任务失败：" + error.message),
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

  const toggleAssetSelection = (assetId: string, checked: boolean) => {
    setSelectedAssetIds((current) => {
      if (checked) {
        return current.includes(assetId) ? current : [...current, assetId];
      }
      return current.filter((id) => id !== assetId);
    });
  };

  const selectFilteredAssets = () => {
    setSelectedAssetIds(filteredAssets.map((asset) => asset.id));
  };

  const handleBatchGenerateCards = () => {
    if (selectedAvailableAssetIds.length === 0) {
      toast.error("选中资产正在执行其它生成任务");
      return;
    }
    const skipped = selectedAssetIds.length - selectedAvailableAssetIds.length;
    if (skipped > 0) {
      toast.info(`已跳过 ${skipped} 个任务中的资产`);
    }
    generateCardMutation.mutate([...selectedAvailableAssetIds]);
  };

  const handleBatchGenerateImages = () => {
    if (selectedImageReadyAssetIds.length === 0) {
      toast.error("请先为选中资产生成提示词卡片");
      return;
    }
    const skipped = selectedAssetIds.length - selectedImageReadyAssetIds.length;
    if (skipped > 0) {
      toast.info(`已跳过 ${skipped} 个未就绪资产`);
    }
    generateImageMutation.mutate([...selectedImageReadyAssetIds]);
  };

  const saveSelectedAsset = () => {
    if (!selectedAsset || !activeAssetDraftState || changedAssetDraftFields.length === 0) {
      return;
    }
    updateAssetMutation.mutate({
      assetId: selectedAsset.id,
      base: activeAssetDraftState.base,
      draft: activeAssetDraftState.draft,
      changedFields: changedAssetDraftFields,
    });
  };

  const useLatestConflictingAsset = () => {
    if (!activeAssetDraftConflict) {
      return;
    }
    cacheCanonicalAsset(activeAssetDraftConflict.latest);
    setAssetDraftState(null);
    setAssetDraftConflict(null);
    toast.success("已采用最新资产内容");
  };

  const overwriteConflictingAssetFields = () => {
    if (!activeAssetDraftConflict || !activeAssetDraftState) {
      return;
    }
    updateAssetMutation.mutate({
      assetId: activeAssetDraftConflict.assetId,
      base: draftFromAsset(activeAssetDraftConflict.latest),
      draft: activeAssetDraftState.draft,
      changedFields: activeAssetDraftConflict.changedFields,
      overwrite: true,
    });
  };

  const closeAssetDialog = () => {
    setSelectedAssetId(null);
    setAssetDraftState(null);
    setAssetDraftConflict(null);
    setReferenceUploadFile(null);
    if (uploadInputRef.current) {
      uploadInputRef.current.value = "";
    }
  };

  return (
    <Surface>
      <SectionTitle title="资产管理" description="筛选、批量生成并维护角色、场景、道具资产" />

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
          <div className="grid gap-3 rounded-lg border bg-background p-4">
            <div className="grid gap-3 lg:grid-cols-[minmax(180px,240px)_minmax(180px,240px)_1fr]">
              <div className="grid gap-2">
                <Label>资产分类</Label>
                <Select value={assetTypeFilter} onValueChange={(value) => setAssetTypeFilter(value as AssetTypeFilter)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部分类</SelectItem>
                    <SelectItem value="character">角色</SelectItem>
                    <SelectItem value="scene">场景</SelectItem>
                    <SelectItem value="prop">道具</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-2">
                <Label>生成状态</Label>
                <Select value={generationFilter} onValueChange={(value) => setGenerationFilter(value as AssetGenerationFilter)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部状态</SelectItem>
                    <SelectItem value="generated">已生成</SelectItem>
                    <SelectItem value="missing">未生成</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-wrap items-end justify-start gap-2 lg:justify-end">
                <Button variant="outline" onClick={selectFilteredAssets} disabled={filteredAssets.length === 0}>
                  <Check className="h-4 w-4" />
                  全选当前筛选
                </Button>
                <Button variant="outline" onClick={() => setSelectedAssetIds([])} disabled={selectedAssetIds.length === 0}>
                  <X className="h-4 w-4" />
                  清空选择
                </Button>
                <Button
                  variant="outline"
                  onClick={handleBatchGenerateCards}
                  disabled={selectedAvailableAssetIds.length === 0 || !project?.revision || generateCardMutation.isPending}
                >
                  {generateCardMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Wand2 className="h-4 w-4" />}
                  {generateCardMutation.isPending ? "正在创建任务" : "生成提示词"}
                </Button>
                <Button
                  onClick={handleBatchGenerateImages}
                  disabled={selectedImageReadyAssetIds.length === 0 || !project?.revision || generateImageMutation.isPending}
                >
                  {generateImageMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <ImageIcon className="h-4 w-4" />}
                  {generateImageMutation.isPending ? "正在创建任务" : "生成资产图片"}
                </Button>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Badge variant="secondary">{assetTypeFilterLabel(assetTypeFilter)}</Badge>
              <Badge variant="secondary">{assetGenerationFilterLabel(generationFilter)}</Badge>
              <span>显示 {filteredAssets.length} 个资产</span>
              <span>已选择 {selectedAssetIds.length} 个</span>
              <span>可生成图片 {selectedImageReadyAssetIds.length} 个</span>
            </div>
          </div>

          {isLoading && <Skeleton className="h-64" />}

          {!isLoading && assets.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <ImageIcon className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无资产</p>
            </div>
          )}

          {!isLoading && assets.length > 0 && filteredAssets.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <ImageIcon className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">当前筛选无资产</p>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {filteredAssets.map((asset) => {
              const Icon = getAssetIcon(asset.assetType);
              const previewReferences = selectedAsset?.id === asset.id && selectedReferences.length > 0 ? selectedReferences : asset.references;
              const previewUrl = assetPreviewUrl(asset, previewReferences, artifactPreviewById);
              const isSelectedForBatch = selectedAssetIdSet.has(asset.id);
              const isPromptReady = assetPromptReady(asset);
              const isGeneratingThisCard = generatingCardAssetIds.has(asset.id);
              const isGeneratingThisImage = generatingImageAssetIds.has(asset.id);
              return (
                <div
                  key={asset.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => setSelectedAssetId(asset.id)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      setSelectedAssetId(asset.id);
                    }
                  }}
                  className={cn(
                    "group relative overflow-hidden rounded-lg border bg-card p-3 text-left transition hover:shadow-md",
                    isSelectedForBatch && "ring-2 ring-primary",
                  )}
                >
                  <span className="absolute left-3 top-3 z-10 rounded-md bg-background/90 p-1 shadow-sm" onClick={(event) => event.stopPropagation()}>
                    <Checkbox
                      checked={isSelectedForBatch}
                      onCheckedChange={(checked) => toggleAssetSelection(asset.id, checked === true)}
                      aria-label={`选择${asset.name}`}
                    />
                  </span>
                  <div className="mb-2 aspect-video overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <PreviewImageButton
                        alt={asset.name}
                        className="relative h-full w-full"
                        description={asset.description}
                        imageClassName="h-full w-full object-cover"
                        onLoadError={refreshAssetPreviewOnce}
                        onOpenImage={(preview) => setImagePreview(preview)}
                        src={previewUrl}
                        title={asset.name}
                      />
                    ) : (
                      <div className="flex h-full items-center justify-center">
                        <Icon className="h-6 w-6 text-muted-foreground" />
                      </div>
                    )}
                  </div>

                  <div className="space-y-1.5">
                    <div className="flex items-start justify-between gap-2">
                      <h3 className="line-clamp-1 text-sm font-medium leading-tight">{asset.name}</h3>
                      <Badge variant="outline">{assetTypeLabel(asset.assetType)}</Badge>
                    </div>
                    {asset.description && <p className="line-clamp-1 text-xs text-muted-foreground">{asset.description}</p>}
                    <div className="flex flex-wrap gap-1.5">
                      <Badge variant={assetHasGeneratedImage(asset) ? "default" : "secondary"}>
                        {assetHasGeneratedImage(asset) ? "已生成" : "未生成"}
                      </Badge>
                      <Badge variant={isPromptReady ? "outline" : "secondary"}>{assetPromptStatusLabel(asset)}</Badge>
                    </div>
                    <div className="flex gap-1.5 pt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateAssetCard(asset.id);
                        }}
                        disabled={isGeneratingThisCard || isGeneratingThisImage}
                      >
                        {isGeneratingThisCard ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <Wand2 className="mr-1 h-3 w-3" />}
                        {isGeneratingThisCard ? "生成提示词中" : isPromptReady ? "重新生成提示词" : "生成提示词"}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={(event) => {
                          event.stopPropagation();
                          generateAssetImage(asset);
                        }}
                        disabled={isGeneratingThisImage || isGeneratingThisCard || !isPromptReady}
                      >
                        {isGeneratingThisImage ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <ImageIcon className="mr-1 h-3 w-3" />}
                        {isGeneratingThisImage
                          ? "生成图片中"
                          : !isPromptReady
                            ? "请先生成提示词"
                            : assetHasGeneratedImage(asset)
                              ? "重新生成图片"
                              : "生成图片"}
                      </Button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="requirements" className="space-y-3">
          {requirementsLoading && <Skeleton className="h-48" />}
          {!requirementsLoading && requirements.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无镜头资产需求</p>
            </div>
          )}
          {!requirementsLoading && requirements.length > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-background p-3">
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <Badge variant="secondary">待确认 {pendingRequirementIds.length}</Badge>
                <Badge variant={needsEditRequirementCount > 0 ? "destructive" : "secondary"}>需修改 {needsEditRequirementCount}</Badge>
                <Badge variant="outline">可生成 {approvedMissingRequirementIds.length}</Badge>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => batchReviewRequirementsMutation.mutate(pendingRequirementIds)}
                  disabled={pendingRequirementIds.length === 0 || batchReviewRequirementsMutation.isPending}
                >
                  {batchReviewRequirementsMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <ListChecks className="h-4 w-4" />}
                  {batchReviewRequirementsMutation.isPending ? "正在校验" : "批量校验并确认"}
                </Button>
                <Button
                  onClick={() => generateDerivedAssetBatchMutation.mutate(approvedMissingRequirementIds)}
                  disabled={approvedMissingRequirementIds.length === 0 || activeDerivedAssetRun || generateDerivedAssetBatchMutation.isPending}
                >
                  {activeDerivedAssetRun || generateDerivedAssetBatchMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <ImageIcon className="h-4 w-4" />}
                  {activeDerivedAssetRun || generateDerivedAssetBatchMutation.isPending ? "生成中" : "生成已确认衍生图"}
                </Button>
              </div>
            </div>
          )}
          <div className="grid gap-3">
            {requirements.map((requirement) => {
              const previewUrl = requirementPreviewUrl(requirement, artifactPreviewById);
              const isGeneratingThisRequirement = generatingRequirementIds.includes(requirement.id);
              return (
                <div key={requirement.id} className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[160px_1fr]">
                  <div className="aspect-video overflow-hidden rounded-md bg-muted">
                    {previewUrl ? (
                      <PreviewImageButton
                        alt={requirement.assetName || requirement.assetId}
                        className="relative h-full w-full"
                        description={requirement.prompt || requirement.action || requirement.roleInShot || undefined}
                        imageClassName="h-full w-full object-cover"
                        onOpenImage={(preview) => setImagePreview(preview)}
                        src={previewUrl}
                        title={requirement.assetName || requirement.assetId}
                      />
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
                      <Button size="sm" onClick={() => generateRequirementImage(requirement.id)} disabled={isGeneratingThisRequirement || requirement.reviewStatus !== "approved"}>
                        {isGeneratingThisRequirement ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="mr-1 h-3.5 w-3.5" />}
                        {isGeneratingThisRequirement ? "生成中" : requirement.reviewStatus === "approved" ? "生成图像" : "先确认"}
                      </Button>
                    </div>
                    <RequirementProvenance requirement={requirement} />
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
          <ArtifactGrid artifacts={artifacts} onOpenImage={(preview) => setImagePreview(preview)} />
        </TabsContent>
      </Tabs>

      <Dialog open={!!selectedAsset} onOpenChange={(open) => !open && closeAssetDialog()}>
        <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-6xl">
          <DialogHeader>
            <DialogTitle>{selectedAsset?.name ?? "资产详情"}</DialogTitle>
          </DialogHeader>
          {selectedAsset ? (
            <AssetDetailPanel
              asset={selectedAsset}
              draft={assetDraft}
              references={selectedReferences}
              uploadFile={referenceUploadFile}
              uploadInputRef={uploadInputRef}
              onDraftChange={(patch) => {
                setAssetDraftConflict(null);
                setAssetDraftState((current) => {
                  if (current?.assetId === selectedAsset.id) {
                    return { ...current, draft: { ...current.draft, ...patch } };
                  }
                  const base = draftFromAsset(selectedAsset);
                  return {
                    assetId: selectedAsset.id,
                    baseRevision: selectedAsset.revision,
                    base,
                    draft: { ...base, ...patch },
                  };
                });
              }}
              onFileChange={setReferenceUploadFile}
              onSave={saveSelectedAsset}
              onGenerateCard={() => generateAssetCard(selectedAsset.id)}
              onGenerateImage={() => generateAssetImage(selectedAsset, assetDraft)}
              canGenerateImage={assetDraftPromptReady(assetDraft) && selectedAsset.status !== "archived" && selectedAsset.status !== "image_running"}
              hasUnsavedChanges={changedAssetDraftFields.length > 0}
              hasRemoteUpdate={Boolean(activeAssetDraftState && selectedAsset.revision !== activeAssetDraftState.baseRevision)}
              conflict={activeAssetDraftConflict}
              onUseLatestConflict={useLatestConflictingAsset}
              onOverwriteConflict={overwriteConflictingAssetFields}
              onArchive={() => setAssetToArchive(selectedAsset)}
              onUpload={(setPrimary) => {
                if (!referenceUploadFile) {
                  toast.error("请选择参考图文件");
                  return;
                }
                uploadReferenceMutation.mutate({ assetId: selectedAsset.id, file: referenceUploadFile, setPrimary });
              }}
              onSetPrimary={(referenceId) => setPrimaryReferenceMutation.mutate({ assetId: selectedAsset.id, referenceId })}
              onDeleteReference={(referenceId) => deleteReferenceMutation.mutate({ assetId: selectedAsset.id, referenceId })}
              onOpenImage={(preview) => setImagePreview(preview)}
              isSaving={updateAssetMutation.isPending}
              isGeneratingCard={generatingCardAssetIds.has(selectedAsset.id)}
              isGeneratingImage={generatingImageAssetIds.has(selectedAsset.id)}
              isArchiving={archiveAssetMutation.isPending}
              isUploading={uploadReferenceMutation.isPending}
              isSettingPrimary={setPrimaryReferenceMutation.isPending}
              isDeletingReference={deleteReferenceMutation.isPending}
            />
          ) : null}
          <ArchiveAssetDialog
            asset={assetToArchive}
            impact={archiveImpact}
            isLoading={archiveImpactLoading}
            isPending={archiveAssetMutation.isPending}
            onOpenChange={(open) => !open && setAssetToArchive(null)}
            onConfirm={() => assetToArchive && archiveAssetMutation.mutate(assetToArchive)}
          />
          <ImagePreviewDialog preview={imagePreview} onClose={() => setImagePreview(null)} />
        </DialogContent>
      </Dialog>

      {!selectedAsset ? <ImagePreviewDialog preview={imagePreview} onClose={() => setImagePreview(null)} /> : null}
    </Surface>
  );
}

function ArtifactGrid({ artifacts, onOpenImage }: { artifacts: Artifact[]; onOpenImage: (preview: ImagePreview) => void }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {artifacts.map((artifact) => (
        <div key={artifact.id} className="overflow-hidden rounded-lg border">
          <ArtifactPreview artifact={artifact} onOpenImage={onOpenImage} />
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

function ArchiveAssetDialog({
  asset,
  impact,
  isLoading,
  isPending,
  onOpenChange,
  onConfirm,
}: {
  asset: CanonicalAsset | null;
  impact?: OutputImpact;
  isLoading: boolean;
  isPending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog open={!!asset} onOpenChange={onOpenChange}>
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>归档资产</AlertDialogTitle>
          <AlertDialogDescription>
            {asset ? `归档「${asset.name}」后，它会从默认核心资产列表隐藏。` : "归档资产"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-3 text-sm">
          {isLoading ? (
            <Skeleton className="h-20" />
          ) : (
            <>
              {impact?.affected?.length ? (
                <div className="grid gap-2">
                  {impact.affected.map((item) => (
                    <div key={item.entityType} className="flex items-center justify-between rounded-md border px-3 py-2">
                      <span>{impactEntityLabel(item.entityType)}</span>
                      <Badge variant="secondary">{item.count}</Badge>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-md border border-dashed p-3 text-muted-foreground">暂无关联产物</div>
              )}
              {impact?.warnings?.length ? (
                <div className="space-y-1 text-xs text-muted-foreground">
                  {impact.warnings.map((warning) => (
                    <p key={warning}>{warning}</p>
                  ))}
                </div>
              ) : null}
            </>
          )}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isPending}>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm} disabled={isPending || isLoading}>
            归档资产
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function RequirementProvenance({ requirement }: { requirement: ShotAssetRequirement }) {
  const metadata = requirement.metadata ?? {};
  const rows = [
    ["供应商调用", stringMetadata(metadata.providerCallId)],
    ["模型", stringMetadata(metadata.modelId)],
    ["模板", stringMetadata(metadata.promptTemplateKey)],
    ["模板版本", stringMetadata(metadata.promptVersionId)],
    ["提示词哈希", stringMetadata(metadata.promptHash)],
  ].filter(([, value]) => value);
  if (rows.length === 0 && !requirement.prompt) {
    return null;
  }
  return (
    <details className="rounded-md border bg-muted/30 px-3 py-2 text-xs">
      <summary className="cursor-pointer text-muted-foreground">技术信息</summary>
      {requirement.prompt ? <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap text-foreground">{requirement.prompt}</pre> : null}
      {rows.length ? (
        <div className="mt-2 grid gap-1 text-muted-foreground">
          {rows.map(([label, value]) => (
            <div key={label} className="grid gap-1 sm:grid-cols-[96px_1fr]">
              <span>{label}</span>
              <span className="break-all text-foreground">{value}</span>
            </div>
          ))}
        </div>
      ) : null}
    </details>
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
  canGenerateImage,
  hasUnsavedChanges,
  hasRemoteUpdate,
  conflict,
  onUseLatestConflict,
  onOverwriteConflict,
  onArchive,
  onUpload,
  onSetPrimary,
  onDeleteReference,
  onOpenImage,
  isSaving,
  isGeneratingCard,
  isGeneratingImage,
  isArchiving,
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
  canGenerateImage: boolean;
  hasUnsavedChanges: boolean;
  hasRemoteUpdate: boolean;
  conflict: AssetDraftConflict | null;
  onUseLatestConflict: () => void;
  onOverwriteConflict: () => void;
  onArchive: () => void;
  onUpload: (setPrimary: boolean) => void;
  onSetPrimary: (referenceId: string) => void;
  onDeleteReference: (referenceId: string) => void;
  onOpenImage: (preview: ImagePreview) => void;
  isSaving: boolean;
  isGeneratingCard: boolean;
  isGeneratingImage: boolean;
  isArchiving: boolean;
  isUploading: boolean;
  isSettingPrimary: boolean;
  isDeletingReference: boolean;
}) {
  const selectedPreview = assetPreviewUrl(asset, references, new Map());
  const canSave = draft.name.trim() !== "" && draft.description.trim() !== "";
  const imageFailureReason = asset.status === "image_failed" ? stringMetadata(asset.metadata?.imageFailedReason) : "";

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
            <Button variant="outline" onClick={onGenerateCard} disabled={isGeneratingCard || isGeneratingImage}>
              {isGeneratingCard ? <Loader2 className="h-4 w-4 animate-spin" /> : <Wand2 className="h-4 w-4" />}
              {isGeneratingCard ? "生成提示词中" : assetDraftPromptReady(draft) ? "重新生成提示词" : "生成提示词"}
            </Button>
            <Button variant="outline" onClick={onGenerateImage} disabled={isSaving || isGeneratingImage || isGeneratingCard || !canGenerateImage}>
              {isGeneratingImage ? <Loader2 className="h-4 w-4 animate-spin" /> : <ImageIcon className="h-4 w-4" />}
              {isGeneratingImage ? "生成图片中" : assetHasGeneratedImage(asset) ? "重新生成图片" : "生成图片"}
            </Button>
            <Button onClick={onSave} disabled={isSaving || isGeneratingCard || isGeneratingImage || !canSave || !hasUnsavedChanges}>
              {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              {isSaving ? "保存中" : "保存资产"}
            </Button>
            <Button variant="destructive" onClick={onArchive} disabled={isArchiving}>
              <Trash2 className="h-4 w-4" />
              归档资产
            </Button>
          </div>
        </div>

        {conflict ? (
          <div role="alert" className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
            <p className="font-medium text-amber-800">资产内容在编辑期间发生了变化</p>
            <p className="mt-1 text-amber-700">冲突字段：{conflict.conflictingFields.map(assetDraftFieldLabel).join("、")}</p>
            <div className="mt-3 flex flex-wrap gap-2">
              <Button type="button" size="sm" variant="outline" onClick={onUseLatestConflict} disabled={isSaving}>
                采用最新内容
              </Button>
              <Button type="button" size="sm" onClick={onOverwriteConflict} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                保留我的修改
              </Button>
            </div>
          </div>
        ) : hasRemoteUpdate ? (
          <div className="rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-700">
            资产已在后台更新，保存时会自动合并你的修改。
          </div>
        ) : null}

        {!assetDraftPromptReady(draft) ? (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-sm text-amber-700">
            基础提示词和一致性提示词均需填写后才能生成图片。
          </div>
        ) : null}

        {imageFailureReason ? (
          <div role="alert" className="rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            图像生成失败：{localizePlatformError(imageFailureReason)}
          </div>
        ) : null}

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
            <PreviewImageButton
              alt={asset.name}
              className="relative aspect-square w-full"
              description={asset.description}
              imageClassName="aspect-square w-full object-cover"
              onOpenImage={onOpenImage}
              src={selectedPreview}
              title={asset.name}
            />
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

        <GeneratedImageHistory references={references} onOpenImage={onOpenImage} />

        <div className="space-y-2">
          <div className="text-sm font-medium">参考图</div>
          {references.length === 0 && <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无参考图</div>}
          <div className="grid gap-2">
            {references.map((reference) => (
              <div key={reference.id} className="grid gap-2 rounded-lg border p-2">
                <div className="flex gap-3">
                  <div className="h-20 w-20 shrink-0 overflow-hidden rounded-md bg-muted">
                    {reference.previewUrl ? (
                      <PreviewImageButton
                        alt={reference.title || "参考图"}
                        className="relative h-full w-full"
                        description={reference.prompt || undefined}
                        imageClassName="h-full w-full object-cover"
                        onOpenImage={onOpenImage}
                        src={reference.previewUrl}
                        title={reference.title || "参考图"}
                      />
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

function GeneratedImageHistory({ references, onOpenImage }: { references: AssetReference[]; onOpenImage: (preview: ImagePreview) => void }) {
  const generatedReferences = references
    .filter((reference) => reference.referenceType === "generated" || Boolean(reference.prompt) || Boolean(reference.metadata?.providerCallId))
    .sort((left, right) => new Date(right.createdAt ?? 0).getTime() - new Date(left.createdAt ?? 0).getTime());

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <div className="text-sm font-medium">历史生图版本</div>
        <Badge variant="secondary">{generatedReferences.length}</Badge>
      </div>
      {generatedReferences.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无生图版本</div>
      ) : (
        <div className="grid max-h-96 gap-2 overflow-y-auto pr-1">
          {generatedReferences.map((reference, index) => (
            <div key={reference.id} className="grid gap-2 rounded-lg border p-2">
              <div className="flex gap-3">
                <div className="h-20 w-20 shrink-0 overflow-hidden rounded-md bg-muted">
                  {reference.previewUrl ? (
                    <PreviewImageButton
                      alt={reference.title || "生图版本"}
                      className="relative h-full w-full"
                      description={reference.prompt || undefined}
                      imageClassName="h-full w-full object-cover"
                      onOpenImage={onOpenImage}
                      src={reference.previewUrl}
                      title={reference.title || "生图版本"}
                    />
                  ) : (
                    <div className="grid h-full place-items-center">
                      <ImageIcon className="h-5 w-5 text-muted-foreground" />
                    </div>
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge variant={reference.isPrimary ? "default" : "outline"}>{reference.isPrimary ? "当前主图" : `版本 ${generatedReferences.length - index}`}</Badge>
                    <Badge variant="secondary">{statusLabel(reference.status)}</Badge>
                  </div>
                  <div className="mt-1 truncate text-sm font-medium">{reference.title || "生成图"}</div>
                  <div className="mt-1 text-xs text-muted-foreground">{reference.createdAt ? formatDateTime(reference.createdAt) : "未记录时间"}</div>
                </div>
              </div>
              {reference.prompt ? (
                <details className="rounded-md bg-muted/40 px-3 py-2 text-xs">
                  <summary className="cursor-pointer text-muted-foreground">生图提示词</summary>
                  <pre className="mt-2 max-h-36 overflow-auto whitespace-pre-wrap text-foreground">{reference.prompt}</pre>
                </details>
              ) : null}
              <ReferenceProvenance reference={reference} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ReferenceProvenance({ reference }: { reference: AssetReference }) {
  const metadata = reference.metadata ?? {};
  const rows = [
    ["供应商调用", stringMetadata(metadata.providerCallId)],
    ["模型", stringMetadata(metadata.modelId)],
    ["模板", stringMetadata(metadata.promptTemplateKey)],
    ["模板版本", stringMetadata(reference.promptVersionId ?? metadata.promptVersionId)],
    ["提示词哈希", stringMetadata(reference.promptHash ?? metadata.promptHash)],
  ].filter(([, value]) => value);

  if (rows.length === 0) {
    return null;
  }

  return (
    <details className="rounded-md bg-muted/40 px-3 py-2 text-xs">
      <summary className="cursor-pointer text-muted-foreground">生成溯源</summary>
      <div className="mt-2 grid gap-1 text-muted-foreground">
        {rows.map(([label, value]) => (
          <div key={label} className="grid gap-1 sm:grid-cols-[80px_1fr]">
            <span>{label}</span>
            <span className="break-all text-foreground">{value}</span>
          </div>
        ))}
      </div>
    </details>
  );
}

function impactEntityLabel(value: string) {
  switch (value) {
    case "scene_asset_link":
      return "场景关联";
    case "asset_reference":
      return "参考图";
    case "shot_asset_requirement":
      return "镜头资产需求";
    case "generated_media":
      return "已生成媒体";
    case "novel_chapter":
      return "分集";
    case "novel_event":
      return "事件";
    case "adaptation_plan":
      return "改编计划";
    case "script":
      return "剧本";
    default:
      return value;
  }
}

function stringMetadata(value: unknown) {
  if (typeof value === "string") {
    return value.trim();
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return "";
}

function PromptField({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Textarea id={id} className="min-h-24" value={value} onChange={(event) => onChange(event.target.value)} />
    </div>
  );
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

function assetHasGeneratedImage(asset: CanonicalAsset) {
  return Boolean(
    asset.primaryReferenceArtifactId ||
      asset.primaryReferenceMediaFileId ||
      asset.primaryReferenceStorageKey ||
      asset.referenceArtifactId ||
      asset.referenceMediaFileId ||
      asset.referenceStorageKey ||
      asset.status === "image_succeeded",
  );
}

function assetPromptReady(asset: CanonicalAsset) {
  return Boolean(asset.basePrompt?.trim() && asset.consistencyPrompt?.trim() && asset.status !== "archived" && asset.status !== "image_running");
}

function assetDraftPromptReady(draft: AssetDraft) {
  return Boolean(draft.basePrompt.trim() && draft.consistencyPrompt.trim());
}

class AssetDraftConflictError extends Error {
  constructor(
    readonly latest: CanonicalAsset,
    readonly conflictingFields: AssetDraftField[],
    readonly changedFields: AssetDraftField[],
  ) {
    super("资产内容已被其他操作更新");
    this.name = "AssetDraftConflictError";
  }
}

async function saveCanonicalAssetDraft(
  session: StudioSession,
  projectId: string,
  payload: SaveAssetDraftPayload,
): Promise<CanonicalAsset> {
  const requestedFields = new Set(payload.changedFields ?? assetDraftChangedFields(payload.base, payload.draft));
  const changedFields = assetDraftFields.filter((field) => requestedFields.has(field));
  let latest = await studioApi.getCanonicalAsset(session, projectId, payload.assetId);
  if (changedFields.length === 0) {
    return latest;
  }

  for (let attempt = 0; attempt < 3; attempt += 1) {
    if (attempt > 0) {
      latest = await studioApi.getCanonicalAsset(session, projectId, payload.assetId);
    }
    const latestDraft = draftFromAsset(latest);
    if (!payload.overwrite) {
      const conflictingFields = changedFields.filter(
        (field) => !assetDraftValueEqual(field, payload.base[field], latestDraft[field]) && !assetDraftValueEqual(field, payload.draft[field], latestDraft[field]),
      );
      if (conflictingFields.length > 0) {
        throw new AssetDraftConflictError(latest, conflictingFields, changedFields);
      }
    }

    const fieldsToPatch = changedFields.filter((field) => !assetDraftValueEqual(field, payload.draft[field], latestDraft[field]));
    if (fieldsToPatch.length === 0) {
      return latest;
    }
    const patch: JsonRecord = { expectedRevision: latest.revision };
    for (const field of fieldsToPatch) {
      patch[field] = field === "lockReference" ? Boolean(payload.draft[field]) : String(payload.draft[field]).trim();
    }
    try {
      return await studioApi.updateCanonicalAsset(session, projectId, payload.assetId, patch);
    } catch (error) {
      if (error instanceof StudioApiError && error.code === "ASSET_REVISION_CONFLICT" && attempt < 2) {
        continue;
      }
      throw error;
    }
  }
  return latest;
}

function assetDraftChangedFields(base: AssetDraft, draft: AssetDraft): AssetDraftField[] {
  return assetDraftFields.filter((field) => !assetDraftValueEqual(field, base[field], draft[field]));
}

function assetDraftValueEqual(field: AssetDraftField, left: AssetDraft[AssetDraftField], right: AssetDraft[AssetDraftField]) {
  if (field === "lockReference") {
    return Boolean(left) === Boolean(right);
  }
  return String(left).trim() === String(right).trim();
}

function assetDraftFieldLabel(field: AssetDraftField) {
  switch (field) {
    case "name":
      return "名称";
    case "description":
      return "描述";
    case "basePrompt":
      return "基础提示词";
    case "consistencyPrompt":
      return "一致性提示词";
    case "negativePrompt":
      return "负面提示词";
    case "lockReference":
      return "参考图锁定";
  }
}

function assetPromptStatusLabel(asset: CanonicalAsset) {
  if (!asset.basePrompt?.trim() || !asset.consistencyPrompt?.trim()) {
    return "卡片未就绪";
  }
  if (asset.status === "draft" || asset.status === "prompt_ready") {
    return "提示词就绪";
  }
  return statusLabel(asset.status);
}

function assetTypeFilterLabel(value: AssetTypeFilter) {
  switch (value) {
    case "character":
      return "角色";
    case "scene":
      return "场景";
    case "prop":
      return "道具";
    default:
      return "全部分类";
  }
}

function assetGenerationFilterLabel(value: AssetGenerationFilter) {
  switch (value) {
    case "generated":
      return "已生成";
    case "missing":
      return "未生成";
    default:
      return "全部状态";
  }
}

function PreviewImageButton({
  src,
  alt,
  title,
  description,
  className,
  imageClassName,
  onLoadError,
  onOpenImage,
}: {
  src: string;
  alt: string;
  title: string;
  description?: string;
  className?: string;
  imageClassName?: string;
  onLoadError?: (src: string) => void;
  onOpenImage: (preview: ImagePreview) => void;
}) {
  return (
    <button
      type="button"
      className={cn("relative block overflow-hidden text-left", className)}
      onClick={(event) => {
        event.stopPropagation();
        onOpenImage({ src, title, description });
      }}
    >
      <NextImage
        alt={alt}
        fill
        className={cn("transition duration-150 hover:scale-[1.02]", imageClassName)}
        onError={() => onLoadError?.(src)}
        sizes="(max-width: 768px) 100vw, 420px"
        src={src}
        unoptimized
      />
    </button>
  );
}

function ImagePreviewDialog({ preview, onClose }: { preview: ImagePreview | null; onClose: () => void }) {
  return (
    <Dialog open={Boolean(preview)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[calc(100vh-2rem)] overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="px-4 pt-4">
          <DialogTitle>{preview?.title ?? "图片预览"}</DialogTitle>
        </DialogHeader>
        {preview ? (
          <div className="relative h-[calc(100vh-8rem)] min-h-64 bg-muted">
            <NextImage alt={preview.title} className="object-contain" fill sizes="100vw" src={preview.src} unoptimized />
          </div>
        ) : null}
        {preview?.description ? <div className="border-t px-4 py-3 text-sm text-muted-foreground">{preview.description}</div> : null}
      </DialogContent>
    </Dialog>
  );
}

function ArtifactPreview({ artifact, onOpenImage }: { artifact: Artifact; onOpenImage: (preview: ImagePreview) => void }) {
  if (artifact.previewUrl && artifact.mimeType?.startsWith("video/")) {
    return <video className="aspect-video w-full bg-black object-cover" controls src={artifact.previewUrl} />;
  }
  if (artifact.previewUrl) {
    return (
      <PreviewImageButton
        alt={artifactTypeLabel(artifact.type)}
        className="relative aspect-video w-full"
        description={formatArtifactSummary(artifact)}
        imageClassName="aspect-video w-full bg-muted object-cover"
        onOpenImage={onOpenImage}
        src={artifact.previewUrl}
        title={artifactTypeLabel(artifact.type)}
      />
    );
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
  const primaryReference = references?.find((reference) => reference.isPrimary);
  const primaryPreview = referencePreviewUrl(primaryReference, artifacts);
  if (primaryPreview) {
    return primaryPreview;
  }
  for (const reference of references ?? []) {
    const preview = referencePreviewUrl(reference, artifacts);
    if (preview) {
      return preview;
    }
  }
  return idPreview(asset.primaryReferenceArtifactId, artifacts)
    ?? idPreview(asset.referenceArtifactId, artifacts)
    ?? "";
}

function referencePreviewUrl(reference: AssetReference | undefined, artifacts: Map<string, string>) {
  return reference?.previewUrl ?? idPreview(reference?.artifactId, artifacts);
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

function requireProjectRevision(revision?: number) {
  if (!revision || revision < 1) {
    throw new Error("项目状态尚未加载，请稍后重试");
  }
  return revision;
}

function isAssetBatchRun(run: WorkflowRun) {
  return run.workflowType === "batch_generate_asset_cards" || run.workflowType === "batch_generate_asset_images";
}

function assetIdsForBatchRuns(runs: WorkflowRun[]) {
  const assetIds = new Set<string>();
  for (const run of runs) {
    const input = asRecord(run.input);
    const items = Array.isArray(input.items) ? input.items : [];
    for (const rawItem of items) {
      const item = asRecord(rawItem);
      if (typeof item.assetId === "string" && item.assetId) {
        assetIds.add(item.assetId);
      }
    }
  }
  return [...assetIds];
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}
