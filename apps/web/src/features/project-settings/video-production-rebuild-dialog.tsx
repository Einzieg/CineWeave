"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, CheckCircle2, Loader2, RefreshCcw } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/cn";
import { localizePlatformError } from "@/lib/error-localization";
import { statusLabel } from "@/lib/labels";
import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type {
  Project,
  VideoProductionCompatibility,
  VideoProductionConfigurationInput,
  VideoProductionProfileVersion,
  VideoProductionRebuild,
  VideoProductionRebuildImpact,
} from "@/lib/types";

type ImpactResult = { impact: VideoProductionRebuildImpact; compatibility: VideoProductionCompatibility };

export function VideoProductionRebuildDialog({
  projectId,
  project,
  open,
  onOpenChange,
  targetConfiguration,
  onConfigurationApplied,
}: {
  projectId: string;
  project: Project;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  targetConfiguration: VideoProductionConfigurationInput;
  onConfigurationApplied?: () => void;
}) {
  const invalidate = useInvalidateKeys();
  const [targetProfileId, setTargetProfileId] = useState("");
  const [impactResult, setImpactResult] = useState<ImpactResult>();
  const [activeRebuildId, setActiveRebuildId] = useState("");
  const notifiedRebuildId = useRef("");
  const unitLabel = project.projectKind === "commerce_video" ? "脚本单元" : "分集";

  const { data: profiles = [], isLoading: profilesLoading } = useApiQuery({
    key: qk.videoProductionProfiles(),
    queryFn: (session) => studioApi.listVideoProductionProfiles(session).then((response) => response.items),
    enabled: open,
  });
  const { data: currentRebuild, isLoading: currentRebuildLoading } = useApiQuery({
    key: qk.currentProjectVideoProductionRebuild(projectId),
    queryFn: (session) => studioApi.getCurrentProjectVideoProductionRebuild(session, projectId),
    enabled: open,
    refetchOnMount: "always",
    refetchInterval: (query) => project.videoProductionLocked && !query.state.data ? 3000 : false,
  });
  const resolvedRebuildId = activeRebuildId || currentRebuild?.id || "";
  const currentProfileVersionId = project.videoProductionBinding?.profileVersionId ?? "";
  const selectedProfile = useMemo(() => {
    const selected = profiles.find((profile) => profile.id === targetProfileId);
    if (selected) return selected;
    const current = profiles.find((profile) => profile.id === currentProfileVersionId);
    return current?.available ? current : profiles.find((profile) => profile.available);
  }, [currentProfileVersionId, profiles, targetProfileId]);

  function handleOpenChange(nextOpen: boolean) {
    if (nextOpen) {
      onOpenChange(true);
      return;
    }
    setTargetProfileId("");
    setImpactResult(undefined);
    setActiveRebuildId("");
    notifiedRebuildId.current = "";
    onOpenChange(false);
  }

  const impactMutation = useApiMutation({
    mutationFn: (session, profile: VideoProductionProfileVersion) => studioApi.getProjectVideoProductionRebuildImpact(
      session,
      projectId,
      profile.profileKey,
      profile.version,
      targetConfiguration,
    ),
    onSuccess: (result) => setImpactResult(result),
    onError: (error) => toast.error("影响分析失败：" + error.message),
  });

  const createMutation = useApiMutation({
    mutationFn: (session, input: { profile: VideoProductionProfileVersion; impact: VideoProductionRebuildImpact }) =>
      studioApi.createProjectVideoProductionRebuild(session, projectId, crypto.randomUUID(), {
        expectedProjectRevision: input.impact.expectedProjectRevision,
        targetProfileKey: input.profile.profileKey,
        targetProfileVersion: input.profile.version,
        targetConfiguration,
        impactToken: input.impact.impactToken,
      }),
    onSuccess: (rebuild) => {
      setActiveRebuildId(rebuild.id);
      toast.success("视频生产方案重建已启动");
      invalidate([
        qk.project(projectId),
        qk.projectVideoProductionProfile(projectId),
        qk.currentProjectVideoProductionRebuild(projectId),
        qk.workflowRuns(projectId),
      ]);
    },
    onError: (error) => toast.error("启动重建失败：" + error.message),
  });

  const { data: rebuild, isLoading: rebuildLoading } = useApiQuery({
    key: qk.projectVideoProductionRebuild(projectId, resolvedRebuildId || "none"),
    queryFn: (session) => studioApi.getProjectVideoProductionRebuild(session, projectId, resolvedRebuildId),
    enabled: open && !!resolvedRebuildId,
    refetchInterval: (query) => isActiveRebuildStatus(query.state.data?.status) ? 3000 : false,
  });
  const { data: rebuildItems = [] } = useApiQuery({
    key: qk.projectVideoProductionRebuildItems(projectId, resolvedRebuildId || "none"),
    queryFn: (session) => studioApi.listProjectVideoProductionRebuildItems(session, projectId, resolvedRebuildId).then((response) => response.items),
    enabled: open && !!resolvedRebuildId,
    refetchInterval: isActiveRebuildStatus(rebuild?.status) ? 3000 : false,
  });

  useEffect(() => {
    if (!rebuild || isActiveRebuildStatus(rebuild.status)) return;
    invalidate([
      qk.project(projectId),
      qk.projectVideoProductionProfile(projectId),
      qk.workflowRuns(projectId),
      qk.productionStatus(projectId),
      qk.shotProductionPrefix(projectId),
    ]);
  }, [invalidate, projectId, rebuild]);

  useEffect(() => {
    if (!rebuild?.targetGenerationId || isActiveRebuildStatus(rebuild.status) || notifiedRebuildId.current === rebuild.id) return;
    notifiedRebuildId.current = rebuild.id;
    onConfigurationApplied?.();
  }, [onConfigurationApplied, rebuild]);

  const retryMutation = useApiMutation({
    mutationFn: (session, current: VideoProductionRebuild) => studioApi.retryFailedProjectVideoProductionRebuildItems(
      session,
      projectId,
      current.id,
      crypto.randomUUID(),
    ),
    onSuccess: (next) => {
      setActiveRebuildId(next.id);
      toast.success("失败分集已重新排队");
      invalidate([
        qk.projectVideoProductionRebuild(projectId, next.id),
        qk.projectVideoProductionRebuildItems(projectId, next.id),
        qk.currentProjectVideoProductionRebuild(projectId),
        qk.workflowRuns(projectId),
      ]);
    },
    onError: (error) => toast.error("重试失败：" + error.message),
  });

  const completedItems = rebuildItems.filter((item) => item.status === "succeeded" || item.status === "skipped").length;
  const failedItems = rebuildItems.filter((item) => item.status === "failed").length;
  const processedItems = completedItems + failedItems;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-4xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>应用视频生产配置</DialogTitle>
        </DialogHeader>

        {currentRebuildLoading && !resolvedRebuildId ? (
          <Skeleton className="my-2 h-28" />
        ) : resolvedRebuildId ? (
          <div className="grid gap-4 py-2">
            {rebuildLoading || !rebuild ? <Skeleton className="h-28" /> : (
              <>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="font-medium">重建进度</div>
                    <div className="mt-1 text-sm text-muted-foreground">
                      已处理 {processedItems}/{Math.max(rebuildItems.length, rebuild.episodeCount)} 个{unitLabel}
                    </div>
                  </div>
                  <Badge variant={rebuild.status === "succeeded" ? "default" : "outline"}>{statusLabel(rebuild.status)}</Badge>
                </div>
                <Progress value={rebuildItems.length > 0 ? Math.round((processedItems / rebuildItems.length) * 100) : 0} className="h-2" />
                {rebuild.failureMessage ? (
                  <div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                    {localizePlatformError(rebuild.failureMessage, rebuild.failureCode ?? undefined)}
                  </div>
                ) : null}
                <div className="divide-y border-y">
                  {rebuildItems.map((item) => (
                    <div key={item.id} className="flex items-start justify-between gap-4 py-3">
                      <div className="min-w-0">
                        <div className="text-sm font-medium">{unitLabel} {item.episodeOrdinal}</div>
                        {item.failureMessage ? (
                          <div className="mt-1 text-xs text-destructive">
                            {localizePlatformError(item.failureMessage, item.failureCode ?? undefined)}
                          </div>
                        ) : null}
                      </div>
                      <Badge variant="outline">{statusLabel(item.status)}</Badge>
                    </div>
                  ))}
                </div>
                {failedItems > 0 && !isActiveRebuildStatus(rebuild.status) ? (
                  <Button onClick={() => retryMutation.mutate(rebuild)} disabled={retryMutation.isPending}>
                    {retryMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCcw className="h-4 w-4" />}
                    重试失败{unitLabel}（{failedItems}）
                  </Button>
                ) : null}
              </>
            )}
          </div>
        ) : (
          <div className="grid gap-5 py-2">
            <div className="grid gap-3 sm:grid-cols-2">
              {profilesLoading ? <><Skeleton className="h-28" /><Skeleton className="h-28" /></> : profiles.map((profile) => {
                const current = profile.id === currentProfileVersionId;
                const selectable = profile.available;
                return (
                  <button
                    key={profile.id}
                    type="button"
                    disabled={!selectable}
                    onClick={() => {
                      setTargetProfileId(profile.id);
                      setImpactResult(undefined);
                    }}
                    className={cn(
                      "grid min-h-28 gap-2 rounded-lg border p-4 text-left transition",
                      selectable && "hover:border-primary hover:bg-muted/40",
                      selectedProfile?.id === profile.id && "border-primary bg-muted/60",
                      !selectable && "cursor-not-allowed opacity-60",
                    )}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <span className="font-medium">{videoProductionProfileLabel(profile.profileKey)}</span>
                      <Badge variant="outline">v{profile.version}</Badge>
                    </div>
                    <div className="text-xs leading-relaxed text-muted-foreground">{profile.description}</div>
                    <div className="flex flex-wrap gap-2">
                      {current ? <Badge>当前方案</Badge> : null}
                      {!profile.available ? <Badge variant="secondary">暂不可用</Badge> : null}
                    </div>
                  </button>
                );
              })}
            </div>

            {selectedProfile && !impactResult ? (
              <Button variant="outline" onClick={() => impactMutation.mutate(selectedProfile)} disabled={impactMutation.isPending}>
                {impactMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <AlertTriangle className="h-4 w-4" />}
                分析重建影响
              </Button>
            ) : null}

            {impactResult ? (
              <div className="grid gap-4 border-t pt-4">
                <div className="flex items-start gap-3">
                  {impactResult.compatibility.executable ? (
                    <CheckCircle2 className="mt-0.5 h-5 w-5 text-status-success" />
                  ) : (
                    <AlertTriangle className="mt-0.5 h-5 w-5 text-status-danger" />
                  )}
                  <div>
                    <div className="font-medium">{impactResult.compatibility.executable ? "模型能力满足目标方案" : "当前模型配置无法执行目标方案"}</div>
                    {impactResult.compatibility.issues.map((issue) => (
                      <div key={issue.code} className="mt-1 text-sm text-destructive">{issue.message}</div>
                    ))}
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <ImpactMetric label="分集" value={impactResult.impact.counts.episodes ?? 0} />
                  <ImpactMetric label="分镜" value={impactResult.impact.counts.storyboardShots ?? 0} />
                  <ImpactMetric label="镜头图片" value={impactResult.impact.counts.shotImages ?? 0} />
                  <ImpactMetric label="镜头视频" value={impactResult.impact.counts.shotVideos ?? 0} />
                </div>
                <div className="rounded-md border border-status-warning/30 bg-status-warning/10 p-3 text-sm">
                  旧生产代的分镜、镜头图片、镜头视频、时间线与成片将归档；{impactResult.impact.counts.retainedAssets ?? 0} 个项目资产会保留。新生产代只按{unitLabel}重建分镜，不会自动生成图片和视频。
                </div>
              </div>
            ) : null}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => handleOpenChange(false)}>关闭</Button>
          {!resolvedRebuildId && impactResult && selectedProfile ? (
            <Button
              onClick={() => createMutation.mutate({ profile: selectedProfile, impact: impactResult.impact })}
              disabled={createMutation.isPending || !impactResult.compatibility.executable}
            >
              {createMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCcw className="h-4 w-4" />}
              确认重建
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ImpactMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="border-l-2 border-primary pl-3">
      <div className="text-xl font-semibold">{value}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

function isActiveRebuildStatus(status?: string) {
  return status === "planned" || status === "approved" || status === "running";
}

function videoProductionProfileLabel(profileKey: VideoProductionProfileVersion["profileKey"]) {
  switch (profileKey) {
    case "single_frame_i2v": return "图生视频模式";
    case "first_last_frame": return "首尾帧衔接模式";
    case "multimodal_reference": return "多模态参考模式";
    case "storyboard_sheet": return "分镜板模式";
  }
}
