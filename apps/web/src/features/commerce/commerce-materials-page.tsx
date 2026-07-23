"use client";

import NextImage from "next/image";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Archive,
  Check,
  Clock3,
  Copy,
  FileText,
  Globe2,
  GripVertical,
  History,
  Image as ImageIcon,
  Languages,
  Loader2,
  Maximize2,
  Package,
  Plus,
  RotateCcw,
  Save,
  Sparkles,
  Star,
} from "lucide-react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { localeLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { cn } from "@/lib/utils";
import type {
  CommerceLanguageMode,
  CommerceLanguageResolution,
  CommerceProduct,
  CommerceProductRebuildImpact,
  CommerceProductReference,
  CommerceProjectLanguageOption,
  CommerceScriptUnit,
  CommerceScriptUnitRebuildImpact,
  CommerceSetupState,
  Project,
} from "@/lib/types";

type ProductReferenceRole = "primary" | "front" | "back" | "detail" | "usage" | "logo" | "other";
type LanguageMode = CommerceLanguageMode;
type SaveState = "saved" | "dirty" | "saving" | "error";

type CommerceScriptProductionSummary = {
  currentStage?: string;
  failedCount?: number;
  finalVideoStatus?: string;
};

type ProductDraft = {
  name: string;
  brand: string;
  sellingPoints: string;
  immutableFeatures: string;
  prohibitedClaims: string;
  notes: string;
};

type ScriptDraft = {
  title: string;
  content: string;
  languageMode: LanguageMode;
  explicitTargetLanguage: string;
  targetDurationSeconds: number;
  targetPlatform: string;
};


const referenceRoles: Array<{ value: ProductReferenceRole; label: string }> = [
  { value: "front", label: "包装正面" },
  { value: "back", label: "包装背面" },
  { value: "detail", label: "产品细节" },
  { value: "usage", label: "使用场景" },
  { value: "logo", label: "品牌标识" },
  { value: "other", label: "其他" },
];

const targetPlatforms = [
  { value: "douyin", label: "抖音" },
  { value: "kuaishou", label: "快手" },
  { value: "xiaohongshu", label: "小红书" },
  { value: "wechat_channels", label: "视频号" },
  { value: "tiktok", label: "TikTok" },
  { value: "youtube_shorts", label: "YouTube Shorts" },
  { value: "other", label: "其他平台" },
];

const emptyProductDraft: ProductDraft = {
  name: "",
  brand: "",
  sellingPoints: "",
  immutableFeatures: "",
  prohibitedClaims: "",
  notes: "",
};

