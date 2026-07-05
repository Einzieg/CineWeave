"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Film } from "lucide-react";

export function StoryboardPage({
  projectId,
  initialShotId = "",
  initialRequirementId = ""
}: {
  projectId: string;
  initialShotId?: string;
  initialRequirementId?: string;
}) {
  return (
    <Surface>
      <SectionTitle title="分镜工作台" description="管理分镜镜头，生成图片和视频" />
      <div className="p-4">
        <div className="rounded-lg border border-dashed p-12 text-center">
          <Film className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
          <p className="mt-4 text-sm text-muted-foreground">
            分镜功能开发中...
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            此功能将用于管理分镜并生成镜头素材
          </p>
        </div>
      </div>
    </Surface>
  );
}
