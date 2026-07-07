"use client";

import { useMemo, useState } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ArrowDown, ArrowUp, Film, Image as ImageIcon, RefreshCw, Save, SkipForward, Trash2, Video } from "lucide-react";
import { toast } from "sonner";
import { assetTypeLabel, requirementTypeLabel, statusLabel } from "@/lib/labels";
import type { ShotProductionShot, StoryboardShot, StoryboardShotDetail, StoryboardShotRequirementDetail } from "@/lib/types";

type ShotRow = {
  id: string;
  workflowRunId: string;
  shotNo: number;
  shotIndex: number;
  durationSeconds?: number;
  visual?: string;
  camera?: string;
  motion?: string;
  mood?: string;
  imagePrompt?: string;
  videoPrompt?: string;
  imageArtifactId?: string;
  videoArtifactId?: string;
  imagePreviewUrl?: string;
  videoPreviewUrl?: string;
  imageStatus?: string;
  videoStatus?: string;
  imageErrorMessage?: string;
  videoErrorMessage?: string;
  staleState?: string;
  canGenerateImage?: boolean;
  canGenerateVideo?: boolean;
  canRetryImage?: boolean;
  canRetryVideo?: boolean;
};

type ShotDraft = {
  durationSeconds: string;
  visual: string;
  camera: string;
  motion: string;
  mood: string;
  imagePrompt: string;
  videoPrompt: string;
};

type RequirementDraft = {
  costume: string;
  pose: string;
  expression: string;
  action: string;
  cameraRelation: string;
  sceneState: string;
  propState: string;
  prompt: string;
};

type DraftEntry<TDraft> = {
  key: string;
  draft: TDraft;
};

const EMPTY_SHOT_DRAFT: ShotDraft = {
  durationSeconds: "",
  visual: "",
  camera: "",
  motion: "",
  mood: "",
  imagePrompt: "",
  videoPrompt: "",
};

