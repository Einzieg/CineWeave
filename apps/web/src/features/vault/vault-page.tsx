"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Folder } from "lucide-react";

export function VaultPage({ projectId }: { projectId: string }) {
  return (
    <Surface>
      <SectionTitle title="媒体资产库" description="管理项目相关的所有媒体文件" />
      <div className="p-4">
        <div className="rounded-lg border border-dashed p-12 text-center">
          <Folder className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
          <p className="mt-4 text-sm text-muted-foreground">
            媒体资产库功能开发中...
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            此功能将用于浏览和管理项目中的所有媒体文件
          </p>
        </div>
      </div>
    </Surface>
  );
}
