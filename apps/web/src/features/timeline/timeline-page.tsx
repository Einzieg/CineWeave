"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Film } from "lucide-react";

export function TimelinePage({
  projectId,
  initialTimelineId = "",
  initialClipId = "",
  initialFinalVideoId = ""
}: {
  projectId: string;
  initialTimelineId?: string;
  initialClipId?: string;
  initialFinalVideoId?: string;
}) {
  return (
    <Surface>
      <SectionTitle title="时间线" description="编辑时间线并生成最终视频" />
      <div className="p-4">
        <div className="rounded-lg border border-dashed p-12 text-center">
          <Film className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
          <p className="mt-4 text-sm text-muted-foreground">
            时间线功能开发中...
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            此功能将用于编辑和合成最终视频
          </p>
        </div>
      </div>
    </Surface>
  );
}
