"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Package } from "lucide-react";

export function ExportPage({ projectId }: { projectId: string }) {
  return (
    <Surface>
      <SectionTitle title="导出中心" description="创建导出任务并下载最终产出" />
      <div className="p-4">
        <div className="rounded-lg border border-dashed p-12 text-center">
          <Package className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
          <p className="mt-4 text-sm text-muted-foreground">
            导出功能开发中...
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            此功能将用于导出项目的最终产出
          </p>
        </div>
      </div>
    </Surface>
  );
}
