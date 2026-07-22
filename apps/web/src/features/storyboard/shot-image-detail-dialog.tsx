"use client";

import NextImage from "next/image";
import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Check, GitBranch, Image as ImageIcon, Layers3, Link2Off, Loader2, Maximize2, Plus, RefreshCw, Save, Search, Sparkles, Video, X } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { cssAspectRatio } from "@/lib/aspect-ratio";
import { localizePlatformError } from "@/lib/error-localization";
import { assetTypeLabel, modalityLabel, shotAnchorRoleLabel, shotReferenceRoleLabel, shotReferenceSemanticsLabel, shotStateRoleLabel, shotTransitionTypeLabel, statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import { wholeSecondDuration } from "@/lib/timing";
import { cn } from "@/lib/utils";
import type { ShotReferencePackResponse, ShotVisualAnchor, StoryboardSheetResponse, StoryboardShotDetail, StoryboardShotImageReferenceOption, StoryboardShotStateResponse, StoryboardShotTransitionResponse } from "@/lib/types";

type ReferenceMode = "auto" | "custom" | "none";
type PlannedAnchorRole = "planned_first_frame" | "planned_last_frame" | "storyboard_sheet";

type ImageDraft = {
  shotId: string;
  promptRevision: string;
  imagePrompt: string;
  referenceMode: ReferenceMode;
  referenceKeys: string[];
};

const EMPTY_DRAFT: ImageDraft = { shotId: "", promptRevision: "", imagePrompt: "", referenceMode: "auto", referenceKeys: [] };
const ASSET_TYPE_FILTERS = ["all", "character", "scene", "prop"] as const;
const SINGLE_ANCHOR_ROLES: PlannedAnchorRole[] = ["planned_first_frame"];
const FIRST_LAST_ANCHOR_ROLES: PlannedAnchorRole[] = ["planned_first_frame", "planned_last_frame"];
const STORYBOARD_SHEET_ANCHOR_ROLES: PlannedAnchorRole[] = ["storyboard_sheet"];

export function ShotImageDetailDialog({
  projectId,
  shotId,
  shotNumber,
  open,
  onOpenChange,
  onChanged,
}: {
  projectId: string;
  shotId: string;
  shotNumber: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}) {
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);
  const [draft, setDraft] = useState<ImageDraft>(EMPTY_DRAFT);
  const [largePreview, setLargePreview] = useState<{ url: string; title: string } | null>(null);
  const [assetPickerOpen, setAssetPickerOpen] = useState(false);
  const [assetSearch, setAssetSearch] = useState("");
  const [assetTypeFilter, setAssetTypeFilter] = useState<(typeof ASSET_TYPE_FILTERS)[number]>("all");
  const [selectedAnchorRole, setSelectedAnchorRole] = useState<PlannedAnchorRole>("planned_first_frame");
  const { data: project } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
    enabled: open && !!projectId,
  });
  const profileKey = project?.videoProductionBinding?.profileKey ?? "single_frame_i2v";
  const firstLastProfile = profileKey === "first_last_frame";
  const storyboardSheetProfile = profileKey === "storyboard_sheet";
  const managedAnchorProfile = firstLastProfile || storyboardSheetProfile;
  const { data: detail, isLoading } = useApiQuery({
    key: qk.shotDetail(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotDetail(session, projectId, shotId),
    enabled: open && !!shotId,
    refetchInterval: (query) => {
      const imageStatus = query.state.data?.shot.imageStatus;
      const promptStatus = query.state.data?.shot.imagePromptStatus;
      return pollingFallback && open && (
        imageStatus === "queued"
        || imageStatus === "running"
        || promptStatus === "queued"
        || promptStatus === "running"
      ) ? 5000 : false;
    },
  });
  const { data: shotStates } = useApiQuery({
    key: qk.shotState(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotState(session, projectId, shotId),
    enabled: open && !!shotId,
  });
  const { data: shotTransition } = useApiQuery({
    key: qk.shotTransition(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotTransition(session, projectId, shotId),
    enabled: open && !!shotId,
  });
  const { data: shotAnchors } = useApiQuery({
    key: qk.shotAnchors(projectId, shotId || "none"),
    queryFn: (session) => studioApi.listStoryboardShotAnchors(session, projectId, shotId),
    enabled: open && !!shotId,
    refetchInterval: (query) => pollingFallback && open && query.state.data?.items.some((anchor) => anchor.status === "generating") ? 5000 : false,
  });
  const { data: referencePack } = useApiQuery({
    key: qk.shotReferencePack(projectId, shotId || "none", "anchor"),
    queryFn: (session) => studioApi.getStoryboardShotReferencePack(session, projectId, shotId, "anchor"),
    enabled: open && !!shotId,
  });
  const { data: storyboardSheet } = useApiQuery({
    key: qk.shotStoryboardSheet(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotStoryboardSheet(session, projectId, shotId),
    enabled: open && !!shotId && storyboardSheetProfile,
  });
  const requiredAnchorRoles = firstLastProfile
    ? FIRST_LAST_ANCHOR_ROLES
    : storyboardSheetProfile
      ? STORYBOARD_SHEET_ANCHOR_ROLES
      : SINGLE_ANCHOR_ROLES;
  const currentAnchors = useMemo(
    () => requiredAnchorRoles.map((role) => latestAnchorForRole(shotAnchors?.items ?? [], role)).filter((anchor): anchor is ShotVisualAnchor => !!anchor),
    [requiredAnchorRoles, shotAnchors?.items],
  );
  const activeAnchorRole: PlannedAnchorRole = storyboardSheetProfile ? "storyboard_sheet" : selectedAnchorRole;
  const selectedAnchor = latestAnchorForRole(shotAnchors?.items ?? [], activeAnchorRole);
  const requiredAnchorsApproved = requiredAnchorRoles.every((role) => {
    const anchor = latestAnchorForRole(shotAnchors?.items ?? [], role);
    return anchor?.status === "ready" && anchor.reviewStatus === "approved" && !!anchor.previewUrl;
  });

  const detailPromptRevision = detail ? imagePromptRevision(detail) : "";
  if (detail && (draft.shotId !== detail.shot.id || draft.promptRevision !== detailPromptRevision)) {
    setDraft(draftFromDetail(detail));
  }

  const referenceOptions = useMemo(() => detail?.imageReferenceOptions ?? [], [detail?.imageReferenceOptions]);
  const selectedReferenceKeys = useMemo(() => {
    if (draft.referenceMode === "custom") return new Set(draft.referenceKeys);
    if (draft.referenceMode === "auto") return new Set(referenceOptions.filter((option) => option.autoSelected).map((option) => option.key));
    return new Set<string>();
  }, [draft.referenceKeys, draft.referenceMode, referenceOptions]);
  const visibleReferenceOptions = useMemo(
    () => referenceOptions.filter((option) => option.isShotAsset || selectedReferenceKeys.has(option.key)),
    [referenceOptions, selectedReferenceKeys],
  );
  const otherReferenceOptions = useMemo(
    () => referenceOptions.filter((option) => !option.isShotAsset && !selectedReferenceKeys.has(option.key)),
    [referenceOptions, selectedReferenceKeys],
  );
  const filteredOtherReferenceOptions = useMemo(() => {
    const search = assetSearch.trim().toLocaleLowerCase("zh-CN");
    return otherReferenceOptions.filter((option) => {
      if (assetTypeFilter !== "all" && option.assetType !== assetTypeFilter) return false;
      if (!search) return true;
      return `${option.assetName} ${option.title}`.toLocaleLowerCase("zh-CN").includes(search);
    });
  }, [assetSearch, assetTypeFilter, otherReferenceOptions]);
  const imageRunning = detail?.shot.imageStatus === "queued" || detail?.shot.imageStatus === "running";
  const promptRunning = detail?.shot.imagePromptStatus === "queued" || detail?.shot.imagePromptStatus === "running";
  const promptManuallyChanged = !!detail && draft.imagePrompt.trim() !== (detail.shot.imagePrompt ?? "").trim();
  const promptReady = detail?.shot.imagePromptStatus === "succeeded";
  const referencesValid = draft.referenceMode !== "custom" || draft.referenceKeys.length > 0;
  const canGenerate = referencesValid && draft.imagePrompt.trim().length > 0 && (promptReady || promptManuallyChanged);

  const refresh = () => {
    invalidate([
      qk.shotDetail(projectId, shotId),
      qk.shotState(projectId, shotId),
      qk.shotTransition(projectId, shotId),
      qk.shotAnchors(projectId, shotId),
      qk.shotReferencePack(projectId, shotId, "anchor"),
      qk.shotStoryboardSheet(projectId, shotId),
      qk.shotVideoPromptPlan(projectId, shotId),
      qk.shotRenderPlan(projectId, shotId),
      qk.shotProductionPrefix(projectId),
      qk.workflowRuns(projectId),
      qk.productionStatus(projectId),
      qk.artifacts(projectId),
    ]);
    onChanged();
  };

  const saveMutation = useApiMutation({
    mutationFn: (session) => studioApi.updateStoryboardShot(session, projectId, shotId, imageUpdateBody(draft)),
    onSuccess: (shot) => {
      setDraft((current) => ({ ...current, imagePrompt: shot.imagePrompt ?? "", referenceMode: normalizeReferenceMode(shot.imageReferenceMode), referenceKeys: shot.imageReferenceKeys ?? [] }));
      toast.success("分镜图片设置已保存");
      refresh();
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const promptMutation = useApiMutation({
    mutationFn: async (session) => {
      await studioApi.updateStoryboardShot(session, projectId, shotId, imageReferenceUpdateBody(draft));
      return studioApi.runShotProductionAction(session, projectId, {
        action: "generate_selected_image_prompts",
        shotIds: [shotId],
        options: { force: true, maxConcurrency: 1 },
      });
    },
    onSuccess: () => {
      toast.success(detail?.shot.imagePrompt ? "分镜图片提示词重新生成中" : "分镜图片提示词生成中");
      refresh();
    },
    onError: (error) => toast.error("提示词生成失败：" + error.message),
  });

  const generateMutation = useApiMutation({
    mutationFn: async (session) => {
      await studioApi.updateStoryboardShot(
        session,
        projectId,
        shotId,
        promptManuallyChanged ? imageUpdateBody(draft) : imageReferenceUpdateBody(draft),
      );
      return studioApi.runShotProductionAction(session, projectId, {
        action: "generate_selected_images",
        shotIds: [shotId],
        options: { maxConcurrency: 1 },
      });
    },
    onSuccess: () => {
      toast.success(detail?.shot.imageArtifactId ? "分镜图片重新生成中" : "分镜图片生成中");
      refresh();
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const unlinkMutation = useApiMutation({
    mutationFn: (session) => studioApi.unlinkStoryboardShotMedia(session, projectId, shotId, "image"),
    onSuccess: () => {
      toast.success("当前图片已解绑");
      refresh();
    },
    onError: (error) => toast.error("解绑失败：" + error.message),
  });

  const videoMutation = useApiMutation({
    mutationFn: (session) => studioApi.runShotProductionAction(session, projectId, {
      action: "generate_selected_videos",
      shotIds: [shotId],
      options: { maxConcurrency: 1 },
    }),
    onSuccess: () => {
      toast.success("镜头视频生成中");
      refresh();
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const anchorGenerateMutation = useApiMutation({
    mutationFn: (session, anchorRole: PlannedAnchorRole) => studioApi.generateStoryboardShotAnchor(session, projectId, shotId, anchorRole),
    onSuccess: (_result, anchorRole) => {
      toast.success(`${shotAnchorRoleLabel(anchorRole)}生成任务已创建`);
      refresh();
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const anchorReviewMutation = useApiMutation({
    mutationFn: (session, input: { anchor: ShotVisualAnchor; decision: "approve" | "reject" }) => studioApi.reviewStoryboardShotAnchor(
      session,
      projectId,
      shotId,
      input.anchor.id,
      input.decision,
      { expectedRevision: input.anchor.revision, reason: input.decision === "reject" ? "人工审核未通过" : "人工确认视觉锚点" },
    ),
    onSuccess: (_anchor, input) => {
      toast.success(input.decision === "approve" ? "视觉锚点已确认" : "视觉锚点已拒绝");
      refresh();
    },
    onError: (error) => toast.error("审核失败：" + error.message),
  });

  const stateReplanMutation = useApiMutation({
    mutationFn: (session) => studioApi.replanStoryboardShotState(session, projectId, shotId),
    onSuccess: () => {
      toast.success("本集分镜重新规划任务已创建");
      refresh();
    },
    onError: (error) => toast.error("重新规划失败：" + error.message),
  });

  function setReferenceMode(mode: ReferenceMode) {
    if (mode !== "custom") setAssetPickerOpen(false);
    setDraft((current) => ({
      ...current,
      referenceMode: mode,
      referenceKeys: mode === "custom"
        ? current.referenceKeys.length > 0
          ? current.referenceKeys
          : referenceOptions.filter((option) => option.selected || option.autoSelected).map((option) => option.key)
        : [],
    }));
  }

  function toggleReference(key: string, checked: boolean) {
    setDraft((current) => {
      const keys = new Set(current.referenceKeys);
      if (checked) keys.add(key);
      else keys.delete(key);
      return { ...current, referenceMode: "custom", referenceKeys: [...keys] };
    });
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!nextOpen && largePreview) return;
      if (!nextOpen) {
        setAssetPickerOpen(false);
        setAssetSearch("");
        setAssetTypeFilter("all");
      }
      onOpenChange(nextOpen);
    }}>
      <DialogContent className="h-[min(90vh,920px)] max-w-[min(96vw,1320px)] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-[min(96vw,1320px)]" showCloseButton={!largePreview}>
        <DialogHeader className="border-b px-5 py-4 pr-14">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle>第 {shotNumber} 镜 · 图片详情</DialogTitle>
            {detail ? <Badge variant="outline">{statusLabel(detail.shot.imageStatus)}</Badge> : null}
            {detail ? <Badge variant="outline">提示词 {statusLabel(detail.shot.imagePromptStatus)}</Badge> : null}
            {detail?.shot.staleState && detail.shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(detail.shot.staleState)}</Badge> : null}
          </div>
          <DialogDescription>{detail?.shot.title || detail?.shot.visual || "查看并调整镜头图片生成设置"}</DialogDescription>
        </DialogHeader>

        {isLoading || !detail ? (
          <div className="grid min-h-0 flex-1 gap-4 overflow-hidden p-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(360px,.85fr)]">
            <Skeleton className="h-full min-h-80" />
            <Skeleton className="h-full min-h-80" />
          </div>
        ) : (
          <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1.15fr)_minmax(360px,.85fr)] lg:overflow-hidden">
            <div className="p-5 lg:min-h-0 lg:overflow-y-auto">
              {firstLastProfile ? (
                <AnchorRoleSelector
                  roles={requiredAnchorRoles}
                  anchors={currentAnchors}
                  selectedRole={selectedAnchorRole}
                  onSelect={setSelectedAnchorRole}
                  onOpen={(url, title) => setLargePreview({ url, title })}
                />
              ) : null}
              <CurrentImage
                detail={detail}
                anchor={managedAnchorProfile ? selectedAnchor : undefined}
                anchorRole={activeAnchorRole}
                onOpen={(url, title) => setLargePreview({ url, title })}
              />
              {storyboardSheetProfile ? (
                <StoryboardSheetPanel
                  data={storyboardSheet}
                  onOpen={(url, title) => setLargePreview({ url, title })}
                />
              ) : null}

              {detail.shot.imageErrorMessage ? (
                <div role="alert" className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {localizePlatformError(detail.shot.imageErrorMessage, detail.shot.imageErrorCode)}
                </div>
              ) : null}

              {detail.shot.imagePromptErrorMessage ? (
                <div role="alert" className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  图片提示词生成失败：{localizePlatformError(detail.shot.imagePromptErrorMessage, detail.shot.imagePromptErrorCode)}
                </div>
              ) : null}

              <div className="mt-4 flex flex-wrap gap-2">
                {managedAnchorProfile ? (
                  <Button
                    onClick={() => anchorGenerateMutation.mutate(activeAnchorRole)}
                    disabled={!referencesValid || selectedAnchor?.status === "generating" || anchorGenerateMutation.isPending || saveMutation.isPending}
                  >
                    {selectedAnchor?.status === "generating" || anchorGenerateMutation.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}
                    {selectedAnchor?.status === "generating"
                      ? "生成中"
                      : selectedAnchor?.artifactId
                        ? `重新生成${shotAnchorRoleLabel(activeAnchorRole)}`
                        : `生成${shotAnchorRoleLabel(activeAnchorRole)}`}
                  </Button>
                ) : (
                  <>
                    <Button onClick={() => generateMutation.mutate()} disabled={!canGenerate || imageRunning || promptRunning || generateMutation.isPending || saveMutation.isPending}>
                      {imageRunning || generateMutation.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                      {imageRunning ? "生成中" : detail.shot.imageArtifactId ? "重新生成" : "生成图片"}
                    </Button>
                    <Button variant="outline" onClick={() => promptMutation.mutate()} disabled={!referencesValid || imageRunning || promptRunning || promptMutation.isPending || generateMutation.isPending || saveMutation.isPending}>
                      {promptRunning || promptMutation.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}
                      {promptRunning ? "提示词生成中" : detail.shot.imagePrompt ? "重新生成提示词" : "生成提示词"}
                    </Button>
                  </>
                )}
                <Button variant="outline" onClick={() => saveMutation.mutate()} disabled={!referencesValid || promptRunning || saveMutation.isPending || generateMutation.isPending}>
                  {saveMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
                  保存设置
                </Button>
                <Button variant="outline" onClick={() => videoMutation.mutate()} disabled={!(managedAnchorProfile ? requiredAnchorsApproved : detail.shot.imageArtifactId) || videoMutation.isPending || detail.shot.videoStatus === "queued" || detail.shot.videoStatus === "running"}>
                  {videoMutation.isPending ? <Loader2 className="animate-spin" /> : <Video />}
                  生成视频
                </Button>
                {!managedAnchorProfile ? (
                  <Button variant="outline" onClick={() => unlinkMutation.mutate()} disabled={!detail.shot.imageArtifactId || unlinkMutation.isPending || imageRunning}>
                    {unlinkMutation.isPending ? <Loader2 className="animate-spin" /> : <Link2Off />}
                    解绑图片
                  </Button>
                ) : null}
              </div>

              <ImageGenerationHistory
                detail={detail}
                onOpen={(url, title) => setLargePreview({ url, title })}
              />

              <ShotContinuityPanel
                states={shotStates}
                transition={shotTransition}
                anchors={shotAnchors?.items ?? []}
                referencePack={referencePack}
                reviewPending={anchorReviewMutation.isPending}
                replanPending={stateReplanMutation.isPending}
                onReview={(anchor, decision) => anchorReviewMutation.mutate({ anchor, decision })}
                onReplan={() => stateReplanMutation.mutate()}
                onOpen={(url, title) => setLargePreview({ url, title })}
                profileKey={profileKey}
              />
            </div>

            <div className="border-t bg-muted/10 p-5 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0">
              <section className="space-y-2">
                <Label htmlFor="shot-image-prompt">{managedAnchorProfile ? `${shotAnchorRoleLabel(activeAnchorRole)}提示词` : "镜头图提示词"}</Label>
                <Textarea
                  id="shot-image-prompt"
                  className="min-h-52 resize-y leading-6"
                  value={managedAnchorProfile ? selectedAnchor?.prompt ?? "" : draft.imagePrompt}
                  readOnly={managedAnchorProfile}
                  onChange={(event) => setDraft((current) => ({ ...current, imagePrompt: event.target.value }))}
                />
              </section>

              <section className="mt-6 space-y-3 border-t pt-5">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="text-sm font-semibold">参考图策略</h3>
                    <p className="mt-1 text-xs text-muted-foreground">选择本镜头提交给图片模型的参考图。</p>
                  </div>
                  {draft.referenceMode === "custom" ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => setAssetPickerOpen((current) => !current)}
                      disabled={otherReferenceOptions.length === 0 && !assetPickerOpen}
                    >
                      <Plus />
                      添加其它资产
                    </Button>
                  ) : null}
                </div>
                <div className="grid grid-cols-3 gap-1 rounded-md bg-muted p-1">
                  <ModeButton active={draft.referenceMode === "auto"} onClick={() => setReferenceMode("auto")}>自动</ModeButton>
                  <ModeButton active={draft.referenceMode === "custom"} onClick={() => setReferenceMode("custom")} disabled={referenceOptions.length === 0}>手动选择</ModeButton>
                  <ModeButton active={draft.referenceMode === "none"} onClick={() => setReferenceMode("none")}>不使用</ModeButton>
                </div>

                {draft.referenceMode === "custom" && draft.referenceKeys.length === 0 ? (
                  <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700">至少选择一张参考图。</div>
                ) : null}
                {draft.referenceMode === "custom" && assetPickerOpen ? (
                  <div className="space-y-3 rounded-md border bg-background p-3">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-xs font-medium">项目资产当前主图</div>
                      <Button type="button" size="icon-xs" variant="ghost" onClick={() => setAssetPickerOpen(false)} aria-label="关闭资产选择">
                        <X />
                      </Button>
                    </div>
                    <div className="relative">
                      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                      <Input
                        className="pl-8"
                        value={assetSearch}
                        onChange={(event) => setAssetSearch(event.target.value)}
                        placeholder="搜索资产名称"
                      />
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {ASSET_TYPE_FILTERS.map((type) => (
                        <button
                          key={type}
                          type="button"
                          className={cn(
                            "rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted",
                            assetTypeFilter === type && "bg-muted text-foreground",
                          )}
                          onClick={() => setAssetTypeFilter(type)}
                        >
                          {type === "all" ? "全部" : assetTypeLabel(type)}
                        </button>
                      ))}
                    </div>
                    {filteredOtherReferenceOptions.length === 0 ? (
                      <div className="rounded-md border border-dashed px-3 py-5 text-center text-xs text-muted-foreground">
                        {otherReferenceOptions.length === 0 ? "其它可用资产已全部添加" : "没有匹配的资产"}
                      </div>
                    ) : (
                      <div className="grid max-h-64 gap-2 overflow-y-auto pr-1 sm:grid-cols-2">
                        {filteredOtherReferenceOptions.map((option) => (
                          <AdditionalAssetOptionCard
                            key={option.key}
                            option={option}
                            onAdd={() => toggleReference(option.key, true)}
                            onOpen={() => option.previewUrl && setLargePreview({ url: option.previewUrl, title: option.title })}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                ) : null}
                {visibleReferenceOptions.length === 0 ? (
                  <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">当前镜头没有可用参考图</div>
                ) : (
                  <div className="grid gap-2 sm:grid-cols-2">
                    {visibleReferenceOptions.map((option) => (
                      <ReferenceOptionCard
                        key={option.key}
                        option={option}
                        checked={selectedReferenceKeys.has(option.key)}
                        disabled={draft.referenceMode !== "custom"}
                        onCheckedChange={(checked) => toggleReference(option.key, checked)}
                        onOpen={() => option.previewUrl && setLargePreview({ url: option.previewUrl, title: option.title })}
                      />
                    ))}
                  </div>
                )}
              </section>

              <section className="mt-6 border-t pt-5">
                <details className="rounded-md border bg-background px-3 py-2 text-xs">
                  <summary className="cursor-pointer font-medium text-muted-foreground">镜头信息</summary>
                  <div className="mt-3 grid gap-2 leading-5">
                    <DetailRow label="画面" value={detail.shot.visual} />
                    <DetailRow label="机位" value={detail.shot.camera} />
                    <DetailRow label="运镜" value={detail.shot.motion} />
                    <DetailRow label="情绪" value={detail.shot.mood} />
                    <DetailRow label="时长" value={detail.shot.durationSeconds ? `${wholeSecondDuration(detail.shot.durationSeconds)} 秒` : ""} />
                    {detail.shot.scriptDialogue.length > 0 ? (
                      <div className="grid gap-1 sm:grid-cols-[56px_1fr]">
                        <span className="text-muted-foreground">台词</span>
                        <div>{detail.shot.scriptDialogue.map((line, index) => <div key={`${line.speaker}-${index}`}>{line.speaker ? `${line.speaker}：` : ""}{line.text}</div>)}</div>
                      </div>
                    ) : null}
                  </div>
                </details>
              </section>
            </div>
          </div>
        )}

        {largePreview ? (
          <div className="absolute inset-0 z-20 grid place-items-center bg-black/90 p-6" onClick={() => setLargePreview(null)}>
            <button type="button" className="absolute right-4 top-4 z-10 grid size-10 place-items-center rounded-full bg-white/10 text-white hover:bg-white/20" onClick={(event) => { event.stopPropagation(); setLargePreview(null); }} aria-label="关闭大图">
              <X />
            </button>
            <div className="relative h-full w-full" onClick={(event) => event.stopPropagation()}>
              <NextImage src={largePreview.url} alt={largePreview.title} fill unoptimized sizes="100vw" className="object-contain" />
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function AnchorRoleSelector({
  roles,
  anchors,
  selectedRole,
  onSelect,
  onOpen,
}: {
  roles: PlannedAnchorRole[];
  anchors: ShotVisualAnchor[];
  selectedRole: PlannedAnchorRole;
  onSelect: (role: PlannedAnchorRole) => void;
  onOpen: (url: string, title: string) => void;
}) {
  return (
    <div className="mb-4 grid gap-2 sm:grid-cols-2">
      {roles.map((role) => {
        const anchor = latestAnchorForRole(anchors, role);
        const selected = selectedRole === role;
        return (
          <div key={role} className={cn("grid grid-cols-[72px_1fr] gap-3 rounded-md border p-2 transition-colors", selected && "border-primary bg-primary/[0.03]")}>
            <button
              type="button"
              className="relative h-14 overflow-hidden rounded bg-muted"
              disabled={!anchor?.previewUrl}
              onClick={() => anchor?.previewUrl && onOpen(anchor.previewUrl, shotAnchorRoleLabel(role))}
            >
              {anchor?.previewUrl
                ? <NextImage src={anchor.previewUrl} alt={shotAnchorRoleLabel(role)} fill unoptimized sizes="72px" className="object-cover" />
                : <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>}
            </button>
            <button type="button" className="min-w-0 text-left" onClick={() => onSelect(role)}>
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="text-sm font-medium">{shotAnchorRoleLabel(role)}</span>
                {anchor ? <Badge variant="outline">{statusLabel(anchor.status)}</Badge> : <Badge variant="secondary">待生成</Badge>}
              </span>
              <span className="mt-1 block text-xs text-muted-foreground">
                {anchor ? `版本 ${anchor.revision} · ${statusLabel(anchor.reviewStatus)}` : "未生成"}
              </span>
            </button>
          </div>
        );
      })}
    </div>
  );
}

function CurrentImage({ detail, anchor, anchorRole, onOpen }: { detail: StoryboardShotDetail; anchor?: ShotVisualAnchor; anchorRole: PlannedAnchorRole; onOpen: (url: string, title: string) => void }) {
  const previewUrl = anchor?.previewUrl || (anchorRole === "planned_first_frame" ? detail.imagePreviewUrl || detail.shot.imagePreviewUrl : undefined);
  const title = anchor ? shotAnchorRoleLabel(anchor.anchorRole) : detail.shot.title || "分镜图片";
  return (
    <div className="group relative overflow-hidden rounded-md bg-muted" style={{ aspectRatio: cssAspectRatio(detail.aspectRatio) }}>
      {previewUrl ? (
        <button type="button" className="relative h-full w-full" onClick={() => onOpen(previewUrl, title)}>
          <NextImage src={previewUrl} alt={title} fill unoptimized sizes="(max-width: 1024px) 100vw, 760px" className="object-contain" />
          <span className="absolute bottom-3 right-3 grid size-9 place-items-center rounded-full bg-black/65 text-white opacity-0 transition-opacity group-hover:opacity-100"><Maximize2 className="size-4" /></span>
        </button>
      ) : (
        <div className="grid h-full place-items-center"><ImageIcon className="size-10 text-muted-foreground/50" /></div>
      )}
    </div>
  );
}

function StoryboardSheetPanel({ data, onOpen }: { data?: StoryboardSheetResponse; onOpen: (url: string, title: string) => void }) {
  const manifest = data?.manifest;
  if (!manifest) {
    return <div className="mt-4 rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">尚未生成分镜板</div>;
  }
  return (
    <section className="mt-5 border-t pt-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-sm font-semibold">分镜板画格</h3>
          <Badge variant="outline">{manifest.gridRows} × {manifest.gridColumns}</Badge>
          <Badge variant={manifest.reviewStatus === "approved" ? "default" : "secondary"}>{statusLabel(manifest.reviewStatus)}</Badge>
        </div>
        {manifest.sheetPreviewUrl ? (
          <Button type="button" size="sm" variant="outline" onClick={() => onOpen(manifest.sheetPreviewUrl!, "完整分镜板")}>
            <Maximize2 />查看整板
          </Button>
        ) : null}
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
        {(data?.panels ?? []).map((panel) => (
          <div key={panel.id} className="overflow-hidden rounded-md border bg-background">
            <button
              type="button"
              className="relative block w-full bg-muted"
              style={{ aspectRatio: cssAspectRatio(manifest.videoAspectRatio) }}
              disabled={!panel.previewUrl}
              onClick={() => panel.previewUrl && onOpen(panel.previewUrl, `画格 ${panel.ordinal}`)}
            >
              {panel.previewUrl
                ? <NextImage src={panel.previewUrl} alt={`分镜板画格 ${panel.ordinal}`} fill unoptimized sizes="(max-width: 640px) 50vw, 220px" className="object-cover" />
                : <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>}
            </button>
            <div className="px-2 py-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium">画格 {panel.ordinal}</span>
                <Badge variant={panel.reviewStatus === "approved" ? "default" : "outline"}>{statusLabel(panel.reviewStatus)}</Badge>
              </div>
              <div className="mt-1 line-clamp-2 text-[11px] leading-4 text-muted-foreground">
                {storyboardPanelStageLabel(panel.stage)} · {Math.round(panel.timeTick / Math.max(manifest.timelineTimebase, 1))} 秒
                {panel.actionStage ? ` · ${panel.actionStage}` : ""}
              </div>
            </div>
          </div>
        ))}
      </div>
      <details className="mt-3 text-xs text-muted-foreground">
        <summary className="cursor-pointer">版本与审核溯源</summary>
        <div className="mt-2 grid gap-1 break-all leading-5">
          <span>Manifest 版本 {manifest.revision} · {shortHash(manifest.manifestHash)}</span>
          <span>契约 {manifest.contractVersion} · 历史 {data?.history.length ?? 0}</span>
          {manifest.reviewerProviderCallId ? <span>审核调用 {manifest.reviewerProviderCallId}</span> : null}
        </div>
      </details>
    </section>
  );
}

function storyboardPanelStageLabel(stage: string) {
  switch (stage) {
    case "entry": return "起始";
    case "early": return "前段";
    case "middle": return "中段";
    case "late": return "后段";
    case "exit": return "结束";
    default: return stage;
  }
}

function ReferenceOptionCard({ option, checked, disabled, onCheckedChange, onOpen }: { option: StoryboardShotImageReferenceOption; checked: boolean; disabled: boolean; onCheckedChange: (checked: boolean) => void; onOpen: () => void }) {
  return (
    <div className={cn("grid grid-cols-[76px_1fr] gap-3 rounded-md border bg-background p-2", checked && "border-primary/60 bg-primary/[0.03]")}>
      <button type="button" className="relative h-16 overflow-hidden rounded bg-muted" onClick={onOpen} disabled={!option.previewUrl}>
        {option.previewUrl ? <NextImage src={option.previewUrl} alt={option.title} fill unoptimized sizes="76px" className="object-cover" /> : <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>}
      </button>
      <label className={cn("flex min-w-0 cursor-pointer items-start gap-2", disabled && "cursor-default")}>
        <Checkbox checked={checked} disabled={disabled} onCheckedChange={(value) => onCheckedChange(value === true)} />
        <span className="min-w-0">
          <span className="block truncate text-xs font-medium">{option.assetName || option.title}</span>
          <span className="mt-1 block text-[11px] text-muted-foreground">{assetTypeLabel(option.assetType)} · {referenceSourceLabel(option.sourceType)}</span>
          {option.autoSelected ? <span className="mt-1 inline-flex items-center gap-1 text-[11px] text-primary"><Check className="size-3" />自动采用</span> : null}
        </span>
      </label>
    </div>
  );
}

function AdditionalAssetOptionCard({ option, onAdd, onOpen }: { option: StoryboardShotImageReferenceOption; onAdd: () => void; onOpen: () => void }) {
  return (
    <div className="grid grid-cols-[64px_1fr_auto] items-center gap-2 rounded-md border p-2">
      <button type="button" className="relative h-14 overflow-hidden rounded bg-muted" onClick={onOpen} disabled={!option.previewUrl}>
        {option.previewUrl ? <NextImage src={option.previewUrl} alt={option.title} fill unoptimized sizes="64px" className="object-cover" /> : <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>}
      </button>
      <div className="min-w-0">
        <div className="truncate text-xs font-medium">{option.assetName}</div>
        <div className="mt-1 text-[11px] text-muted-foreground">{assetTypeLabel(option.assetType)}</div>
      </div>
      <Button type="button" size="icon-sm" variant="ghost" onClick={onAdd} aria-label={`添加 ${option.assetName}`}>
        <Plus />
      </Button>
    </div>
  );
}

function ImageGenerationHistory({ detail, onOpen }: { detail: StoryboardShotDetail; onOpen: (url: string, title: string) => void }) {
  return (
    <section className="mt-6 border-t pt-5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">生成记录</h3>
        <Badge variant="secondary">{detail.imageGenerationRuns.length}</Badge>
      </div>
      {detail.imageGenerationRuns.length === 0 ? <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无生成记录</div> : (
        <div className="grid gap-2">
          {detail.imageGenerationRuns.map((run, index) => (
            <div key={run.providerCallId} className="grid grid-cols-[88px_1fr] gap-3 rounded-md border p-2">
              <button type="button" className="relative h-20 overflow-hidden rounded bg-muted" disabled={!run.previewUrl} onClick={() => run.previewUrl && onOpen(run.previewUrl, `生成版本 ${detail.imageGenerationRuns.length - index}`)}>
                {run.previewUrl ? <NextImage src={run.previewUrl} alt="分镜生成版本" fill unoptimized sizes="88px" className="object-cover" /> : <span className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/50" /></span>}
              </button>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={run.status === "succeeded" ? "default" : run.status === "failed" ? "destructive" : "outline"}>{statusLabel(run.status)}</Badge>
                  <span className="truncate text-xs text-muted-foreground">{run.modelName || run.modelId || "未记录模型"}</span>
                </div>
                <div className="mt-2 text-xs text-muted-foreground">{formatRunTime(run.completedAt || run.startedAt)}</div>
                {run.errorMessage ? <p className="mt-2 line-clamp-2 text-xs text-destructive">{localizePlatformError(run.errorMessage, run.errorCode)}</p> : null}
                {run.prompt ? <details className="mt-2 text-xs"><summary className="cursor-pointer text-muted-foreground">查看提示词与溯源</summary><pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-muted/50 p-2 text-foreground">{run.prompt}{run.promptTruncated ? "\n\n[内容过长，详情中已截断]" : ""}</pre><div className="mt-2 break-all text-muted-foreground">调用：{run.providerCallId}{run.promptHash ? `\n哈希：${run.promptHash}` : ""}</div></details> : null}
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function ShotContinuityPanel({
  states,
  transition,
  anchors,
  referencePack,
  reviewPending,
  replanPending,
  onReview,
  onReplan,
  onOpen,
  profileKey,
}: {
  states?: StoryboardShotStateResponse;
  transition?: StoryboardShotTransitionResponse;
  anchors: ShotVisualAnchor[];
  referencePack?: ShotReferencePackResponse;
  reviewPending: boolean;
  replanPending: boolean;
  onReview: (anchor: ShotVisualAnchor, decision: "approve" | "reject") => void;
  onReplan: () => void;
  onOpen: (url: string, title: string) => void;
  profileKey: string;
}) {
  const currentStates = (states?.items ?? []).filter((item, index, all) => all.findIndex((candidate) => candidate.stateRole === item.stateRole) === index);
  const plannedRoles: PlannedAnchorRole[] = profileKey === "first_last_frame"
    ? ["planned_first_frame", "planned_last_frame"]
    : profileKey === "storyboard_sheet"
      ? ["storyboard_sheet"]
      : ["planned_first_frame"];
  const currentAnchors = plannedRoles.map((role) => latestAnchorForRole(anchors, role)).filter((anchor): anchor is ShotVisualAnchor => !!anchor);
  const pairApproved = profileKey === "first_last_frame" && plannedRoles.every((role) => {
    const anchor = latestAnchorForRole(anchors, role);
    return anchor?.status === "ready" && anchor.reviewStatus === "approved";
  });
  return (
    <section className="mt-6 border-t pt-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><GitBranch className="size-4" />镜头连续性</h3>
        <Button type="button" size="sm" variant="outline" onClick={onReplan} disabled={replanPending}>
          {replanPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          重新规划本集
        </Button>
      </div>

      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        {currentStates.map((state) => (
          <div key={state.id} className="rounded-md border px-3 py-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium">{shotStateRoleLabel(state.stateRole)}</span>
              <Badge variant="outline">{statusLabel(state.status)}</Badge>
            </div>
            <div className="mt-2 text-[11px] text-muted-foreground">版本 {state.revision} · {shortHash(state.stateHash)}</div>
          </div>
        ))}
      </div>

      {transition?.active ? (
        <div className="mt-3 rounded-md border px-3 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{shotTransitionTypeLabel(transition.active.transitionType)}</Badge>
            <Badge variant="outline">{statusLabel(transition.active.reviewStatus)}</Badge>
            <span className="text-xs text-muted-foreground">置信度 {Math.round(transition.active.confidence * 100)}%</span>
          </div>
          <details className="mt-2 text-xs">
            <summary className="cursor-pointer text-muted-foreground">转场约束与历史</summary>
            <div className="mt-2 grid gap-1 leading-5">
              <span>保留：{transition.active.carryConstraints.join("、") || "无"}</span>
              <span>重置：{transition.active.resetConstraints.join("、") || "无"}</span>
              <span>版本：{transition.active.revision} / 历史 {transition.items.length}</span>
            </div>
          </details>
        </div>
      ) : null}

      {currentAnchors.length > 0 ? (
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {currentAnchors.map((anchor) => (
            <div key={anchor.id} className="grid gap-3 rounded-md border p-3 sm:grid-cols-[96px_1fr]">
              <button
                type="button"
                className="relative h-20 overflow-hidden rounded bg-muted"
                disabled={!anchor.previewUrl}
                onClick={() => anchor.previewUrl && onOpen(anchor.previewUrl, shotAnchorRoleLabel(anchor.anchorRole))}
              >
                {anchor.previewUrl
                  ? <NextImage src={anchor.previewUrl} alt={shotAnchorRoleLabel(anchor.anchorRole)} fill unoptimized sizes="96px" className="object-cover" />
                  : <span className="grid h-full place-items-center"><ImageIcon className="size-6 text-muted-foreground/50" /></span>}
              </button>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">{shotAnchorRoleLabel(anchor.anchorRole)}</span>
                  <Badge variant="outline">{statusLabel(anchor.status)}</Badge>
                  <Badge variant={anchor.reviewStatus === "approved" ? "default" : "secondary"}>{statusLabel(anchor.reviewStatus)}</Badge>
                </div>
                <div className="mt-2 text-xs text-muted-foreground">版本 {anchor.revision}{anchor.promptHash ? ` · ${shortHash(anchor.promptHash)}` : ""}</div>
                {profileKey !== "first_last_frame" && profileKey !== "storyboard_sheet" && anchor.status === "ready" && anchor.reviewStatus !== "approved" ? (
                  <div className="mt-3 flex gap-2">
                    <Button type="button" size="sm" onClick={() => onReview(anchor, "approve")} disabled={reviewPending}>
                      {reviewPending ? <Loader2 className="animate-spin" /> : <Check />}
                      确认锚点
                    </Button>
                    <Button type="button" size="sm" variant="outline" onClick={() => onReview(anchor, "reject")} disabled={reviewPending}>
                      <X />拒绝
                    </Button>
                  </div>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      ) : null}
      {profileKey === "first_last_frame" ? (
        <div className={cn("mt-3 rounded-md border px-3 py-2 text-xs", pairApproved ? "border-emerald-500/30 bg-emerald-500/5 text-emerald-700" : "text-muted-foreground")}>
          {pairApproved ? "首尾帧契约审核已通过" : "首帧和尾帧生成完成后将自动进行一致性与动作可达性审核"}
        </div>
      ) : null}

      {referencePack?.pack ? (
        <details className="mt-3 rounded-md border px-3 py-2">
          <summary className="flex cursor-pointer list-none items-center justify-between gap-2 text-xs font-medium">
            <span className="flex items-center gap-2"><Layers3 className="size-4" />引用包</span>
            <span className="text-muted-foreground">{referencePack.items.length} 项 · {statusLabel(referencePack.pack.status)}</span>
          </summary>
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            {referencePack.items.map((reference) => (
              <div key={reference.id} className="grid grid-cols-[52px_1fr] gap-2 rounded border p-2">
                <button type="button" className="relative h-12 overflow-hidden rounded bg-muted" disabled={!reference.previewUrl} onClick={() => reference.previewUrl && onOpen(reference.previewUrl, shotReferenceRoleLabel(reference.role))}>
                  {reference.previewUrl ? <NextImage src={reference.previewUrl} alt={shotReferenceRoleLabel(reference.role)} fill unoptimized sizes="52px" className="object-cover" /> : <span className="grid h-full place-items-center"><ImageIcon className="size-4 text-muted-foreground/50" /></span>}
                </button>
                <div className="min-w-0">
                  <div className="truncate text-xs font-medium">{shotReferenceRoleLabel(reference.role)}</div>
                  <div className="mt-1 truncate text-[11px] text-muted-foreground">{reference.required ? "必需" : "可选"} · {modalityLabel(reference.mediaType)} · {shotReferenceSemanticsLabel(reference.semantics)} · {shortHash(reference.contentHash)}</div>
                </div>
              </div>
            ))}
          </div>
        </details>
      ) : null}
    </section>
  );
}

function shortHash(value: string) {
  const normalized = value.replace(/^sha256:/, "");
  return normalized.length > 12 ? `${normalized.slice(0, 12)}…` : normalized;
}

function latestAnchorForRole(anchors: ShotVisualAnchor[], role: PlannedAnchorRole) {
  return anchors
    .filter((anchor) => anchor.anchorRole === role)
    .sort((left, right) => {
      const leftActive = left.status === "archived" || left.status === "stale" ? 0 : 1;
      const rightActive = right.status === "archived" || right.status === "stale" ? 0 : 1;
      return rightActive - leftActive || right.revision - left.revision;
    })[0];
}

function ModeButton({ active, disabled, onClick, children }: { active: boolean; disabled?: boolean; onClick: () => void; children: ReactNode }) {
  return <button type="button" className={cn("rounded px-2 py-1.5 text-xs font-medium text-muted-foreground transition-colors", active && "bg-background text-foreground shadow-sm", disabled && "cursor-not-allowed opacity-40")} disabled={disabled} onClick={onClick}>{children}</button>;
}

function DetailRow({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return <div className="grid gap-1 sm:grid-cols-[56px_1fr]"><span className="text-muted-foreground">{label}</span><span>{value}</span></div>;
}

function draftFromDetail(detail: StoryboardShotDetail): ImageDraft {
  const availableKeys = new Set(detail.imageReferenceOptions.map((option) => option.key));
  return {
    shotId: detail.shot.id,
    promptRevision: imagePromptRevision(detail),
    imagePrompt: detail.shot.imagePrompt ?? "",
    referenceMode: normalizeReferenceMode(detail.shot.imageReferenceMode),
    referenceKeys: (detail.shot.imageReferenceKeys ?? []).filter((key) => availableKeys.has(key)),
  };
}

function imagePromptRevision(detail: StoryboardShotDetail) {
  return detail.shot.imagePromptUpdatedAt ?? `${detail.shot.imagePromptStatus}:${detail.shot.imagePrompt ?? ""}`;
}

function imageUpdateBody(draft: ImageDraft) {
  return {
    imagePrompt: draft.imagePrompt,
    imageReferenceMode: draft.referenceMode,
    imageReferenceKeys: draft.referenceMode === "custom" ? draft.referenceKeys : [],
  };
}

function imageReferenceUpdateBody(draft: ImageDraft) {
  return {
    imageReferenceMode: draft.referenceMode,
    imageReferenceKeys: draft.referenceMode === "custom" ? draft.referenceKeys : [],
  };
}

function normalizeReferenceMode(value?: string): ReferenceMode {
  return value === "custom" || value === "none" ? value : "auto";
}

function referenceSourceLabel(sourceType: string) {
  if (sourceType === "derived_asset") return "镜头衍生图";
  if (sourceType === "asset_primary") return "资产当前主图";
  return "资产参考图";
}

function formatRunTime(value?: string) {
  if (!value) return "未记录时间";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}
