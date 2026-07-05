"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { AlertCircle } from "lucide-react";

export function ReviewPage({ projectId }: { projectId: string }) {
  return (
    <Surface>
      <SectionTitle title="审阅中心" description="运行审查、查看问题并生成修复建议" />
      <div className="p-4">
        <div className="rounded-lg border border-dashed p-12 text-center">
          <AlertCircle className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
          <p className="mt-4 text-sm text-muted-foreground">
            审阅功能开发中...
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            此功能将用于审查项目内容并生成修复建议
          </p>
        </div>
      </div>
    </Surface>
  );
}