export function CommerceMaterialsPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const uploadInputRef = useRef<HTMLInputElement | null>(null);
  const [productDraft, setProductDraft] = useState<ProductDraft>(emptyProductDraft);
  const [productDraftVersionId, setProductDraftVersionId] = useState<string | null>(null);
  const [scriptCursor, setScriptCursor] = useState("");
  const [scriptPages, setScriptPages] = useState<CommerceScriptUnit[][]>([]);
  const [selectedScriptIds, setSelectedScriptIds] = useState<string[]>([]);
  const [selectedScriptId, setSelectedScriptId] = useState<string | null>(null);
  const [createScriptOpen, setCreateScriptOpen] = useState(false);
  const [languageVariantOpen, setLanguageVariantOpen] = useState(false);
  const [variantLanguage, setVariantLanguage] = useState("");
  const [referenceToArchive, setReferenceToArchive] = useState<CommerceProductReference | null>(null);
  const [productRebuildImpact, setProductRebuildImpact] = useState<CommerceProductRebuildImpact | null>(null);
  const [productRebuildDialogOpen, setProductRebuildDialogOpen] = useState(false);
  const [scriptIdsToArchive, setScriptIdsToArchive] = useState<string[]>([]);
  const [imagePreview, setImagePreview] = useState<CommerceProductReference | null>(null);
  const [draggedReferenceId, setDraggedReferenceId] = useState<string | null>(null);
  const [draggedScriptId, setDraggedScriptId] = useState<string | null>(null);
	const [confirmedSetupLocale, setConfirmedSetupLocale] = useState("");

	const projectQuery = useApiQuery({
		key: qk.project(projectId),
		queryFn: (session) => studioApi.getProject(session, projectId),
	});
	const setupSessionId = projectQuery.data?.setupSessionId ?? "";
	const setupQuery = useApiQuery({
		key: qk.commerceSetupSession(projectId, setupSessionId),
		queryFn: (session) => studioApi.getCommerceSetupSession(session, projectId, setupSessionId),
		enabled: Boolean(setupSessionId),
		refetchInterval: (query) => isActiveCommerceSetupState(query.state.data?.state) ? 3000 : false,
	});
	const setupSession = setupQuery.data;
	const setupLanguageQuery = useApiQuery({
		key: qk.commerceLanguageResolution(projectId, setupSession?.scriptUnitId ?? ""),
		queryFn: (session) => studioApi.getCommerceScriptLanguageResolution(session, projectId, setupSession?.scriptUnitId ?? ""),
		enabled: setupSession?.state === "waiting_user_confirmation" && Boolean(setupSession.scriptUnitId),
	});
	const setupOptionsQuery = useApiQuery({
		key: qk.commerceProjectOptions(projectQuery.data?.workspaceId ?? ""),
		queryFn: (session) => studioApi.getCommerceProjectOptions(session, projectQuery.data?.workspaceId ?? ""),
		enabled: Boolean(projectQuery.data?.workspaceId),
	});
	const languageOptions = useMemo(
		() => executableLanguageOptions(
			setupOptionsQuery.data?.languages ?? [],
			projectQuery.data?.audioStrategy,
			projectQuery.data?.audioRequirement,
		),
		[projectQuery.data?.audioRequirement, projectQuery.data?.audioStrategy, setupOptionsQuery.data?.languages],
	);
	const durationOptions = setupOptionsQuery.data?.durations?.length ? setupOptionsQuery.data.durations : [15, 30, 60];
	const createScriptDefaults = scriptDraftFromProjectDefaults(projectQuery.data, languageOptions, durationOptions);

	const selectedSetupLocale = confirmedSetupLocale || setupLanguageQuery.data?.targetLanguage || "";

  const productQuery = useApiQuery({
    key: qk.commerceProduct(projectId),
    queryFn: (session) => studioApi.getCommerceProduct(session, projectId),
  });
  const referenceQuery = useApiQuery({
    key: qk.commerceProductReferences(projectId),
    queryFn: (session) => studioApi.listCommerceProductReferences(session, projectId).then((response) => response.items),
  });
  const scriptPageQuery = useApiQuery({
    key: qk.commerceScriptUnits(projectId, "active", scriptCursor),
    queryFn: (session) => studioApi.listCommerceScriptUnits(session, projectId, { status: "active", cursor: scriptCursor, limit: 24 }),
  });

  const product = productQuery.data;
  const currentProductVersionId = product?.currentVersion?.id ?? null;
  const visibleProductDraft = productDraftVersionId === currentProductVersionId
    ? productDraft
    : product
      ? productDraftFromProduct(product)
      : emptyProductDraft;
  const references = useMemo(
    () => [...(referenceQuery.data ?? [])].sort((left, right) => left.ordinal - right.ordinal),
    [referenceQuery.data],
  );
  const scripts = useMemo(
    () => uniqueById([...scriptPages.flat(), ...(scriptPageQuery.data?.items ?? [])]),
    [scriptPageQuery.data?.items, scriptPages],
  );
  const primaryReference = references.find((reference) => reference.isPrimary) ?? references[0];
  const selectedScript = scripts.find((script) => selectedScriptIds.includes(script.id));

  function updateProductDraft(patch: Partial<ProductDraft>) {
    setProductDraft({ ...visibleProductDraft, ...patch });
    setProductDraftVersionId(currentProductVersionId);
  }

  const saveProductMutation = useApiMutation({
    mutationFn: async (session, draft: ProductDraft) => {
      const result = await studioApi.createCommerceProductVersion(session, projectId, {
        expectedRevision: product?.revision ?? 0,
        name: draft.name.trim(),
        brand: draft.brand.trim(),
        sellingPoints: lines(draft.sellingPoints),
        immutableFeatures: { packaging: lines(draft.immutableFeatures) },
        prohibitedClaims: lines(draft.prohibitedClaims),
        metadata: { notes: draft.notes.trim() },
      });
      if (!result.requiresRebuild) return { result, impact: null as CommerceProductRebuildImpact | null };
      const impact = await studioApi.getCommerceProductRebuildImpact(session, projectId, {
        targetProductVersionId: result.version.id,
        targetReferenceIds: references.map((reference) => reference.id),
        expectedProductRevision: result.product.revision,
      });
      return { result, impact };
    },
    onSuccess: ({ result, impact }, draft) => {
      setProductDraft(draft);
      setProductDraftVersionId(result.product.currentVersionId ?? result.version.id);
      if (result.requiresRebuild) {
        setProductRebuildImpact(impact);
        setProductRebuildDialogOpen(true);
      } else {
        toast.success("商品信息已保存");
      }
      invalidate([qk.commerceProduct(projectId), qk.commerceProductVersions(projectId)]);
    },
    onError: (error) => toast.error(`保存失败：${error.message}`),
  });

  const rebuildProductMutation = useApiMutation({
    mutationFn: (session, impact: CommerceProductRebuildImpact) => studioApi.createCommerceProductRebuild(
      session,
      projectId,
      { impactToken: impact.impactToken, expectedProductRevision: impact.expectedProductRevision },
      crypto.randomUUID(),
    ),
    onSuccess: (result) => {
      setProductRebuildImpact(null);
      setProductRebuildDialogOpen(false);
      setProductDraftVersionId(null);
      toast.success(`商品换版完成，已为 ${result.affectedUnitCount} 个脚本建立新生产代`);
      invalidate([
        qk.commerceProduct(projectId),
        qk.commerceProductVersions(projectId),
        qk.commerceProductReferences(projectId),
        commerceScriptUnitsRoot(projectId),
      ]);
    },
    onError: (error) => toast.error(`换版失败：${error.message}`),
  });

  const uploadReferenceMutation = useApiMutation({
    mutationFn: async (session, file: File) => {
      const ticket = await studioApi.createCommerceProductReferenceUpload(
        session,
        projectId,
        { fileName: file.name, mimeType: file.type, expiresSeconds: 900 },
        crypto.randomUUID(),
      );
      await studioApi.uploadCommerceProductReferenceFile(ticket, file);
      return studioApi.completeCommerceProductReferenceUpload(session, projectId, {
        uploadId: ticket.uploadId,
        referenceRole: references.length === 0 ? "primary" : "other",
        setPrimary: references.length === 0,
      });
    },
    onSuccess: () => {
      toast.success("商品图片已上传");
      invalidate([qk.commerceProductReferences(projectId), qk.commerceProduct(projectId)]);
    },
    onError: (error) => toast.error(`上传失败：${error.message}`),
  });

  const updateReferenceMutation = useApiMutation({
    mutationFn: (
      session,
      payload: { reference: CommerceProductReference; referenceRole?: ProductReferenceRole; ordinal?: number; setPrimary?: boolean },
    ) => studioApi.updateCommerceProductReference(session, projectId, payload.reference.id, {
      expectedRevision: payload.reference.revision,
      referenceRole: payload.referenceRole,
      ordinal: payload.ordinal,
      setPrimary: payload.setPrimary,
    }),
    onSuccess: () => invalidate([qk.commerceProductReferences(projectId), qk.commerceProduct(projectId)]),
    onError: (error) => toast.error(`更新图片失败：${error.message}`),
  });

  const archiveReferenceMutation = useApiMutation({
    mutationFn: (session, reference: CommerceProductReference) =>
      studioApi.archiveCommerceProductReference(session, projectId, reference.id, reference.revision),
    onSuccess: () => {
      setReferenceToArchive(null);
      toast.success("商品图片已归档");
      invalidate([qk.commerceProductReferences(projectId), qk.commerceProduct(projectId)]);
    },
    onError: (error) => toast.error(`归档失败：${error.message}`),
  });

  const createScriptMutation = useApiMutation({
    mutationFn: (session, draft: ScriptDraft) =>
      studioApi.createCommerceScriptUnit(
        session,
        projectId,
        scriptCreateBody(scriptPageQuery.data?.scriptUnitsRevision ?? product?.scriptUnitsRevision ?? 0, draft),
      ),
    onSuccess: (result) => {
      const created = unwrapScriptUnit(result);
      setCreateScriptOpen(false);
      toast.success("脚本已新增");
      refreshScriptUnits();
      if (created) setSelectedScriptId(created.id);
    },
    onError: (error) => toast.error(`新增失败：${error.message}`),
  });

  const duplicateScriptMutation = useApiMutation({
    mutationFn: (session, script: CommerceScriptUnit) =>
      studioApi.duplicateCommerceScriptUnit(session, projectId, script.id, scriptPageQuery.data?.scriptUnitsRevision ?? product?.scriptUnitsRevision ?? 0),
    onSuccess: () => {
      toast.success("脚本副本已创建");
      setSelectedScriptIds([]);
      refreshScriptUnits();
    },
    onError: (error) => toast.error(`复制失败：${error.message}`),
  });

  const languageVariantMutation = useApiMutation({
    mutationFn: (session, payload: { script: CommerceScriptUnit; language: string }) =>
      studioApi.createCommerceScriptLanguageVariant(
        session,
        projectId,
        payload.script.id,
        {
          expectedScriptUnitsRevision: scriptPageQuery.data?.scriptUnitsRevision ?? product?.scriptUnitsRevision ?? 0,
          targetLanguage: payload.language,
        },
      ),
    onSuccess: () => {
      setLanguageVariantOpen(false);
      setSelectedScriptIds([]);
      toast.success("语言版本已创建");
      refreshScriptUnits();
    },
    onError: (error) => toast.error(`创建失败：${error.message}`),
  });

  const archiveScriptsMutation = useApiMutation({
    mutationFn: async (session, scriptIds: string[]) => {
      const targets = scripts.filter((script) => scriptIds.includes(script.id));
      const results = await Promise.allSettled(
        targets.map((script) => studioApi.archiveCommerceScriptUnit(session, projectId, script.id, script.revision)),
      );
      const failed = results.filter((result) => result.status === "rejected");
      if (failed.length > 0) throw new Error(`${failed.length} 个脚本未能归档，请刷新后重试`);
      return targets.length;
    },
    onSuccess: (count) => {
      setScriptIdsToArchive([]);
      setSelectedScriptIds([]);
      toast.success(`已归档 ${count} 个脚本`);
      refreshScriptUnits();
    },
    onError: (error) => toast.error(`归档失败：${error.message}`),
  });

  const reorderScriptsMutation = useApiMutation({
    mutationFn: (session, ordered: CommerceScriptUnit[]) =>
      studioApi.reorderCommerceScriptUnits(
        session,
        projectId,
        {
          expectedScriptUnitsRevision: scriptPageQuery.data?.scriptUnitsRevision ?? product?.scriptUnitsRevision ?? 0,
          items: ordered.map((script, index) => ({ scriptUnitId: script.id, sortOrder: index + 1 })),
        },
      ),
    onSuccess: () => {
      toast.success("脚本顺序已更新");
      refreshScriptUnits();
    },
    onError: (error) => toast.error(`排序失败：${error.message}`),
  });

	const confirmSetupLanguageMutation = useApiMutation({
		mutationFn: (session, locale: string) => {
			if (!setupSession || !setupLanguageQuery.data) throw new Error("语言确认状态尚未准备完成");
			return studioApi.confirmCommerceSetupLanguage(session, projectId, setupSession.id, {
				expectedRevision: setupSession.revision,
				resolutionId: setupLanguageQuery.data.id,
				targetLanguage: locale,
			});
		},
		onSuccess: () => {
			toast.success("视频语言已确认，项目准备流程继续运行");
			invalidate([
				qk.commerceSetupSession(projectId, setupSessionId),
				qk.project(projectId),
				qk.commerceLanguageResolution(projectId, setupSession?.scriptUnitId ?? ""),
			]);
		},
		onError: (error) => toast.error(`确认失败：${error.message}`),
	});

	const retrySetupMutation = useApiMutation({
		mutationFn: (session) => {
			if (!setupSession || setupSession.state !== "failed") throw new Error("当前项目准备任务不能重试");
			return studioApi.completeCommerceSetupSession(
				session,
				projectId,
				setupSession.id,
				{ expectedRevision: setupSession.revision },
				crypto.randomUUID(),
			);
		},
		onSuccess: () => {
			toast.success("已重新提交项目准备任务");
			invalidate([qk.commerceSetupSession(projectId, setupSessionId), qk.project(projectId)]);
		},
		onError: (error) => toast.error(`重试失败：${error.message}`),
	});

  function refreshScriptUnits() {
    setScriptPages([]);
    setScriptCursor("");
    invalidate([commerceScriptUnitsRoot(projectId), qk.commerceProduct(projectId)]);
  }

  function loadMoreScripts() {
    const page = scriptPageQuery.data;
    if (!page?.hasMore || !page.nextCursor) return;
    setScriptPages((current) => [...current, page.items]);
    setScriptCursor(page.nextCursor);
  }

  function handleReferenceDrop(target: CommerceProductReference) {
    const dragged = references.find((reference) => reference.id === draggedReferenceId);
    setDraggedReferenceId(null);
    if (!dragged || dragged.id === target.id) return;
    updateReferenceMutation.mutate({ reference: dragged, ordinal: target.ordinal });
  }

  function handleScriptDrop(target: CommerceScriptUnit) {
    const sourceIndex = scripts.findIndex((script) => script.id === draggedScriptId);
    const targetIndex = scripts.findIndex((script) => script.id === target.id);
    setDraggedScriptId(null);
    if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex || scriptPageQuery.data?.hasMore) return;
    const ordered = [...scripts];
    const [moved] = ordered.splice(sourceIndex, 1);
    ordered.splice(targetIndex, 0, moved);
    reorderScriptsMutation.mutate(ordered);
  }

  const allScriptsSelected = scripts.length > 0 && scripts.every((script) => selectedScriptIds.includes(script.id));

  return (
    <div className="space-y-5 pb-10">
		{setupSession && setupSession.state !== "completed" ? (
			<Surface>
				<div className="flex flex-col gap-3 p-4 md:flex-row md:items-center md:justify-between">
					<div className="flex min-w-0 items-center gap-3">
						{isActiveCommerceSetupState(setupSession.state) && setupSession.state !== "waiting_user_confirmation" ? (
							<Loader2 className="size-5 shrink-0 animate-spin text-primary" />
						) : (
							<Clock3 className="size-5 shrink-0 text-primary" />
						)}
						<div className="min-w-0">
							<div className="font-medium">{commerceSetupStateLabel(setupSession.state)}</div>
							{setupSession.lastErrorMessage ? <div className="mt-1 text-sm text-destructive">{setupSession.lastErrorMessage}</div> : null}
						</div>
					</div>
					{setupSession.state === "waiting_user_confirmation" ? (
						<div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
							<Select value={selectedSetupLocale} onValueChange={setConfirmedSetupLocale}>
								<SelectTrigger className="w-full sm:w-52"><SelectValue placeholder="选择视频语言" /></SelectTrigger>
								<SelectContent>
									{languageOptions.map((language) => (
										<SelectItem key={language.locale} value={language.locale}>{language.label}</SelectItem>
									))}
								</SelectContent>
							</Select>
							<Button
								disabled={!selectedSetupLocale || confirmSetupLanguageMutation.isPending}
								onClick={() => confirmSetupLanguageMutation.mutate(selectedSetupLocale)}
							>
								{confirmSetupLanguageMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
								确认语言
							</Button>
						</div>
					) : setupSession.state === "failed" ? (
						<Button disabled={retrySetupMutation.isPending} onClick={() => retrySetupMutation.mutate(undefined)}>
							{retrySetupMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <RotateCcw className="size-4" />}
							重试项目准备
						</Button>
					) : null}
				</div>
			</Surface>
		) : null}
      <Surface>
        <SectionTitle title="商品资料" description="商品事实和参考图将被所有脚本单元共同引用" />
        <div className="grid gap-5 p-4 xl:grid-cols-[minmax(300px,0.9fr)_minmax(520px,1.5fr)]">
          <div className="min-w-0">
            <div className="relative aspect-[4/3] overflow-hidden rounded-lg border bg-muted/40">
              {primaryReference?.previewUrl ? (
                <button className="group relative block size-full" type="button" onClick={() => setImagePreview(primaryReference)}>
                  <NextImage alt={product?.currentVersion?.name || "商品主图"} className="object-contain p-3 transition-transform duration-200 group-hover:scale-[1.02]" fill sizes="(max-width: 1280px) 100vw, 36vw" src={primaryReference.previewUrl} unoptimized />
                  <span className="absolute right-2 top-2 rounded-md bg-background/90 p-1.5 opacity-0 shadow-sm transition-opacity group-hover:opacity-100">
                    <Maximize2 className="size-4" />
                  </span>
                </button>
              ) : (
                <div className="flex size-full flex-col items-center justify-center gap-2 text-muted-foreground">
                  <Package className="size-10" />
                  <span className="text-sm">尚未上传商品图片</span>
                </div>
              )}
            </div>

            <div className="mt-3 flex items-center gap-2 overflow-x-auto pb-1">
              {references.map((reference) => (
                <button
                  className={cn(
                    "group relative size-16 shrink-0 overflow-hidden rounded-md border bg-muted/40",
                    reference.isPrimary && "border-primary ring-1 ring-primary/30",
                  )}
                  draggable
                  key={reference.id}
                  onClick={() => setImagePreview(reference)}
                  onDragEnd={() => setDraggedReferenceId(null)}
                  onDragOver={(event) => event.preventDefault()}
                  onDragStart={() => setDraggedReferenceId(reference.id)}
                  onDrop={(event) => {
                    event.preventDefault();
                    handleReferenceDrop(reference);
                  }}
                  title="拖动排序，点击查看大图"
                  type="button"
                >
                  {reference.previewUrl ? <NextImage alt={referenceRoleLabel(reference)} className="object-cover" fill sizes="64px" src={reference.previewUrl} unoptimized /> : <ImageIcon className="m-auto size-5 text-muted-foreground" />}
                  {reference.isPrimary ? <Star className="absolute left-1 top-1 size-3.5 fill-amber-400 text-amber-500" /> : null}
                </button>
              ))}
              <button
                className="flex size-16 shrink-0 flex-col items-center justify-center gap-1 rounded-md border border-dashed text-xs text-muted-foreground transition-colors hover:border-primary hover:text-primary"
                disabled={uploadReferenceMutation.isPending}
                onClick={() => uploadInputRef.current?.click()}
                type="button"
              >
                {uploadReferenceMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
                上传
              </button>
              <input
                accept="image/jpeg,image/png,image/webp"
                className="hidden"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (file) uploadReferenceMutation.mutate(file);
                  event.currentTarget.value = "";
                }}
                ref={uploadInputRef}
                type="file"
              />
            </div>

            {references.length > 0 ? (
              <div className="mt-3 divide-y rounded-lg border">
                {references.map((reference) => (
                  <div className="flex items-center gap-2 px-2 py-2" key={reference.id}>
                    <GripVertical className="size-4 shrink-0 text-muted-foreground" />
                    <Select
                      disabled={updateReferenceMutation.isPending || reference.isPrimary}
                      onValueChange={(value) => updateReferenceMutation.mutate({ reference, referenceRole: value as ProductReferenceRole })}
                      value={reference.isPrimary ? "primary" : reference.referenceRole}
                    >
                      <SelectTrigger className="h-7 min-w-0 flex-1"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {reference.isPrimary ? <SelectItem value="primary">主图</SelectItem> : null}
                        {referenceRoles.map((role) => <SelectItem key={role.value} value={role.value}>{role.label}</SelectItem>)}
                      </SelectContent>
                    </Select>
                    <span className="hidden whitespace-nowrap text-xs text-muted-foreground sm:inline">{reference.width}×{reference.height}</span>
                    {!reference.isPrimary ? (
                      <Button onClick={() => updateReferenceMutation.mutate({ reference, setPrimary: true })} size="icon-xs" title="设为主图" type="button" variant="ghost"><Star /></Button>
                    ) : null}
                    <Button onClick={() => setReferenceToArchive(reference)} size="icon-xs" title="归档图片" type="button" variant="ghost"><Archive /></Button>
                  </div>
                ))}
              </div>
            ) : null}
          </div>

          {productQuery.isLoading ? (
            <div className="space-y-4"><Skeleton className="h-16" /><Skeleton className="h-28" /><Skeleton className="h-28" /></div>
          ) : productQuery.error ? (
            <QueryError message={productQuery.error.message} onRetry={() => void productQuery.refetch()} />
          ) : (
            <div className="grid content-start gap-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="产品名称"><Input onChange={(event) => updateProductDraft({ name: event.target.value })} value={visibleProductDraft.name} /></Field>
                <Field label="品牌"><Input onChange={(event) => updateProductDraft({ brand: event.target.value })} value={visibleProductDraft.brand} /></Field>
              </div>
              <div className="grid gap-4 lg:grid-cols-2">
                <Field label="核心卖点"><Textarea className="min-h-28 resize-y" onChange={(event) => updateProductDraft({ sellingPoints: event.target.value })} placeholder="每行一个卖点" value={visibleProductDraft.sellingPoints} /></Field>
                <Field label="禁止改变的包装特征"><Textarea className="min-h-28 resize-y" onChange={(event) => updateProductDraft({ immutableFeatures: event.target.value })} placeholder="每行一个必须保持的外观特征" value={visibleProductDraft.immutableFeatures} /></Field>
                <Field label="禁用声明"><Textarea className="min-h-24 resize-y" onChange={(event) => updateProductDraft({ prohibitedClaims: event.target.value })} placeholder="每行一条不能使用的宣传说法" value={visibleProductDraft.prohibitedClaims} /></Field>
                <Field label="备注"><Textarea className="min-h-24 resize-y" onChange={(event) => updateProductDraft({ notes: event.target.value })} value={visibleProductDraft.notes} /></Field>
              </div>
              <div className="flex items-center justify-between border-t pt-3">
                <div className="text-xs text-muted-foreground">
                  {product?.currentVersion ? `当前版本 ${product.currentVersion.version} · ${formatDateTime(product.currentVersion.createdAt)}` : "尚未建立商品版本"}
                </div>
                <div className="flex items-center gap-2">
                  {productRebuildImpact ? <Button onClick={() => setProductRebuildDialogOpen(true)} type="button" variant="outline">确认换版</Button> : null}
                  <Button disabled={!visibleProductDraft.name.trim() || saveProductMutation.isPending || Boolean(productRebuildImpact)} onClick={() => saveProductMutation.mutate(visibleProductDraft)} type="button">
                    {saveProductMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}保存商品
                  </Button>
                </div>
              </div>
            </div>
          )}
        </div>
      </Surface>

      <Surface>
        <div className="flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 className="text-sm font-semibold">广告脚本</h2>
            <p className="mt-1 text-sm text-muted-foreground">每个脚本单元独立生成一条成片</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={() => setCreateScriptOpen(true)} type="button"><Plus />新增脚本</Button>
            <Button disabled={!selectedScript || selectedScriptIds.length !== 1 || duplicateScriptMutation.isPending} onClick={() => selectedScript && duplicateScriptMutation.mutate(selectedScript)} type="button" variant="outline">
              {duplicateScriptMutation.isPending ? <Loader2 className="animate-spin" /> : <Copy />}复制
            </Button>
            <Button disabled={!selectedScript || selectedScriptIds.length !== 1 || languageOptions.length === 0} onClick={() => { setVariantLanguage(languageOptions[0]?.locale ?? ""); setLanguageVariantOpen(true); }} type="button" variant="outline"><Languages />创建语言版本</Button>
            <Button disabled={selectedScriptIds.length === 0} onClick={() => setScriptIdsToArchive(selectedScriptIds)} type="button" variant="outline"><Archive />归档</Button>
          </div>
        </div>

        <div className="p-4">
          <div className="mb-3 flex items-center gap-3 border-b pb-3 text-sm">
            <Checkbox checked={allScriptsSelected} onCheckedChange={(checked) => setSelectedScriptIds(checked === true ? scripts.map((script) => script.id) : [])} />
            <span className="text-muted-foreground">{selectedScriptIds.length > 0 ? `已选择 ${selectedScriptIds.length} 个` : `共加载 ${scripts.length} 个脚本`}</span>
          </div>

          {scriptPageQuery.isLoading && scripts.length === 0 ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 6 }).map((_, index) => <Skeleton className="h-40" key={index} />)}</div>
          ) : scriptPageQuery.error && scripts.length === 0 ? (
            <QueryError message={scriptPageQuery.error.message} onRetry={() => void scriptPageQuery.refetch()} />
          ) : scripts.length === 0 ? (
            <div className="flex min-h-48 flex-col items-center justify-center gap-3 text-center">
              <FileText className="size-9 text-muted-foreground" />
              <div><p className="font-medium">还没有广告脚本</p><p className="mt-1 text-sm text-muted-foreground">新增第一个脚本单元开始制作</p></div>
              <Button onClick={() => setCreateScriptOpen(true)} type="button"><Plus />新增脚本</Button>
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {scripts.map((script) => {
                const selected = selectedScriptIds.includes(script.id);
                const summary = scriptProductionSummary(script);
                return (
                  <article
                    className={cn(
                      "group relative cursor-pointer rounded-lg border bg-card p-3 transition-colors hover:border-primary/50 hover:bg-muted/20",
                      selected && "border-primary bg-primary/[0.03]",
                    )}
                    draggable={!scriptPageQuery.data?.hasMore}
                    key={script.id}
                    onClick={() => setSelectedScriptId(script.id)}
                    onDragEnd={() => setDraggedScriptId(null)}
                    onDragOver={(event) => {
                      if (!scriptPageQuery.data?.hasMore) event.preventDefault();
                    }}
                    onDragStart={() => setDraggedScriptId(script.id)}
                    onDrop={(event) => {
                      event.preventDefault();
                      handleScriptDrop(script);
                    }}
                    style={{ contentVisibility: "auto", containIntrinsicSize: "180px" }}
                  >
                    <div className="flex items-start gap-2">
                      <Checkbox
                        checked={selected}
                        onCheckedChange={(checked) => setSelectedScriptIds((ids) => checked === true ? [...new Set([...ids, script.id])] : ids.filter((id) => id !== script.id))}
                        onClick={(event) => event.stopPropagation()}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-xs font-medium text-primary">脚本 {String(script.unitNo).padStart(2, "0")}</span>
                          <GripVertical className="size-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                        </div>
                        <h3 className="mt-1 line-clamp-1 font-semibold">{script.title}</h3>
                      </div>
                    </div>
                    <p className="mt-3 line-clamp-3 min-h-15 whitespace-pre-wrap text-sm leading-5 text-muted-foreground">{scriptPreview(script)}</p>
                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      <Badge variant="outline"><Globe2 />{localeLabel(script.explicitTargetLanguage || script.languageResolution?.targetLanguage || (script.languageMode === "auto" ? "auto" : ""))}</Badge>
                      <Badge variant="outline"><Clock3 />{script.targetDurationSeconds} 秒</Badge>
                      <Badge variant={summary?.failedCount ? "destructive" : "secondary"}>{scriptStageLabel(summary?.currentStage, script)}</Badge>
                      {summary?.failedCount ? <Badge variant="destructive">失败 {summary.failedCount}</Badge> : null}
                    </div>
                    <div className="mt-3 flex items-center justify-between border-t pt-2 text-xs text-muted-foreground">
                      <span>版本 {script.currentSourceVersion?.version ?? (script.currentSourceVersionId ? "已建立" : "草稿")}</span>
                      <span>{formatDateTime(script.updatedAt)}</span>
                    </div>
                  </article>
                );
              })}
            </div>
          )}

          {scriptPageQuery.data?.hasMore ? (
            <div className="mt-4 flex justify-center">
              <Button disabled={scriptPageQuery.isFetching} onClick={loadMoreScripts} type="button" variant="outline">
                {scriptPageQuery.isFetching ? <Loader2 className="animate-spin" /> : null}加载更多
              </Button>
            </div>
          ) : null}
        </div>
      </Surface>

      {createScriptOpen ? (
        <CreateScriptDialog
          initialDraft={createScriptDefaults}
          languages={languageOptions}
          loading={createScriptMutation.isPending}
          onOpenChange={setCreateScriptOpen}
          onSubmit={(draft) => createScriptMutation.mutate(draft)}
          open
          durations={durationOptions}
        />
      ) : null}

      <Dialog onOpenChange={setLanguageVariantOpen} open={languageVariantOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>创建语言版本</DialogTitle><DialogDescription>复制当前脚本并为新单元设置独立目标语言。</DialogDescription></DialogHeader>
          <Field label="目标语言">
            <Select onValueChange={setVariantLanguage} value={variantLanguage}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{languageOptions.map((locale) => <SelectItem key={locale.locale} value={locale.locale}>{locale.label}</SelectItem>)}</SelectContent></Select>
          </Field>
          <DialogFooter>
            <Button onClick={() => setLanguageVariantOpen(false)} type="button" variant="outline">取消</Button>
            <Button disabled={!selectedScript || languageVariantMutation.isPending} onClick={() => selectedScript && languageVariantMutation.mutate({ script: selectedScript, language: variantLanguage })} type="button">
              {languageVariantMutation.isPending ? <Loader2 className="animate-spin" /> : <Languages />}创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ScriptDetailDialog
        key={selectedScriptId ?? "closed"}
        onChanged={refreshScriptUnits}
        onOpenChange={(open) => !open && setSelectedScriptId(null)}
        open={Boolean(selectedScriptId)}
        projectId={projectId}
        scriptUnitId={selectedScriptId}
        languages={languageOptions}
        durations={durationOptions}
      />

      <Dialog onOpenChange={(open) => !open && setImagePreview(null)} open={Boolean(imagePreview)}>
        <DialogContent className="bg-black/95 p-2 ring-white/10 sm:max-w-5xl">
          <DialogTitle className="sr-only">查看商品图片</DialogTitle>
          {imagePreview?.previewUrl ? (
            <div className="relative h-[78vh] w-full"><NextImage alt={referenceRoleLabel(imagePreview)} className="object-contain" fill sizes="90vw" src={imagePreview.previewUrl} unoptimized /></div>
          ) : null}
          {imagePreview ? (
            <div className="absolute bottom-3 left-3 flex items-center gap-2 rounded-md bg-black/70 px-3 py-2 text-xs text-white">
              <span>{referenceRoleLabel(imagePreview)}</span><span>{imagePreview.width}×{imagePreview.height}</span><span>{qualityLabel(imagePreview.qualityReview)}</span>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <AlertDialog onOpenChange={(open) => !open && setReferenceToArchive(null)} open={Boolean(referenceToArchive)}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>归档商品图片</AlertDialogTitle><AlertDialogDescription>图片将从当前商品图库隐藏，已冻结的历史生产记录不受影响。</AlertDialogDescription></AlertDialogHeader>
          <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={archiveReferenceMutation.isPending} onClick={() => referenceToArchive && archiveReferenceMutation.mutate(referenceToArchive)} variant="destructive">归档</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog onOpenChange={setProductRebuildDialogOpen} open={productRebuildDialogOpen && Boolean(productRebuildImpact)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认商品换版</AlertDialogTitle>
            <AlertDialogDescription>
              新商品版本将用于 {productRebuildImpact?.affectedUnits.length ?? 0} 个活动脚本；系统会保留旧生产代，并为受影响脚本建立新的待制作生产代。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {productRebuildImpact?.blockers.length ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
              {productRebuildImpact.blockers.map((blocker) => <p key={blocker}>{blocker}</p>)}
            </div>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>稍后处理</AlertDialogCancel>
            <AlertDialogAction
              disabled={!productRebuildImpact || productRebuildImpact.blockers.length > 0 || rebuildProductMutation.isPending}
              onClick={() => productRebuildImpact && rebuildProductMutation.mutate(productRebuildImpact)}
            >
              {rebuildProductMutation.isPending ? <Loader2 className="animate-spin" /> : null}确认换版
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog onOpenChange={(open) => !open && setScriptIdsToArchive([])} open={scriptIdsToArchive.length > 0}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>归档脚本</AlertDialogTitle><AlertDialogDescription>将归档 {scriptIdsToArchive.length} 个脚本单元。历史版本、媒体和生产记录仍会保留。</AlertDialogDescription></AlertDialogHeader>
          <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={archiveScriptsMutation.isPending} onClick={() => archiveScriptsMutation.mutate(scriptIdsToArchive)} variant="destructive">归档</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CreateScriptDialog({ open, loading, onOpenChange, onSubmit, initialDraft, languages, durations }: {
  open: boolean;
  loading: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (draft: ScriptDraft) => void;
  initialDraft: ScriptDraft;
  languages: CommerceProjectLanguageOption[];
  durations: number[];
}) {
  const [draft, setDraft] = useState<ScriptDraft>(initialDraft);

  const valid = draft.title.trim() && languages.length > 0 && durations.includes(draft.targetDurationSeconds)
    && (draft.languageMode === "auto" || draft.explicitTargetLanguage);
  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader><DialogTitle>新增广告脚本</DialogTitle><DialogDescription>创建独立脚本单元；保存后仍可继续编辑并建立正式版本。</DialogDescription></DialogHeader>
        <div className="grid gap-4">
          <Field label="脚本标题"><Input autoFocus onChange={(event) => setDraft((value) => ({ ...value, title: event.target.value }))} value={draft.title} /></Field>
          <Field label="脚本正文"><Textarea className="min-h-64 resize-y font-mono leading-6" onChange={(event) => setDraft((value) => ({ ...value, content: event.target.value }))} value={draft.content} /></Field>
          <ScriptSettings draft={draft} onChange={setDraft} languages={languages} durations={durations} />
        </div>
        <DialogFooter><Button onClick={() => onOpenChange(false)} type="button" variant="outline">取消</Button><Button disabled={!valid || loading} onClick={() => onSubmit(draft)} type="button">{loading ? <Loader2 className="animate-spin" /> : <Plus />}创建脚本</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ScriptDetailDialog({ projectId, scriptUnitId, open, onOpenChange, onChanged, languages, durations }: {
  projectId: string;
  scriptUnitId: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
  languages: CommerceProjectLanguageOption[];
  durations: number[];
}) {
  const detailQuery = useApiQuery({
    key: qk.commerceScriptUnit(projectId, scriptUnitId ?? ""),
    queryFn: (session) => studioApi.getCommerceScriptUnit(session, projectId, scriptUnitId ?? ""),
    enabled: open && Boolean(scriptUnitId),
  });

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent className="h-[90vh] grid-rows-[auto_minmax(0,1fr)] overflow-hidden p-0 sm:max-w-7xl">
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle>{detailQuery.data ? `脚本 ${String(detailQuery.data.unitNo).padStart(2, "0")} · ${detailQuery.data.title}` : "脚本详情"}</DialogTitle>
          <DialogDescription>编辑脚本、管理版本并核对本地化内容。</DialogDescription>
        </DialogHeader>
        {detailQuery.isLoading ? <div className="space-y-4 p-5"><Skeleton className="h-10" /><Skeleton className="h-80" /></div> : detailQuery.error ? <div className="p-5"><QueryError message={detailQuery.error.message} onRetry={() => void detailQuery.refetch()} /></div> : detailQuery.data ? (
          <ScriptEditor key={detailQuery.data.id} onChanged={onChanged} projectId={projectId} unit={detailQuery.data} languages={languages} durations={durations} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ScriptEditor({ projectId, unit, onChanged, languages, durations }: {
  projectId: string;
  unit: CommerceScriptUnit;
  onChanged: () => void;
  languages: CommerceProjectLanguageOption[];
  durations: number[];
}) {
  const invalidate = useInvalidateKeys();
  const [draft, setDraft] = useState<ScriptDraft>(() => scriptDraftFromUnit(unit));
  const [revision, setRevision] = useState(unit.revision);
  const [saveState, setSaveState] = useState<SaveState>("saved");
  const [selectedLocalizationId, setSelectedLocalizationId] = useState(unit.currentLocalizationId ?? "");
  const [localizedContent, setLocalizedContent] = useState(unit.currentLocalization?.localizedContent ?? unit.currentSourceVersion?.content ?? unit.draftContent);
  const [confirmationLocale, setConfirmationLocale] = useState(unit.languageResolution?.targetLanguage ?? unit.explicitTargetLanguage ?? "zh-CN");
  const [scriptRebuildImpact, setScriptRebuildImpact] = useState<CommerceScriptUnitRebuildImpact | null>(null);
  const [scriptRebuildDialogOpen, setScriptRebuildDialogOpen] = useState(false);
  const initialDraft = scriptDraftFromUnit(unit);
  const lastSavedDraft = useRef(initialDraft);
  const lastSavedSnapshot = useRef(scriptDraftSnapshot(initialDraft));

  const versionsQuery = useApiQuery({
    key: qk.commerceScriptVersions(projectId, unit.id),
    queryFn: (session) => studioApi.listCommerceScriptVersions(session, projectId, unit.id).then((response) => response.items),
  });
  const localizationsQuery = useApiQuery({
    key: qk.commerceLocalizations(projectId, unit.id),
    queryFn: (session) => studioApi.listCommerceScriptLocalizations(session, projectId, unit.id).then((response) => response.items),
  });

  const updateDraftMutation = useApiMutation({
    mutationFn: (session, payload: { draft: ScriptDraft; revision: number; snapshot: string }) =>
      studioApi.updateCommerceScriptUnit(session, projectId, unit.id, scriptUpdateBody(payload.revision, payload.draft, lastSavedDraft.current)),
    onSuccess: (updated, variables) => {
      lastSavedDraft.current = variables.draft;
      lastSavedSnapshot.current = variables.snapshot;
      setRevision(updated.revision);
      setSaveState(scriptDraftSnapshot(draft) === variables.snapshot ? "saved" : "dirty");
      invalidate([qk.commerceScriptUnit(projectId, unit.id), commerceScriptUnitsRoot(projectId)]);
      onChanged();
    },
    onError: (error) => {
      setSaveState("error");
      toast.error(`自动保存失败：${error.message}`);
    },
  });

  const createVersionMutation = useApiMutation({
    mutationFn: async (session, payload: { draft: ScriptDraft; revision: number; snapshot: string }) => {
      let nextRevision = payload.revision;
      if (payload.snapshot !== lastSavedSnapshot.current) {
        const updated = await studioApi.updateCommerceScriptUnit(session, projectId, unit.id, scriptUpdateBody(nextRevision, payload.draft, lastSavedDraft.current));
        nextRevision = updated.revision;
      }
      const result = await studioApi.createCommerceScriptVersion(session, projectId, unit.id, {
        expectedRevision: nextRevision,
        content: payload.draft.content,
        activate: true,
      });
      const impact = result.requiresRebuild
        ? await studioApi.getCommerceScriptUnitRebuildImpact(
            session,
            projectId,
            unit.id,
            scriptUnitRebuildTarget(result.scriptUnit.revision, result.version.id, payload.draft),
          )
        : null;
      return { result, impact };
    },
    onSuccess: ({ result, impact }, variables) => {
      lastSavedDraft.current = variables.draft;
      lastSavedSnapshot.current = variables.snapshot;
      setRevision(result.scriptUnit.revision);
      setSaveState("saved");
      if (impact) {
        setScriptRebuildImpact(impact);
        setScriptRebuildDialogOpen(true);
        toast.success("脚本候选版本已保存，请确认换代影响");
      } else {
        toast.success("脚本新版本已创建并启用");
      }
      invalidate([
        qk.commerceScriptUnit(projectId, unit.id),
        qk.commerceScriptVersions(projectId, unit.id),
        qk.commerceLocalizations(projectId, unit.id),
        commerceScriptUnitsRoot(projectId),
      ]);
      onChanged();
    },
    onError: (error) => toast.error(`创建版本失败：${error.message}`),
  });

  const activateVersionMutation = useApiMutation({
    mutationFn: async (session, versionId: string) => {
      if (unit.activeUnitGenerationId) {
        const impact = await studioApi.getCommerceScriptUnitRebuildImpact(
          session,
          projectId,
          unit.id,
          scriptUnitRebuildTarget(revision, versionId, draft),
        );
        return { updated: null as CommerceScriptUnit | null, impact };
      }
      const updated = await studioApi.activateCommerceScriptVersion(session, projectId, unit.id, versionId, revision);
      return { updated, impact: null as CommerceScriptUnitRebuildImpact | null };
    },
    onSuccess: ({ updated, impact }) => {
      if (impact) {
        setScriptRebuildImpact(impact);
        setScriptRebuildDialogOpen(true);
        toast.success("已生成脚本换代影响，请确认后继续");
        return;
      }
      if (!updated) return;
      const activatedDraft = scriptDraftFromUnit(updated);
      setDraft(activatedDraft);
      lastSavedDraft.current = activatedDraft;
      lastSavedSnapshot.current = scriptDraftSnapshot(activatedDraft);
      setSaveState("saved");
      setRevision(updated.revision);
      toast.success("脚本版本已启用");
      invalidate([qk.commerceScriptUnit(projectId, unit.id), qk.commerceScriptVersions(projectId, unit.id), commerceScriptUnitsRoot(projectId)]);
      onChanged();
    },
    onError: (error) => toast.error(`启用失败：${error.message}`),
  });

  const resolveLanguageMutation = useApiMutation({
    mutationFn: (session) => studioApi.resolveCommerceScriptLanguage(session, projectId, unit.id),
    onSuccess: (resolution) => {
      setConfirmationLocale(resolution.targetLanguage ?? "zh-CN");
      toast.success(resolution.needsUserConfirmation ? "语言判断完成，请确认目标语言" : "语言判断完成");
      invalidate([qk.commerceScriptUnit(projectId, unit.id), qk.commerceLanguageResolution(projectId, unit.id)]);
    },
    onError: (error) => toast.error(`语言判断失败：${error.message}`),
  });

  const confirmLanguageMutation = useApiMutation({
    mutationFn: (session, payload: { resolutionId: string; locale: string }) =>
      studioApi.confirmCommerceScriptLanguage(session, projectId, unit.id, {
        languageResolutionId: payload.resolutionId,
        targetLanguage: payload.locale,
      }),
    onSuccess: (response) => {
      const acceptedByWorkflow = "workflowRun" in response;
      const confirmedResolution = acceptedByWorkflow ? response.languageResolution : response;
      setConfirmationLocale(confirmedResolution.targetLanguage ?? confirmationLocale);
      toast.success(acceptedByWorkflow ? "目标语言确认已提交，任务将继续执行" : "目标语言已确认");
      invalidate([
        qk.commerceScriptUnit(projectId, unit.id),
        qk.commerceLanguageResolution(projectId, unit.id),
        qk.workflowRuns(projectId),
      ]);
    },
    onError: (error) => toast.error(`确认失败：${error.message}`),
  });

  const saveLocalizationMutation = useApiMutation({
    mutationFn: (session, payload: { content: string; resolution: CommerceLanguageResolution }) => {
      if (!unit.currentSourceVersionId || !payload.resolution.sourceLanguage || !payload.resolution.targetLanguage) {
        throw new Error("当前脚本版本或语言信息不完整");
      }
      return studioApi.createCommerceScriptLocalization(session, projectId, unit.id, {
        sourceScriptVersionId: unit.currentSourceVersionId,
        languageResolutionId: payload.resolution.id,
        sourceLanguage: payload.resolution.sourceLanguage,
        targetLanguage: payload.resolution.targetLanguage,
        localizedContent: payload.content,
        structuredContract: {},
        reviewerOutput: { source: "manual" },
        approve: true,
      });
    },
    onSuccess: (localization) => {
      setSelectedLocalizationId(localization.id);
      toast.success("本地化版本已保存");
      invalidate([qk.commerceScriptUnit(projectId, unit.id), qk.commerceLocalizations(projectId, unit.id), commerceScriptUnitsRoot(projectId)]);
      onChanged();
    },
    onError: (error) => toast.error(`保存本地化失败：${error.message}`),
  });

  const activateLocalizationMutation = useApiMutation({
    mutationFn: async (session, localizationId: string) => {
      await studioApi.activateCommerceScriptLocalization(session, projectId, unit.id, localizationId, revision);
      return studioApi.getCommerceScriptUnit(session, projectId, unit.id);
    },
    onSuccess: (updated) => {
      setRevision(updated.revision);
      toast.success("本地化版本已启用");
      invalidate([qk.commerceScriptUnit(projectId, unit.id), qk.commerceLocalizations(projectId, unit.id), commerceScriptUnitsRoot(projectId)]);
      onChanged();
    },
    onError: (error) => toast.error(`启用失败：${error.message}`),
  });

  const prepareMutation = useApiMutation({
    mutationFn: (session) => studioApi.prepareCommerceScriptUnit(session, projectId, unit.id, revision),
    onSuccess: (run) => {
      toast.success(`分镜准备任务已提交：${run.id.slice(0, 8)}`);
      invalidate([
        qk.commerceScriptUnit(projectId, unit.id),
        commerceScriptUnitsRoot(projectId),
        qk.workflowRuns(projectId),
      ]);
      onChanged();
    },
    onError: (error) => toast.error(`启动失败：${error.message}`),
  });

  const organizeMutation = useApiMutation({
    mutationFn: (session) => {
      if (!unit.activeUnitGenerationId) throw new Error("当前脚本尚未建立生产代");
      return studioApi.organizeCommerceScriptUnit(
        session,
        projectId,
        unit.id,
        unit.activeUnitGenerationId,
        crypto.randomUUID(),
      );
    },
    onSuccess: (run) => {
      toast.success(`销售脚本整理任务已提交：${run.id.slice(0, 8)}`);
      invalidate([
        qk.commerceScriptUnit(projectId, unit.id),
        qk.commerceUnitProductionStatus(projectId, unit.id),
        qk.commerceProjectProductionStatus(projectId),
        qk.workflowRuns(projectId),
      ]);
      onChanged();
    },
    onError: (error) => toast.error(`整理失败：${error.message}`),
  });

  const rebuildScriptMutation = useApiMutation({
    mutationFn: (session, impact: CommerceScriptUnitRebuildImpact) => studioApi.createCommerceScriptUnitRebuild(
      session,
      projectId,
      unit.id,
      { impactToken: impact.impactToken, expectedRevision: impact.expectedRevision },
      crypto.randomUUID(),
    ),
    onSuccess: (run) => {
      setScriptRebuildImpact(null);
      setScriptRebuildDialogOpen(false);
      toast.success(`脚本换代任务已提交：${run.id.slice(0, 8)}`);
      invalidate([
        qk.commerceScriptUnit(projectId, unit.id),
        qk.commerceScriptVersions(projectId, unit.id),
        qk.commerceLocalizations(projectId, unit.id),
        qk.commerceUnitProductionStatus(projectId, unit.id),
        qk.commerceProjectProductionStatus(projectId),
        qk.workflowRuns(projectId),
        commerceScriptUnitsRoot(projectId),
      ]);
      onChanged();
    },
    onError: (error) => toast.error(`脚本换代失败：${error.message}`),
  });

  const snapshot = scriptDraftSnapshot(draft);
  useEffect(() => {
    if (snapshot === lastSavedSnapshot.current || updateDraftMutation.isPending || createVersionMutation.isPending) return;
    const timeout = window.setTimeout(() => {
      setSaveState("saving");
      updateDraftMutation.mutate({ draft, revision, snapshot });
    }, 900);
    return () => window.clearTimeout(timeout);
  }, [createVersionMutation.isPending, draft, revision, snapshot, updateDraftMutation]);

  const localizations = localizationsQuery.data ?? [];
  const selectedLocalization = localizations.find((item) => item.id === selectedLocalizationId) ?? unit.currentLocalization ?? localizations[0];
  const resolution = unit.languageResolution;
  const sourceContent = unit.currentSourceVersion?.content ?? unit.draftContent;
  const timingSeconds = selectedLocalization?.estimatedVoiceoverSeconds;

  return (
    <>
    <Tabs className="min-h-0" defaultValue="editor">
      <div className="flex items-center justify-between gap-3 border-b px-5 py-2">
        <TabsList variant="line"><TabsTrigger value="editor"><FileText />编辑</TabsTrigger><TabsTrigger value="versions"><History />版本</TabsTrigger><TabsTrigger value="localization"><Languages />本地化</TabsTrigger></TabsList>
        <div className="flex items-center gap-2">
          <SaveIndicator state={saveState} />
          {scriptRebuildImpact ? <Button onClick={() => setScriptRebuildDialogOpen(true)} size="sm" type="button" variant="outline">确认脚本换代</Button> : null}
          {unit.activeUnitGenerationId ? (
            <Button disabled={saveState !== "saved" || organizeMutation.isPending} onClick={() => organizeMutation.mutate()} size="sm" type="button">
              {organizeMutation.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}整理销售脚本
            </Button>
          ) : (
            <Button disabled={!unit.currentSourceVersionId || saveState !== "saved" || prepareMutation.isPending} onClick={() => prepareMutation.mutate()} size="sm" type="button">
              {prepareMutation.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}准备并生成分镜
            </Button>
          )}
        </div>
      </div>

      <TabsContent className="min-h-0 overflow-hidden" value="editor">
        <ScrollArea className="h-full">
          <div className="space-y-4 p-5">
            <Field label="脚本标题"><Input onChange={(event) => { setDraft((value) => ({ ...value, title: event.target.value })); setSaveState("dirty"); }} value={draft.title} /></Field>
            <Field label="脚本正文"><Textarea className="min-h-[42vh] resize-y font-mono text-sm leading-6" onChange={(event) => { setDraft((value) => ({ ...value, content: event.target.value })); setSaveState("dirty"); }} value={draft.content} /></Field>
            <ScriptSettings draft={draft} onChange={(next) => { setDraft(next); setSaveState("dirty"); }} languages={languages} durations={durations} />
            <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
              <div className="flex flex-wrap gap-2 text-xs text-muted-foreground"><span>单元编号 {unit.unitNo}</span><span>当前版本 {unit.currentSourceVersion?.version ?? "未建立"}</span><span>更新于 {formatDateTime(unit.updatedAt)}</span></div>
              <Button disabled={!draft.content.trim() || updateDraftMutation.isPending || createVersionMutation.isPending} onClick={() => createVersionMutation.mutate({ draft, revision, snapshot })} type="button">
                {createVersionMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}创建并启用新版本
              </Button>
            </div>
          </div>
        </ScrollArea>
      </TabsContent>

      <TabsContent className="min-h-0 overflow-hidden" value="versions">
        <ScrollArea className="h-full"><div className="space-y-2 p-5">{versionsQuery.isLoading ? <Skeleton className="h-32" /> : (versionsQuery.data ?? []).map((version) => (
          <div className="flex items-start justify-between gap-4 rounded-lg border p-3" key={version.id}>
            <div className="min-w-0"><div className="flex items-center gap-2"><span className="font-medium">版本 {version.version}</span>{unit.currentSourceVersionId === version.id ? <Badge variant="secondary"><Check />当前</Badge> : null}</div><p className="mt-2 line-clamp-3 whitespace-pre-wrap text-sm leading-5 text-muted-foreground">{version.content}</p><p className="mt-2 text-xs text-muted-foreground">{formatDateTime(version.createdAt)} · {version.manualOverride ? "人工版本" : "系统版本"}</p></div>
            {unit.currentSourceVersionId !== version.id ? <Button disabled={activateVersionMutation.isPending} onClick={() => activateVersionMutation.mutate(version.id)} size="sm" type="button" variant="outline">启用</Button> : null}
          </div>
        ))}</div></ScrollArea>
      </TabsContent>

      <TabsContent className="min-h-0 overflow-hidden" value="localization">
        <ScrollArea className="h-full">
          <div className="space-y-4 p-5">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/20 p-3">
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <Badge variant="outline">源语言：{localeLabel(resolution?.sourceLanguage ?? "", languages)}</Badge>
                <Badge variant="outline">目标语言：{localeLabel(resolution?.targetLanguage ?? unit.explicitTargetLanguage ?? "", languages)}</Badge>
                {typeof resolution?.confidence === "number" ? <span className="text-xs text-muted-foreground">判断置信度 {Math.round(resolution.confidence * 100)}%</span> : null}
                {timingSeconds ? <span className={cn("text-xs", timingSeconds > unit.targetDurationSeconds ? "text-destructive" : "text-muted-foreground")}>预计旁白 {timingSeconds.toFixed(1)} 秒 / 目标 {unit.targetDurationSeconds} 秒</span> : null}
              </div>
              <div className="flex items-center gap-2">
                <Button disabled={resolveLanguageMutation.isPending || !unit.currentSourceVersionId} onClick={() => resolveLanguageMutation.mutate()} size="sm" type="button" variant="outline">{resolveLanguageMutation.isPending ? <Loader2 className="animate-spin" /> : <Globe2 />}判断语言</Button>
              </div>
            </div>

            {resolution?.needsUserConfirmation || resolution?.status === "needs_confirmation" ? (
              <div className="flex flex-wrap items-end gap-3 rounded-lg border border-amber-300/60 bg-amber-50/60 p-3 dark:bg-amber-950/10">
                <Field className="min-w-56 flex-1" label="确认目标语言"><Select onValueChange={setConfirmationLocale} value={confirmationLocale}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{languages.map((locale) => <SelectItem key={locale.locale} value={locale.locale}>{locale.label}</SelectItem>)}</SelectContent></Select></Field>
                <Button disabled={confirmLanguageMutation.isPending} onClick={() => confirmLanguageMutation.mutate({ resolutionId: resolution.id, locale: confirmationLocale })} type="button">{confirmLanguageMutation.isPending ? <Loader2 className="animate-spin" /> : <Check />}确认语言</Button>
              </div>
            ) : null}

            <div className="flex flex-wrap items-center gap-2">
              <Label>本地化版本</Label>
              <Select
                onValueChange={(id) => {
                  setSelectedLocalizationId(id);
                  const item = localizations.find((localization) => localization.id === id);
                  if (item) setLocalizedContent(item.localizedContent);
                }}
                value={selectedLocalization?.id ?? "none"}
              >
                <SelectTrigger className="w-56"><SelectValue placeholder="尚无本地化版本" /></SelectTrigger>
                <SelectContent>
                  {localizations.length === 0 ? <SelectItem disabled value="none">尚无本地化版本</SelectItem> : localizations.map((localization) => <SelectItem key={localization.id} value={localization.id}>版本 {localization.version} · {localeLabel(localization.targetLanguage, languages)}</SelectItem>)}
                </SelectContent>
              </Select>
              {selectedLocalization && unit.currentLocalizationId !== selectedLocalization.id && selectedLocalization.status === "approved" ? <Button disabled={activateLocalizationMutation.isPending} onClick={() => activateLocalizationMutation.mutate(selectedLocalization.id)} size="sm" type="button" variant="outline">启用此版本</Button> : null}
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
              <Field label="原始脚本"><Textarea className="min-h-[38vh] resize-y bg-muted/30 font-mono leading-6" readOnly value={sourceContent} /></Field>
              <Field label="本地化脚本"><Textarea className="min-h-[38vh] resize-y font-mono leading-6" onChange={(event) => setLocalizedContent(event.target.value)} value={localizedContent} /></Field>
            </div>
            <div className="flex justify-end">
              <Button disabled={!resolution || resolution.status !== "confirmed" || !localizedContent.trim() || saveLocalizationMutation.isPending} onClick={() => resolution && saveLocalizationMutation.mutate({ content: localizedContent, resolution })} type="button">
                {saveLocalizationMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}保存本地化版本
              </Button>
            </div>
          </div>
        </ScrollArea>
      </TabsContent>
    </Tabs>

    <AlertDialog onOpenChange={setScriptRebuildDialogOpen} open={scriptRebuildDialogOpen && Boolean(scriptRebuildImpact)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>确认脚本换代</AlertDialogTitle>
          <AlertDialogDescription>
            系统会保留当前生产代，先按候选脚本重新整理并生成分镜；新分镜准备成功后才原子切换。不会自动生成图片或视频。
          </AlertDialogDescription>
        </AlertDialogHeader>
        {scriptRebuildImpact ? (
          <div className="space-y-3 text-sm">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <ImpactCount label="分镜方案" value={scriptRebuildImpact.affected.storyboardPlans} />
              <ImpactCount label="镜头" value={scriptRebuildImpact.affected.storyboardShots} />
              <ImpactCount label="参考图" value={scriptRebuildImpact.affected.referenceImages} />
              <ImpactCount label="视频提示词" value={scriptRebuildImpact.affected.videoPrompts} />
              <ImpactCount label="镜头视频" value={scriptRebuildImpact.affected.shotVideos} />
              <ImpactCount label="时间线" value={scriptRebuildImpact.affected.timelines} />
              <ImpactCount label="成片" value={scriptRebuildImpact.affected.finalVideos} />
              <ImpactCount label="预计 Agent 调用" value={scriptRebuildImpact.estimatedAgentCalls} />
            </div>
            {scriptRebuildImpact.blockers.length ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-destructive">
                {scriptRebuildImpact.blockers.map((blocker) => <p key={blocker}>{blocker}</p>)}
              </div>
            ) : null}
          </div>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>稍后处理</AlertDialogCancel>
          <AlertDialogAction
            disabled={!scriptRebuildImpact || scriptRebuildImpact.blockers.length > 0 || rebuildScriptMutation.isPending}
            onClick={() => scriptRebuildImpact && rebuildScriptMutation.mutate(scriptRebuildImpact)}
          >
            {rebuildScriptMutation.isPending ? <Loader2 className="animate-spin" /> : null}确认并重建分镜
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    </>
  );
}

function ImpactCount({ label, value }: { label: string; value: number }) {
  return <div className="rounded-md border bg-muted/20 p-2"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 font-medium">{value}</p></div>;
}

function ScriptSettings({ draft, onChange, languages, durations }: {
  draft: ScriptDraft;
  onChange: (draft: ScriptDraft) => void;
  languages: CommerceProjectLanguageOption[];
  durations: number[];
}) {
  return (
    <div className="grid gap-4 rounded-lg border bg-muted/15 p-3 sm:grid-cols-2 xl:grid-cols-4">
      <Field label="目标语言方式"><Select onValueChange={(value) => onChange({ ...draft, languageMode: value as LanguageMode })} value={draft.languageMode}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="auto">自动判断</SelectItem><SelectItem value="explicit">明确指定</SelectItem></SelectContent></Select></Field>
      <Field label="目标语言"><Select disabled={draft.languageMode === "auto"} onValueChange={(value) => onChange({ ...draft, explicitTargetLanguage: value })} value={draft.explicitTargetLanguage}><SelectTrigger><SelectValue placeholder="选择可执行语言" /></SelectTrigger><SelectContent>{languages.map((locale) => <SelectItem key={locale.locale} value={locale.locale}>{locale.label}</SelectItem>)}</SelectContent></Select></Field>
      <Field label="目标时长"><Select onValueChange={(value) => onChange({ ...draft, targetDurationSeconds: Number(value) })} value={String(draft.targetDurationSeconds)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{durations.map((duration) => <SelectItem key={duration} value={String(duration)}>{duration} 秒</SelectItem>)}</SelectContent></Select></Field>
      <Field label="目标平台"><Select onValueChange={(value) => onChange({ ...draft, targetPlatform: value })} value={draft.targetPlatform}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{targetPlatforms.map((platform) => <SelectItem key={platform.value} value={platform.value}>{platform.label}</SelectItem>)}</SelectContent></Select></Field>
    </div>
  );
}

function SaveIndicator({ state }: { state: SaveState }) {
  const content = state === "saving" ? <><Loader2 className="size-3.5 animate-spin" />正在保存</> : state === "dirty" ? <>未保存</> : state === "error" ? <>保存失败</> : <><Check className="size-3.5" />已保存</>;
  return <span className={cn("flex items-center gap-1 text-xs", state === "error" ? "text-destructive" : "text-muted-foreground")}>{content}</span>;
}

function Field({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return <div className={cn("grid gap-1.5", className)}><Label>{label}</Label>{children}</div>;
}

function QueryError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return <div className="flex min-h-40 flex-col items-center justify-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-5 text-center"><p className="text-sm text-destructive">{message}</p><Button onClick={onRetry} size="sm" type="button" variant="outline">重新加载</Button></div>;
}

function commerceScriptUnitsRoot(projectId: string) {
  return qk.commerceScriptUnits(projectId).slice(0, 3);
}

function unwrapScriptUnit(value: unknown): CommerceScriptUnit | undefined {
  if (!isRecord(value)) return undefined;
  const candidate = isRecord(value.scriptUnit) ? value.scriptUnit : value;
  return typeof candidate.id === "string" ? candidate as CommerceScriptUnit : undefined;
}

function scriptCreateBody(expectedRevision: number, draft: ScriptDraft) {
  return {
    expectedScriptUnitsRevision: expectedRevision,
    title: draft.title.trim(),
    content: draft.content,
    languageMode: draft.languageMode,
    explicitTargetLanguage: draft.languageMode === "explicit" ? draft.explicitTargetLanguage : undefined,
    targetDurationSeconds: draft.targetDurationSeconds,
    targetPlatform: draft.targetPlatform,
  };
}

function scriptUpdateBody(expectedRevision: number, draft: ScriptDraft, saved: ScriptDraft) {
  const body: {
    expectedRevision: number;
    title?: string;
    draftContent?: string;
    languageMode?: LanguageMode;
    explicitTargetLanguage?: string | null;
    targetDurationSeconds?: number;
    targetPlatform?: string;
  } = { expectedRevision };
  if (draft.title.trim() !== saved.title.trim()) body.title = draft.title.trim();
  if (draft.content !== saved.content) body.draftContent = draft.content;
  if (draft.languageMode !== saved.languageMode) {
    body.languageMode = draft.languageMode;
    body.explicitTargetLanguage = draft.languageMode === "explicit" ? draft.explicitTargetLanguage : null;
  } else if (draft.languageMode === "explicit" && draft.explicitTargetLanguage !== saved.explicitTargetLanguage) {
    body.explicitTargetLanguage = draft.explicitTargetLanguage;
  }
  if (draft.targetDurationSeconds !== saved.targetDurationSeconds) body.targetDurationSeconds = draft.targetDurationSeconds;
  if (draft.targetPlatform !== saved.targetPlatform) body.targetPlatform = draft.targetPlatform;
  return body;
}

function scriptUnitRebuildTarget(expectedRevision: number, targetSourceScriptVersionId: string, draft: ScriptDraft) {
  return {
    expectedRevision,
    targetSourceScriptVersionId,
    targetLanguageMode: draft.languageMode,
    targetLanguage: draft.languageMode === "explicit" ? draft.explicitTargetLanguage : undefined,
    targetDurationSeconds: draft.targetDurationSeconds,
    targetPlatform: draft.targetPlatform,
  };
}

function productDraftFromProduct(product: CommerceProduct): ProductDraft {
  const version = product.currentVersion;
  return {
    name: version?.name ?? "",
    brand: version?.brand ?? "",
    sellingPoints: stringList(version?.sellingPoints).join("\n"),
    immutableFeatures: immutableFeatureList(version?.immutableFeatures).join("\n"),
    prohibitedClaims: stringList(version?.prohibitedClaims).join("\n"),
    notes: stringProperty(product.metadata, "notes"),
  };
}

function scriptDraftFromUnit(unit: CommerceScriptUnit): ScriptDraft {
  return {
    title: unit.title,
    content: unit.draftContent || unit.currentSourceVersion?.content || "",
    languageMode: unit.languageMode,
    explicitTargetLanguage: unit.explicitTargetLanguage ?? unit.languageResolution?.targetLanguage ?? "zh-CN",
    targetDurationSeconds: unit.targetDurationSeconds,
    targetPlatform: unit.targetPlatform,
  };
}

function scriptDraftSnapshot(draft: ScriptDraft) {
  return JSON.stringify({ ...draft, title: draft.title.trim() });
}

function lines(value: string) {
  return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function immutableFeatureList(value: unknown): string[] {
  if (!isRecord(value)) return [];
  const packaging = value.packaging;
  if (Array.isArray(packaging)) return packaging.filter((item): item is string => typeof item === "string");
  return Object.entries(value).map(([key, item]) => `${key}：${typeof item === "string" ? item : JSON.stringify(item)}`);
}

function stringProperty(value: unknown, key: string) {
  return isRecord(value) && typeof value[key] === "string" ? value[key] : "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function uniqueById<T extends { id: string }>(items: T[]) {
  return [...new Map(items.map((item) => [item.id, item])).values()];
}

function referenceRoleLabel(reference: CommerceProductReference) {
  if (reference.isPrimary) return "主图";
  return referenceRoles.find((role) => role.value === reference.referenceRole)?.label ?? "其他";
}

function qualityLabel(value: unknown) {
  if (!isRecord(value) || typeof value.status !== "string") return "待检查";
  if (value.status === "accepted" || value.status === "passed") return "质量通过";
  if (value.status === "rejected" || value.status === "failed") return "需要替换";
  return "待检查";
}

function executableLanguageOptions(
  languages: CommerceProjectLanguageOption[],
  audioStrategy?: Project["audioStrategy"],
  audioRequirement?: Project["audioRequirement"],
) {
  return languages.filter((language) => {
    if (!language.textAvailable || !language.imagePromptAvailable || !language.videoPromptAvailable) return false;
    return !(audioStrategy === "native_av" && audioRequirement === "required") || language.nativeAudioAvailable;
  });
}

function scriptDraftFromProjectDefaults(
  project: Project | undefined,
  languages: CommerceProjectLanguageOption[],
  durations: number[],
): ScriptDraft {
  const defaults = project?.scriptUnitDefaults;
  const duration = defaults && durations.includes(defaults.targetDurationSeconds)
    ? defaults.targetDurationSeconds
    : durations[0] ?? 30;
  const targetLanguage = defaults?.targetLanguage && languages.some((language) => language.locale === defaults.targetLanguage)
    ? defaults.targetLanguage
    : languages[0]?.locale ?? "";
  return {
    title: "",
    content: "",
    languageMode: defaults?.languageMode ?? "auto",
    explicitTargetLanguage: targetLanguage,
    targetDurationSeconds: duration,
    targetPlatform: defaults?.targetPlatform ?? "douyin",
  };
}

function scriptProductionSummary(script: CommerceScriptUnit): CommerceScriptProductionSummary | undefined {
  const value = (script as unknown as Record<string, unknown>).productionSummary;
  if (!isRecord(value)) return undefined;
  return {
    currentStage: typeof value.currentStage === "string" ? value.currentStage : undefined,
    failedCount: typeof value.failedCount === "number" ? value.failedCount : undefined,
    finalVideoStatus: typeof value.finalVideoStatus === "string" ? value.finalVideoStatus : undefined,
  };
}

function scriptStageLabel(stage: string | undefined, script: CommerceScriptUnit) {
  const labels: Record<string, string> = {
    draft: "草稿",
    language_resolution: "语言确认",
    localization: "本地化",
    storyboard: "分镜制作",
    reference_images: "参考图生成",
    video_prompts: "提示词生成",
    shot_videos: "视频生成",
    final_video: "成片合成",
    completed: "已完成",
    failed: "执行失败",
  };
  if (stage) return labels[stage] ?? stage;
  if (script.activeUnitGenerationId) return "待继续制作";
  if (script.currentSourceVersionId) return "脚本已就绪";
  return "草稿";
}

function scriptPreview(script: CommerceScriptUnit) {
  return (script.draftContent || script.currentSourceVersion?.content || "尚未填写脚本正文").trim();
}

function formatDateTime(value?: string | null) {
  if (!value) return "未记录时间";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", { hour12: false });
}

function isActiveCommerceSetupState(state: CommerceSetupState | undefined) {
	return state === "resolving_language" || state === "waiting_user_confirmation" || state === "localizing" ||
		state === "validating" || state === "ready" || state === "starting" || state === "started";
}

function commerceSetupStateLabel(state: CommerceSetupState) {
	const labels: Record<CommerceSetupState, string> = {
		draft: "项目资料待完善",
		uploading: "商品图片上传中",
		resolving_language: "正在判断视频语言",
		waiting_user_confirmation: "请确认视频语言",
		localizing: "正在生成并审核多语言脚本",
		validating: "正在校验模型与生产配置",
		needs_user_review: "创建流程需要人工处理",
		ready: "项目准备完成",
		starting: "正在启动项目准备流程",
		started: "项目准备流程运行中",
		completed: "项目准备完成",
		failed: "项目准备失败",
		abandoned: "项目创建已放弃",
	};
	return labels[state];
}