export function StoryboardPage({
  projectId,
  initialShotId = "",
}: {
  projectId: string;
  initialShotId?: string;
  initialRequirementId?: string;
}) {
  const invalidate = useInvalidateKeys();
  const [selectedShotId, setSelectedShotId] = useState(initialShotId);
  const [shotDraftEntry, setShotDraftEntry] = useState<DraftEntry<ShotDraft> | null>(null);
  const [requirementDrafts, setRequirementDrafts] = useState<Record<string, DraftEntry<RequirementDraft>>>({});
  const [shotToDelete, setShotToDelete] = useState<ShotRow | null>(null);

  const { data: workflowRuns = [] } = useApiQuery({
    key: qk.workflowRuns(projectId),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId).then((response) => response.items || []),
  });
  const latestRunId = workflowRuns[0]?.id ?? "";
  const { data: workflowShots = [], isLoading: workflowShotsLoading } = useApiQuery({
    key: ["workflow-shots", latestRunId],
    queryFn: (session) => studioApi.listWorkflowShots(session, latestRunId).then((response) => response.items || []),
    enabled: !!latestRunId,
  });
  const { data: shotProduction, isLoading: productionLoading } = useApiQuery({
    key: qk.shotProduction(projectId),
    queryFn: (session) =>
      studioApi.getShotProductionStatus(session, projectId, { includePreviewUrl: true, previewExpiresSeconds: 900 }),
  });

  const rows = useMemo(() => mergeShotRows(shotProduction?.shots ?? [], workflowShots), [shotProduction?.shots, workflowShots]);
  const selectedRow = rows.find((shot) => shot.id === selectedShotId) ?? rows[0] ?? null;
  const selectedId = selectedRow?.id ?? "";
  const { data: shotDetail, isLoading: detailLoading } = useApiQuery({
    key: qk.shotDetail(projectId, selectedId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotDetail(session, projectId, selectedId),
    enabled: !!selectedId,
  });

  const selectedShot = shotDetail?.shot ?? selectedRow;
  const selectedShotDraftKey = selectedShot ? shotDraftKey(selectedShot) : "";
  const shotDraft =
    selectedShot && shotDraftEntry?.key === selectedShotDraftKey ? shotDraftEntry.draft : selectedShot ? draftFromShot(selectedShot) : EMPTY_SHOT_DRAFT;

  const productionMutation = useApiMutation({
    mutationFn: (session, payload: { action: string; shotId: string }) =>
      studioApi.runShotProductionAction(session, projectId, {
        action: payload.action,
        shotIds: [payload.shotId],
        options: { maxConcurrency: 1 },
      }),
    onSuccess: () => {
      toast.success("任务已启动");
      refreshStoryboard(projectId, latestRunId, selectedId, invalidate);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const updateShotMutation = useApiMutation({
    mutationFn: (session, payload: { shotId: string; draft: ShotDraft }) =>
      studioApi.updateStoryboardShot(session, projectId, payload.shotId, shotUpdateBody(payload.draft)),
    onSuccess: (_data, payload) => {
      toast.success("镜头已保存");
      setShotDraftEntry(null);
      refreshStoryboard(projectId, latestRunId, payload.shotId, invalidate);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const reorderMutation = useApiMutation({
    mutationFn: (session, payload: { shotId: string; direction: "up" | "down" }) => {
      const currentIndex = rows.findIndex((shot) => shot.id === payload.shotId);
      const targetIndex = payload.direction === "up" ? currentIndex - 1 : currentIndex + 1;
      if (currentIndex < 0 || targetIndex < 0 || targetIndex >= rows.length) {
        return Promise.resolve({ items: [] });
      }
      const nextRows = [...rows];
      [nextRows[currentIndex], nextRows[targetIndex]] = [nextRows[targetIndex], nextRows[currentIndex]];
      return studioApi.reorderStoryboardShots(session, projectId, {
        items: nextRows.map((shot, index) => ({ shotId: shot.id, shotIndex: index, shotNo: index + 1 })),
      });
    },
    onSuccess: () => {
      toast.success("镜头顺序已更新");
      refreshStoryboard(projectId, latestRunId, selectedId, invalidate);
    },
    onError: (error) => toast.error("重排失败：" + error.message),
  });

  const deleteShotMutation = useApiMutation({
    mutationFn: (session, shotId: string) => studioApi.deleteStoryboardShot(session, projectId, shotId),
    onSuccess: () => {
      toast.success("镜头已删除");
      setShotToDelete(null);
      setSelectedShotId("");
      refreshStoryboard(projectId, latestRunId, "", invalidate);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const updateRequirementMutation = useApiMutation({
    mutationFn: (session, payload: { requirementId: string; draft: RequirementDraft }) =>
      studioApi.updateShotAssetRequirement(session, projectId, payload.requirementId, requirementUpdateBody(payload.draft)),
    onSuccess: (_data, payload) => {
      toast.success("资产需求已保存");
      setRequirementDrafts((drafts) => {
        const next = { ...drafts };
        delete next[payload.requirementId];
        return next;
      });
      refreshStoryboard(projectId, latestRunId, selectedId, invalidate);
      invalidate([qk.requirements(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const skipRequirementMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.skipShotAssetRequirement(session, projectId, requirementId),
    onSuccess: () => {
      toast.success("资产需求已跳过");
      refreshStoryboard(projectId, latestRunId, selectedId, invalidate);
      invalidate([qk.requirements(projectId)]);
    },
    onError: (error) => toast.error("跳过失败：" + error.message),
  });

  const generateDerivedMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.generateDerivedAssetImage(session, projectId, requirementId),
    onSuccess: () => {
      toast.success("衍生资产图任务已完成");
      refreshStoryboard(projectId, latestRunId, selectedId, invalidate);
      invalidate([qk.requirements(projectId), qk.assets(projectId), qk.artifacts(projectId)]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const loading = workflowShotsLoading || productionLoading;
  const selectedIndex = selectedRow ? rows.findIndex((shot) => shot.id === selectedRow.id) : -1;
  const deleteImpactItems = shotToDelete ? buildDeleteImpactItems(shotToDelete, shotDetail?.shot.id === shotToDelete.id ? shotDetail : undefined) : [];

  function setShotDraftField(field: keyof ShotDraft, value: string) {
    if (!selectedShot) {
      return;
    }
    const key = shotDraftKey(selectedShot);
    const base = shotDraftEntry?.key === key ? shotDraftEntry.draft : draftFromShot(selectedShot);
    setShotDraftEntry({ key, draft: { ...base, [field]: value } });
  }

  function requirementDraft(requirement: StoryboardShotRequirementDetail) {
    const key = requirementDraftKey(requirement);
    return requirementDrafts[requirement.id]?.key === key ? requirementDrafts[requirement.id].draft : draftFromRequirement(requirement);
  }

  function setRequirementDraftField(requirement: StoryboardShotRequirementDetail, field: keyof RequirementDraft, value: string) {
    const key = requirementDraftKey(requirement);
    const base = requirementDrafts[requirement.id]?.key === key ? requirementDrafts[requirement.id].draft : draftFromRequirement(requirement);
    setRequirementDrafts((drafts) => ({
      ...drafts,
      [requirement.id]: { key, draft: { ...base, [field]: value } },
    }));
  }

  return (
    <Surface>
      <SectionTitle title="分镜" description="查看和调整镜头、镜头资产需求、镜头图片和镜头视频" />
      <div className="grid gap-4 p-4 xl:grid-cols-[320px_1fr]">
        <div className="flex flex-col gap-3">
          {loading && <Skeleton className="h-64" />}
          {!loading && rows.length === 0 && (
            <div className="grid min-h-64 place-items-center rounded-lg border border-dashed p-8 text-center">
              <div>
                <Film className="mx-auto text-muted-foreground" />
                <p className="mt-3 text-sm text-muted-foreground">暂无分镜</p>
              </div>
            </div>
          )}
          {rows.map((shot, index) => (
            <button
              key={shot.id}
              type="button"
              className={`rounded-lg border p-3 text-left transition hover:bg-muted/60 ${selectedRow?.id === shot.id ? "border-primary bg-muted/70" : "bg-background"}`}
              onClick={() => {
                setSelectedShotId(shot.id);
                setShotDraftEntry(null);
              }}
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="font-medium">镜头 {shot.shotNo || index + 1}</div>
                  <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{shot.visual || "未填写画面描述"}</div>
                </div>
                {shot.staleState && shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(shot.staleState)}</Badge> : null}
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge variant="outline">图像 {statusLabel(shot.imageStatus || "pending")}</Badge>
                <Badge variant="outline">视频 {statusLabel(shot.videoStatus || "pending")}</Badge>
              </div>
            </button>
          ))}
        </div>

        <div className="min-w-0">
          {!selectedShot && !loading ? (
            <div className="grid min-h-96 place-items-center rounded-lg border border-dashed p-8 text-center">
              <div>
                <Film className="mx-auto text-muted-foreground" />
                <p className="mt-3 text-sm text-muted-foreground">选择一个镜头查看详情</p>
              </div>
            </div>
          ) : null}

          {selectedShot ? (
            <div className="grid gap-4">
              <div className="rounded-lg border bg-background">
                <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
                  <div>
                    <h3 className="text-lg font-semibold">镜头 {selectedShot.shotNo || selectedIndex + 1}</h3>
                    <p className="mt-1 text-xs text-muted-foreground">{selectedShot.workflowRunId}</p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Badge variant="outline">图像 {statusLabel(selectedShot.imageStatus || selectedRow?.imageStatus || "pending")}</Badge>
                    <Badge variant="outline">视频 {statusLabel(selectedShot.videoStatus || selectedRow?.videoStatus || "pending")}</Badge>
                    {selectedShot.staleState && selectedShot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(selectedShot.staleState)}</Badge> : null}
                  </div>
                </div>

                <div className="grid gap-4 p-4 lg:grid-cols-[minmax(260px,380px)_1fr]">
                  <div className="grid content-start gap-3">
                    <ShotMediaPreview kind="image" previewUrl={shotDetail?.imagePreviewUrl || selectedRow?.imagePreviewUrl} />
                    <ShotMediaPreview kind="video" previewUrl={shotDetail?.videoPreviewUrl || selectedRow?.videoPreviewUrl} />
                    <div className="flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          productionMutation.mutate({
                            action: selectedRow?.canRetryImage ? "regenerate_failed_images" : "generate_selected_images",
                            shotId: selectedShot.id,
                          })
                        }
                        disabled={productionMutation.isPending || (!selectedRow?.canGenerateImage && !selectedRow?.canRetryImage)}
                      >
                        <ImageIcon data-icon="inline-start" />
                        生成图片
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          productionMutation.mutate({
                            action: selectedRow?.canRetryVideo ? "regenerate_failed_videos" : "generate_selected_videos",
                            shotId: selectedShot.id,
                          })
                        }
                        disabled={productionMutation.isPending || (!selectedRow?.canGenerateVideo && !selectedRow?.canRetryVideo)}
                      >
                        <Video data-icon="inline-start" />
                        生成视频
                      </Button>
                    </div>
                    {selectedRow?.imageErrorMessage ? <p className="text-xs text-destructive">图片错误：{selectedRow.imageErrorMessage}</p> : null}
                    {selectedRow?.videoErrorMessage ? <p className="text-xs text-destructive">视频错误：{selectedRow.videoErrorMessage}</p> : null}
                  </div>

                  <div className="grid gap-4">
                    <div className="grid gap-3 md:grid-cols-3">
                      <FieldText label="时长（秒）" value={shotDraft.durationSeconds} onChange={(value) => setShotDraftField("durationSeconds", value)} />
                      <FieldText label="景别/机位" value={shotDraft.camera} onChange={(value) => setShotDraftField("camera", value)} />
                      <FieldText label="情绪" value={shotDraft.mood} onChange={(value) => setShotDraftField("mood", value)} />
                    </div>
                    <FieldTextarea label="画面描述" value={shotDraft.visual} onChange={(value) => setShotDraftField("visual", value)} rows={4} />
                    <FieldTextarea label="动作与运镜" value={shotDraft.motion} onChange={(value) => setShotDraftField("motion", value)} rows={3} />
                    <FieldTextarea label="镜头图提示词" value={shotDraft.imagePrompt} onChange={(value) => setShotDraftField("imagePrompt", value)} rows={3} />
                    <FieldTextarea label="镜头视频提示词" value={shotDraft.videoPrompt} onChange={(value) => setShotDraftField("videoPrompt", value)} rows={3} />
                    <div className="flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        onClick={() => updateShotMutation.mutate({ shotId: selectedShot.id, draft: shotDraft })}
                        disabled={updateShotMutation.isPending}
                      >
                        <Save data-icon="inline-start" />
                        保存镜头
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => reorderMutation.mutate({ shotId: selectedShot.id, direction: "up" })}
                        disabled={reorderMutation.isPending || selectedIndex <= 0}
                      >
                        <ArrowUp data-icon="inline-start" />
                        上移
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => reorderMutation.mutate({ shotId: selectedShot.id, direction: "down" })}
                        disabled={reorderMutation.isPending || selectedIndex < 0 || selectedIndex >= rows.length - 1}
                      >
                        <ArrowDown data-icon="inline-start" />
                        下移
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => setShotToDelete(selectedRow ?? rowFromShot(selectedShot))} disabled={deleteShotMutation.isPending}>
                        <Trash2 data-icon="inline-start" />
                        删除镜头
                      </Button>
                    </div>
                  </div>
                </div>
              </div>

              <div className="rounded-lg border bg-background">
                <div className="border-b p-4">
                  <h3 className="font-semibold">镜头资产需求</h3>
                  <p className="mt-1 text-sm text-muted-foreground">为当前镜头管理角色、场景和道具的衍生参考图</p>
                </div>
                {detailLoading ? <Skeleton className="m-4 h-36" /> : null}
                {!detailLoading && (!shotDetail?.requirements || shotDetail.requirements.length === 0) ? (
                  <p className="p-4 text-sm text-muted-foreground">当前镜头没有资产需求</p>
                ) : null}
                <div className="grid gap-4 p-4">
                  {shotDetail?.requirements.map((requirement) => {
                    const draft = requirementDraft(requirement);
                    const skipped = requirement.status === "skipped";
                    return (
                      <div key={requirement.id} className="grid gap-4 rounded-lg border p-4 lg:grid-cols-[220px_1fr]">
                        <div className="grid content-start gap-3">
                          <ShotMediaPreview kind="image" previewUrl={requirement.derivedPreviewUrl} />
                          <div>
                            <div className="font-medium">{requirement.assetName || requirement.asset?.name || "未命名资产"}</div>
                            <div className="mt-1 flex flex-wrap gap-2">
                              <Badge variant="outline">{assetTypeLabel(requirement.assetType || requirement.asset?.assetType)}</Badge>
                              <Badge variant="outline">{requirementTypeLabel(requirement.requirementType)}</Badge>
                              <Badge variant={skipped ? "secondary" : "outline"}>{statusLabel(requirement.status)}</Badge>
                            </div>
                          </div>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => generateDerivedMutation.mutate(requirement.id)}
                            disabled={generateDerivedMutation.isPending || skipped}
                          >
                            <RefreshCw data-icon="inline-start" />
                            生成衍生图
                          </Button>
                        </div>
                        <div className="grid gap-3">
                          <div className="grid gap-3 md:grid-cols-2">
                            <FieldText label="服装" value={draft.costume} onChange={(value) => setRequirementDraftField(requirement, "costume", value)} disabled={skipped} />
                            <FieldText label="姿态" value={draft.pose} onChange={(value) => setRequirementDraftField(requirement, "pose", value)} disabled={skipped} />
                            <FieldText label="表情" value={draft.expression} onChange={(value) => setRequirementDraftField(requirement, "expression", value)} disabled={skipped} />
                            <FieldText label="动作" value={draft.action} onChange={(value) => setRequirementDraftField(requirement, "action", value)} disabled={skipped} />
                            <FieldText label="镜头关系" value={draft.cameraRelation} onChange={(value) => setRequirementDraftField(requirement, "cameraRelation", value)} disabled={skipped} />
                            <FieldText label="场景状态" value={draft.sceneState} onChange={(value) => setRequirementDraftField(requirement, "sceneState", value)} disabled={skipped} />
                          </div>
                          <FieldText label="道具状态" value={draft.propState} onChange={(value) => setRequirementDraftField(requirement, "propState", value)} disabled={skipped} />
                          <FieldTextarea label="衍生图提示词" value={draft.prompt} onChange={(value) => setRequirementDraftField(requirement, "prompt", value)} rows={3} disabled={skipped} />
                          <div className="flex flex-wrap gap-2">
                            <Button
                              size="sm"
                              onClick={() => updateRequirementMutation.mutate({ requirementId: requirement.id, draft })}
                              disabled={updateRequirementMutation.isPending || skipped}
                            >
                              <Save data-icon="inline-start" />
                              保存需求
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => skipRequirementMutation.mutate(requirement.id)}
                              disabled={skipRequirementMutation.isPending || skipped}
                            >
                              <SkipForward data-icon="inline-start" />
                              跳过需求
                            </Button>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          ) : null}
        </div>
      </div>

      <AlertDialog open={!!shotToDelete} onOpenChange={(open) => !open && setShotToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除镜头</AlertDialogTitle>
            <AlertDialogDescription>删除后该镜头会从分镜表移除，并影响关联的镜头资产、镜头媒体和成片合成。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="rounded-lg border bg-muted/40 p-3 text-sm">
            <div className="font-medium">影响范围</div>
            <ul className="mt-2 grid gap-1 text-muted-foreground">
              {deleteImpactItems.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => shotToDelete && deleteShotMutation.mutate(shotToDelete.id)}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Surface>
  );
}

function refreshStoryboard(
  projectId: string,
  workflowRunId: string,
  shotId: string,
  invalidate: (keys: QueryKey[]) => void,
) {
  const keys: QueryKey[] = [qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)];
  if (workflowRunId) {
    keys.push(["workflow-shots", workflowRunId]);
  }
  if (shotId) {
    keys.push(qk.shotDetail(projectId, shotId));
  }
  invalidate(keys);
}

function mergeShotRows(productionShots: ShotProductionShot[], workflowShots: StoryboardShot[]): ShotRow[] {
  const rows =
    productionShots.length > 0
      ? productionShots.map((shot) => ({
          id: shot.id,
          workflowRunId: shot.workflowRunId,
          shotNo: shot.shotNo,
          shotIndex: shot.shotIndex,
          visual: shot.visual,
          imageArtifactId: shot.imageArtifactId,
          videoArtifactId: shot.videoArtifactId,
          imagePreviewUrl: shot.imagePreviewUrl,
          videoPreviewUrl: shot.videoPreviewUrl,
          imageStatus: shot.imageStatus,
          videoStatus: shot.videoStatus,
          imageErrorMessage: shot.imageErrorMessage,
          videoErrorMessage: shot.videoErrorMessage,
          staleState: shot.staleState,
          canGenerateImage: shot.canGenerateImage,
          canGenerateVideo: shot.canGenerateVideo,
          canRetryImage: shot.canRetryImage,
          canRetryVideo: shot.canRetryVideo,
        }))
      : workflowShots.map((shot) => ({
          id: shot.id,
          workflowRunId: shot.workflowRunId,
          shotNo: shot.shotNo,
          shotIndex: shot.shotIndex,
          durationSeconds: shot.durationSeconds,
          visual: shot.visual,
          camera: shot.camera,
          motion: shot.motion,
          mood: shot.mood,
          imagePrompt: shot.imagePrompt,
          videoPrompt: shot.videoPrompt,
          imageArtifactId: shot.imageArtifactId,
          videoArtifactId: shot.videoArtifactId,
          imagePreviewUrl: shot.imagePreviewUrl,
          videoPreviewUrl: shot.videoPreviewUrl,
          imageStatus: shot.imageStatus,
          videoStatus: shot.videoStatus,
          imageErrorMessage: shot.imageErrorMessage,
          videoErrorMessage: shot.videoErrorMessage,
          staleState: shot.staleState,
          canGenerateImage: !shot.imagePreviewUrl,
          canGenerateVideo: !!shot.imagePreviewUrl && !shot.videoPreviewUrl,
        }));
  return [...rows].sort((left, right) => left.shotIndex - right.shotIndex || left.shotNo - right.shotNo);
}

function rowFromShot(shot: StoryboardShot | ShotRow): ShotRow {
  return {
    id: shot.id,
    workflowRunId: shot.workflowRunId,
    shotNo: shot.shotNo,
    shotIndex: shot.shotIndex,
    durationSeconds: shot.durationSeconds,
    visual: shot.visual,
    camera: "camera" in shot ? shot.camera : undefined,
    motion: "motion" in shot ? shot.motion : undefined,
    mood: "mood" in shot ? shot.mood : undefined,
    imagePrompt: "imagePrompt" in shot ? shot.imagePrompt : undefined,
    videoPrompt: "videoPrompt" in shot ? shot.videoPrompt : undefined,
    imageArtifactId: shot.imageArtifactId,
    videoArtifactId: shot.videoArtifactId,
    imagePreviewUrl: shot.imagePreviewUrl,
    videoPreviewUrl: shot.videoPreviewUrl,
    imageStatus: shot.imageStatus,
    videoStatus: shot.videoStatus,
    staleState: shot.staleState,
  };
}

function shotDraftKey(shot: StoryboardShot | ShotRow) {
  return [
    shot.id,
    shot.durationSeconds ?? "",
    shot.visual ?? "",
    "camera" in shot ? shot.camera ?? "" : "",
    "motion" in shot ? shot.motion ?? "" : "",
    "mood" in shot ? shot.mood ?? "" : "",
    "imagePrompt" in shot ? shot.imagePrompt ?? "" : "",
    "videoPrompt" in shot ? shot.videoPrompt ?? "" : "",
  ].join("\u0001");
}

function draftFromShot(shot: StoryboardShot | ShotRow): ShotDraft {
  return {
    durationSeconds: shot.durationSeconds ? String(shot.durationSeconds) : "",
    visual: shot.visual ?? "",
    camera: "camera" in shot ? shot.camera ?? "" : "",
    motion: "motion" in shot ? shot.motion ?? "" : "",
    mood: "mood" in shot ? shot.mood ?? "" : "",
    imagePrompt: "imagePrompt" in shot ? shot.imagePrompt ?? "" : "",
    videoPrompt: "videoPrompt" in shot ? shot.videoPrompt ?? "" : "",
  };
}

function shotUpdateBody(draft: ShotDraft) {
  const duration = Number.parseFloat(draft.durationSeconds.trim());
  return {
    visual: draft.visual,
    camera: draft.camera,
    motion: draft.motion,
    mood: draft.mood,
    ...(Number.isFinite(duration) && duration > 0 ? { durationSeconds: duration } : {}),
    imagePrompt: draft.imagePrompt,
    videoPrompt: draft.videoPrompt,
  };
}

function requirementDraftKey(requirement: StoryboardShotRequirementDetail) {
  return [
    requirement.id,
    requirement.costume ?? "",
    requirement.pose ?? "",
    requirement.expression ?? "",
    requirement.action ?? "",
    requirement.cameraRelation ?? "",
    requirement.sceneState ?? "",
    requirement.propState ?? "",
    requirement.prompt ?? "",
  ].join("\u0001");
}

function draftFromRequirement(requirement: StoryboardShotRequirementDetail): RequirementDraft {
  return {
    costume: requirement.costume ?? "",
    pose: requirement.pose ?? "",
    expression: requirement.expression ?? "",
    action: requirement.action ?? "",
    cameraRelation: requirement.cameraRelation ?? "",
    sceneState: requirement.sceneState ?? "",
    propState: requirement.propState ?? "",
    prompt: requirement.prompt ?? "",
  };
}

function requirementUpdateBody(draft: RequirementDraft) {
  return {
    costume: draft.costume,
    pose: draft.pose,
    expression: draft.expression,
    action: draft.action,
    cameraRelation: draft.cameraRelation,
    sceneState: draft.sceneState,
    propState: draft.propState,
    prompt: draft.prompt,
  };
}

function buildDeleteImpactItems(shot: ShotRow, detail?: StoryboardShotDetail) {
  const items = [`镜头 ${shot.shotNo || shot.shotIndex + 1} 将从分镜表移除`];
  const requirementCount = detail?.requirements.length ?? 0;
  if (requirementCount > 0) {
    items.push(`${requirementCount} 个镜头资产需求会随镜头失效`);
  }
  if (shot.imagePreviewUrl || shot.imageArtifactId) {
    items.push("已绑定的镜头图片会解除关联");
  }
  if (shot.videoPreviewUrl || shot.videoArtifactId) {
    items.push("已绑定的镜头视频会解除关联");
  }
  items.push("当前成片版本需要重新合成");
  return items;
}

function FieldText({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <Input value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} />
    </div>
  );
}

function FieldTextarea({
  label,
  value,
  onChange,
  rows = 3,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
  disabled?: boolean;
}) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <Textarea value={value} onChange={(event) => onChange(event.target.value)} rows={rows} disabled={disabled} />
    </div>
  );
}

function ShotMediaPreview({ kind, previewUrl }: { kind: "image" | "video"; previewUrl?: string }) {
  if (previewUrl && kind === "video") {
    return <video className="aspect-video w-full rounded-md bg-black object-cover" controls src={previewUrl} />;
  }
  if (previewUrl) {
    return <img alt="镜头图片" className="aspect-video w-full rounded-md bg-muted object-cover" src={previewUrl} />;
  }
  return (
    <div className="grid aspect-video place-items-center rounded-md bg-muted">
      {kind === "image" ? <ImageIcon className="text-muted-foreground" /> : <Video className="text-muted-foreground" />}
    </div>
  );
}
