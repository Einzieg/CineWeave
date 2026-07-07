"use client";

import { useMemo, useState } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Film, Image as ImageIcon, Save, Trash2, Video } from "lucide-react";
import { toast } from "sonner";
import { statusLabel } from "@/lib/labels";
import type { ShotProductionShot, StoryboardShot } from "@/lib/types";

type ShotRow = {
  id: string;
  workflowRunId: string;
  shotNo: number;
  shotIndex: number;
  visual?: string;
  imagePreviewUrl?: string;
  videoPreviewUrl?: string;
  imageStatus?: string;
  videoStatus?: string;
  staleState?: string;
  canGenerateImage?: boolean;
  canGenerateVideo?: boolean;
  canRetryImage?: boolean;
  canRetryVideo?: boolean;
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
  const [editingShotId, setEditingShotId] = useState(initialShotId);
  const [draftVisual, setDraftVisual] = useState("");

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

  const productionMutation = useApiMutation({
    mutationFn: (session, payload: { action: string; shotId: string }) =>
      studioApi.runShotProductionAction(session, projectId, {
        action: payload.action,
        shotIds: [payload.shotId],
        options: { maxConcurrency: 1 },
      }),
    onSuccess: () => {
      toast.success("任务已启动");
      invalidate([qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const updateShotMutation = useApiMutation({
    mutationFn: (session, payload: { shotId: string; visual: string }) =>
      studioApi.updateStoryboardShot(session, projectId, payload.shotId, { visual: payload.visual, manualOverride: true }),
    onSuccess: () => {
      toast.success("分镜已保存");
      setEditingShotId("");
      invalidate([qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const deleteShotMutation = useApiMutation({
    mutationFn: (session, shotId: string) => studioApi.deleteStoryboardShot(session, projectId, shotId),
    onSuccess: () => {
      toast.success("分镜已删除");
      invalidate([qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const loading = workflowShotsLoading || productionLoading;

  return (
    <Surface>
      <SectionTitle title="分镜工作台" description="管理分镜镜头，生成图片和视频" />
      <div className="grid gap-4 p-4">
        {loading && <Skeleton className="h-64" />}
        {!loading && rows.length === 0 && (
          <div className="rounded-lg border border-dashed p-12 text-center">
            <Film className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
            <p className="mt-4 text-sm text-muted-foreground">暂无分镜</p>
          </div>
        )}
        <div className="grid gap-4">
          {rows.map((shot) => {
            const editing = editingShotId === shot.id;
            return (
              <div key={shot.id} className="grid gap-4 rounded-lg border p-4 lg:grid-cols-[minmax(220px,360px)_1fr]">
                <div className="grid gap-3">
                  <ShotMediaPreview kind="image" previewUrl={shot.imagePreviewUrl} />
                  <ShotMediaPreview kind="video" previewUrl={shot.videoPreviewUrl} />
                </div>
                <div className="grid content-start gap-3">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <h3 className="text-lg font-semibold">镜头 {shot.shotNo || shot.shotIndex + 1}</h3>
                      <p className="text-xs text-muted-foreground">{shot.workflowRunId}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <Badge variant="outline">图像 {statusLabel(shot.imageStatus || "pending")}</Badge>
                      <Badge variant="outline">视频 {statusLabel(shot.videoStatus || "pending")}</Badge>
                      {shot.staleState && shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(shot.staleState)}</Badge> : null}
                    </div>
                  </div>
                  {editing ? (
                    <Textarea value={draftVisual} onChange={(event) => setDraftVisual(event.target.value)} />
                  ) : (
                    <p className="text-sm leading-6 text-muted-foreground">{shot.visual || "未填写画面描述"}</p>
                  )}
                  <div className="flex flex-wrap gap-2">
                    {editing ? (
                      <Button size="sm" onClick={() => updateShotMutation.mutate({ shotId: shot.id, visual: draftVisual })} disabled={updateShotMutation.isPending}>
                        <Save className="mr-1 h-3.5 w-3.5" />
                        保存
                      </Button>
                    ) : (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setEditingShotId(shot.id);
                          setDraftVisual(shot.visual || "");
                        }}
                      >
                        编辑
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => productionMutation.mutate({ action: shot.canRetryImage ? "regenerate_failed_images" : "generate_selected_images", shotId: shot.id })}
                      disabled={productionMutation.isPending || (!shot.canGenerateImage && !shot.canRetryImage)}
                    >
                      <ImageIcon className="mr-1 h-3.5 w-3.5" />
                      生成图片
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => productionMutation.mutate({ action: shot.canRetryVideo ? "regenerate_failed_videos" : "generate_selected_videos", shotId: shot.id })}
                      disabled={productionMutation.isPending || (!shot.canGenerateVideo && !shot.canRetryVideo)}
                    >
                      <Video className="mr-1 h-3.5 w-3.5" />
                      生成视频
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => deleteShotMutation.mutate(shot.id)} disabled={deleteShotMutation.isPending}>
                      <Trash2 className="mr-1 h-3.5 w-3.5" />
                      删除
                    </Button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </Surface>
  );
}

function mergeShotRows(productionShots: ShotProductionShot[], workflowShots: StoryboardShot[]): ShotRow[] {
  if (productionShots.length > 0) {
    return productionShots.map((shot) => ({
      id: shot.id,
      workflowRunId: shot.workflowRunId,
      shotNo: shot.shotNo,
      shotIndex: shot.shotIndex,
      visual: shot.visual,
      imagePreviewUrl: shot.imagePreviewUrl,
      videoPreviewUrl: shot.videoPreviewUrl,
      imageStatus: shot.imageStatus,
      videoStatus: shot.videoStatus,
      staleState: shot.staleState,
      canGenerateImage: shot.canGenerateImage,
      canGenerateVideo: shot.canGenerateVideo,
      canRetryImage: shot.canRetryImage,
      canRetryVideo: shot.canRetryVideo,
    }));
  }
  return workflowShots.map((shot) => ({
    id: shot.id,
    workflowRunId: shot.workflowRunId,
    shotNo: shot.shotNo,
    shotIndex: shot.shotIndex,
    visual: shot.visual,
    imagePreviewUrl: shot.imagePreviewUrl,
    videoPreviewUrl: shot.videoPreviewUrl,
    imageStatus: shot.imageStatus,
    videoStatus: shot.videoStatus,
    staleState: shot.staleState,
    canGenerateImage: !shot.imagePreviewUrl,
    canGenerateVideo: !!shot.imagePreviewUrl && !shot.videoPreviewUrl,
  }));
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
      {kind === "image" ? <ImageIcon className="h-6 w-6 text-muted-foreground" /> : <Video className="h-6 w-6 text-muted-foreground" />}
    </div>
  );
}
