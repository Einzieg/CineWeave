"use client";

import { Loader2, RefreshCw, Volume2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { studioApi } from "@/lib/api-client";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";

export function EpisodeAudioPanel({ projectId, episodeId }: { projectId: string; episodeId: string }) {
  const invalidateKeys = useInvalidateKeys();
  const { data, isLoading } = useApiQuery({
    key: qk.episodeAudio(projectId, episodeId),
    queryFn: (session) => studioApi.getEpisodeAudio(session, projectId, episodeId),
  });
  const { data: voices = [] } = useApiQuery({
    key: qk.characterVoices(projectId),
    queryFn: (session) => studioApi.listCharacterVoices(session, projectId).then((response) => response.items),
  });
  const produceMutation = useApiMutation({
    mutationFn: (session, force: boolean) => studioApi.produceEpisodeAudio(session, projectId, episodeId, {
      force,
      maxConcurrency: 5,
      mixAfterTts: true,
    }),
    onSuccess: (run) => {
      invalidateKeys([qk.workflowRuns(projectId), qk.episodeAudio(projectId, episodeId)]);
      toast.success(`配音任务已创建：${run.id.slice(0, 8)}`);
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "配音任务创建失败"),
  });

  if (isLoading) {
    return <Skeleton className="h-24 w-full" />;
  }

  const clips = data?.clips ?? [];
  const activeClips = newestActiveClips(clips);
  const succeeded = activeClips.filter((clip) => clip.status === "succeeded").length;
  const failed = activeClips.filter((clip) => clip.status === "failed").length;
  const running = activeClips.filter((clip) => clip.status === "queued" || clip.status === "running").length;
  const activeMix = data?.mixes.find((mix) => mix.active) ?? data?.mixes[0];
  const hasGenerated = activeClips.length > 0;

  return (
    <section className="space-y-3 border-t pt-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-2 font-medium"><Volume2 className="h-4 w-4" />分集音频</div>
          {hasGenerated ? <Badge variant="outline">成功 {succeeded}</Badge> : null}
          {running > 0 ? <Badge variant="secondary"><Loader2 className="mr-1 h-3 w-3 animate-spin" />运行中 {running}</Badge> : null}
          {failed > 0 ? <Badge variant="destructive">失败 {failed}</Badge> : null}
          {voices.length === 0 ? <Badge variant="outline">未配置角色声音</Badge> : null}
        </div>
        <div className="flex gap-2">
          {failed > 0 ? (
            <Button type="button" variant="outline" disabled={produceMutation.isPending || voices.length === 0} onClick={() => produceMutation.mutate(false)}>
              {produceMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-2 h-4 w-4" />}重试失败音频
            </Button>
          ) : null}
          <Button type="button" disabled={produceMutation.isPending || voices.length === 0 || running > 0} onClick={() => produceMutation.mutate(hasGenerated)}>
            {produceMutation.isPending || running > 0 ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Volume2 className="mr-2 h-4 w-4" />}
            {hasGenerated ? "全部重新生成" : "生成分集音频"}
          </Button>
        </div>
      </div>

      {activeMix?.previewUrl ? (
        <div className="rounded-md border p-3">
          <div className="mb-2 flex items-center justify-between gap-3 text-sm">
            <span>分集混音 · 版本 {activeMix.revision}</span>
            <Badge variant={activeMix.productionReadiness === "ready" ? "default" : "outline"}>{statusLabel(activeMix.productionReadiness)}</Badge>
          </div>
          <audio className="w-full" controls preload="metadata" src={activeMix.previewUrl} />
        </div>
      ) : null}

      {activeClips.length > 0 ? (
        <div className="max-h-56 divide-y overflow-y-auto rounded-md border">
          {activeClips.map((clip) => (
            <div key={clip.id} className="grid gap-2 p-3 md:grid-cols-[minmax(0,1fr)_minmax(220px,320px)] md:items-center">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-medium">{clip.speaker || "旁白"}</span>
                  <Badge variant="outline">{statusLabel(clip.status)}</Badge>
                  {clip.durationSeconds ? <span className="text-muted-foreground">{clip.durationSeconds.toFixed(2)} 秒</span> : null}
                </div>
                <div className="mt-1 line-clamp-2 text-sm text-muted-foreground">{clip.sourceText}</div>
                {clip.errorMessage ? <div className="mt-1 text-sm text-destructive">{clip.errorMessage}</div> : null}
              </div>
              {clip.previewUrl ? <audio className="w-full" controls preload="none" src={clip.previewUrl} /> : null}
            </div>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function newestActiveClips<T extends { timingUnitId: string; active: boolean; revision: number }>(clips: T[]) {
  const byUnit = new Map<string, T>();
  for (const clip of clips) {
    const current = byUnit.get(clip.timingUnitId);
    if (!current || clip.active || clip.revision > current.revision) {
      byUnit.set(clip.timingUnitId, clip);
    }
  }
  return [...byUnit.values()];
}
