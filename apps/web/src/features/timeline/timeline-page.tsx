"use client";

import { useMemo, useState } from "react";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { CheckCircle2, ExternalLink, Film, Play, Plus, Video } from "lucide-react";
import { toast } from "sonner";
import { statusLabel } from "@/lib/labels";
import type { FinalVideoVersion, TimelineClipDetail } from "@/lib/types";

export function TimelinePage({
  projectId,
  initialTimelineId = "",
  initialFinalVideoId = "",
}: {
  projectId: string;
  initialTimelineId?: string;
  initialClipId?: string;
  initialFinalVideoId?: string;
}) {
  const invalidate = useInvalidateKeys();
  const [selectedTimelineId, setSelectedTimelineId] = useState(initialTimelineId);

  const { data: timelines = [], isLoading } = useApiQuery({
    key: qk.timelines(projectId),
    queryFn: (session) => studioApi.listTimelines(session, projectId).then((response) => response.items || []),
  });
  const effectiveTimelineId = selectedTimelineId || timelines[0]?.id || "";
  const { data: detail } = useApiQuery({
    key: qk.timelineDetail(projectId, effectiveTimelineId),
    queryFn: (session) => studioApi.getTimelineDetail(session, projectId, effectiveTimelineId),
    enabled: !!effectiveTimelineId,
  });
  const { data: finalVideos = [] } = useApiQuery({
    key: qk.finalVideos(projectId),
    queryFn: (session) => studioApi.listFinalVideos(session, projectId).then((response) => response.items || []),
  });

  const versions = useMemo(() => detail?.finalVideoVersions?.length ? detail.finalVideoVersions : finalVideos, [detail?.finalVideoVersions, finalVideos]);

  const createTimelineMutation = useApiMutation({
    mutationFn: (session) =>
      studioApi.createTimeline(session, projectId, {
        title: "默认时间线",
        fromStoryboardShots: true,
      }),
    onSuccess: (timeline) => {
      toast.success("时间线已创建");
      setSelectedTimelineId(timeline.id);
      invalidate([qk.timelines(projectId), qk.timelineDetail(projectId, timeline.id)]);
    },
    onError: (error) => toast.error("创建失败：" + error.message),
  });

  const composeMutation = useApiMutation({
    mutationFn: (session, timelineId: string) => studioApi.composeTimeline(session, projectId, timelineId, {}),
    onSuccess: (_response, timelineId) => {
      toast.success("合成任务已启动");
      invalidate([qk.timelines(projectId), qk.timelineDetail(projectId, timelineId), qk.finalVideos(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const activateMutation = useApiMutation({
    mutationFn: (session, versionId: string) => studioApi.activateFinalVideo(session, projectId, versionId),
    onSuccess: () => {
      toast.success("成片版本已激活");
      invalidate([qk.finalVideos(projectId), qk.productionStatus(projectId), qk.timelineDetail(projectId, effectiveTimelineId)]);
    },
    onError: (error) => toast.error("激活失败：" + error.message),
  });

  return (
    <Surface>
      <SectionTitle title="时间线" description="查看时间线、镜头片段和成片版本" />
      <div className="grid gap-5 p-4">
        {isLoading && <Skeleton className="h-64" />}
        {!isLoading && timelines.length === 0 && (
          <div className="rounded-lg border border-dashed p-12 text-center">
            <Film className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
            <p className="mt-4 text-sm text-muted-foreground">暂无时间线</p>
            <Button className="mt-4" onClick={() => createTimelineMutation.mutate()} disabled={createTimelineMutation.isPending}>
              <Plus className="mr-2 h-4 w-4" />
              从分镜创建
            </Button>
          </div>
        )}
        {timelines.length > 0 && (
          <div className="grid gap-4 lg:grid-cols-[280px_1fr]">
            <div className="grid content-start gap-2">
              {timelines.map((timeline) => (
                <button
                  key={timeline.id}
                  className={`rounded-lg border p-3 text-left transition hover:bg-muted/50 ${effectiveTimelineId === timeline.id ? "bg-muted/50 ring-2 ring-primary" : ""}`}
                  onClick={() => setSelectedTimelineId(timeline.id)}
                  type="button"
                >
                  <div className="font-medium">{timeline.title}</div>
                  <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                    <span>{timeline.aspectRatio}</span>
                    <span>{timeline.resolution}</span>
                  </div>
                  <Badge className="mt-2" variant="outline">{statusLabel(timeline.status)}</Badge>
                </button>
              ))}
              <Button variant="outline" onClick={() => createTimelineMutation.mutate()} disabled={createTimelineMutation.isPending}>
                <Plus className="mr-2 h-4 w-4" />
                新建时间线
              </Button>
            </div>
            <div className="grid gap-4">
              {detail ? (
                <>
                  <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4">
                    <div>
                      <h3 className="text-lg font-semibold">{detail.timeline.title}</h3>
                      <p className="text-sm text-muted-foreground">
                        {detail.clips.length} 个片段 · {detail.timeline.aspectRatio} · {detail.timeline.resolution}
                      </p>
                    </div>
                    <Button onClick={() => composeMutation.mutate(detail.timeline.id)} disabled={composeMutation.isPending || detail.clips.length === 0}>
                      <Play className="mr-2 h-4 w-4" />
                      合成成片
                    </Button>
                  </div>
                  <div className="grid gap-3">
                    {detail.clips.map((clip) => <ClipRow clip={clip} key={clip.id} />)}
                    {detail.clips.length === 0 ? (
                      <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无片段</div>
                    ) : null}
                  </div>
                </>
              ) : (
                <Skeleton className="h-64" />
              )}
            </div>
          </div>
        )}
        <div className="grid gap-3">
          <h3 className="text-lg font-semibold">成片版本</h3>
          {versions.length === 0 ? <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无成片版本</div> : null}
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {versions.map((version) => (
              <FinalVideoCard
                key={version.id}
                version={version}
                active={version.status === "active" || version.id === initialFinalVideoId}
                onActivate={() => activateMutation.mutate(version.id)}
                busy={activateMutation.isPending}
              />
            ))}
          </div>
        </div>
      </div>
    </Surface>
  );
}

function ClipRow({ clip }: { clip: TimelineClipDetail }) {
  return (
    <div className="grid gap-3 rounded-lg border p-3 sm:grid-cols-[180px_1fr]">
      {clip.previewUrl ? (
        <video className="aspect-video w-full rounded-md bg-black object-cover" controls src={clip.previewUrl} />
      ) : (
        <div className="grid aspect-video place-items-center rounded-md bg-muted">
          <Video className="h-6 w-6 text-muted-foreground" />
        </div>
      )}
      <div className="grid content-start gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="font-medium">{clip.title}</div>
          <Badge variant={clip.enabled ? "outline" : "secondary"}>{statusLabel(clip.enabled ? "enabled" : "disabled")}</Badge>
        </div>
        <div className="text-sm text-muted-foreground">
          #{clip.clipIndex + 1} · {clip.targetDurationSeconds ?? clip.sourceDurationSeconds ?? "-"}s
        </div>
        {clip.notes ? <p className="text-sm text-muted-foreground">{clip.notes}</p> : null}
      </div>
    </div>
  );
}

function FinalVideoCard({ version, active, busy, onActivate }: { version: FinalVideoVersion; active: boolean; busy: boolean; onActivate: () => void }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      {version.previewUrl ? (
        <video className="aspect-video w-full bg-black object-cover" controls src={version.previewUrl} />
      ) : (
        <div className="grid aspect-video place-items-center bg-muted">
          <Film className="h-6 w-6 text-muted-foreground" />
        </div>
      )}
      <div className="grid gap-3 p-3">
        <div className="flex items-start justify-between gap-2">
          <div>
            <div className="font-medium">{version.title}</div>
            <div className="text-xs text-muted-foreground">v{version.version} · {version.resolution} · {version.aspectRatio}</div>
          </div>
          <Badge variant={active ? "default" : "outline"}>{statusLabel(version.status)}</Badge>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={onActivate} disabled={busy || active}>
            <CheckCircle2 className="mr-1 h-3.5 w-3.5" />
            激活
          </Button>
          {version.previewUrl ? (
            <a className="inline-flex h-8 items-center gap-1 rounded-lg border px-2.5 text-sm" href={version.previewUrl} rel="noreferrer" target="_blank">
              打开
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          ) : null}
        </div>
      </div>
    </div>
  );
}
