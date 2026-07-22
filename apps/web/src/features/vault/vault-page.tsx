"use client";

import NextImage from "next/image";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ExternalLink, File, Image as ImageIcon } from "lucide-react";
import { artifactTypeLabel } from "@/lib/labels";
import type { Artifact } from "@/lib/types";

export function VaultPage({ projectId }: { projectId: string }) {
  const { data: artifacts = [], isLoading } = useApiQuery({
    key: qk.artifacts(projectId),
    queryFn: (session) => studioApi.listArtifacts(session, projectId).then((response) => response.items || []),
  });

  return (
    <Surface>
      <SectionTitle title="媒体资产库" description="查看项目生成的图片、视频和文档" />
      <div className="grid gap-4 p-4">
        {isLoading && <Skeleton className="h-64" />}
        {!isLoading && artifacts.length === 0 && (
          <div className="rounded-lg border border-dashed p-12 text-center">
            <File className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
            <p className="mt-4 text-sm text-muted-foreground">暂无媒体文件</p>
          </div>
        )}
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {artifacts.map((artifact) => (
            <div key={artifact.id} className="overflow-hidden rounded-lg border">
              <ArtifactPreview artifact={artifact} />
              <div className="grid gap-2 p-3">
                <div className="flex items-center justify-between gap-2">
                  <Badge variant="outline">{artifactTypeLabel(artifact.type)}</Badge>
                  {artifact.previewUrl ? (
                    <a className="inline-flex items-center gap-1 text-sm text-primary" href={artifact.previewUrl} rel="noreferrer" target="_blank">
                      打开
                      <ExternalLink className="h-3.5 w-3.5" />
                    </a>
                  ) : null}
                </div>
                <div className="grid gap-1 text-xs text-muted-foreground">
                  <span>{artifact.mimeType || "未知类型"}</span>
                  <span className="truncate">{formatArtifactSummary(artifact)}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </Surface>
  );
}

function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  if (artifact.previewUrl && artifact.mimeType?.startsWith("video/")) {
    return <video className="aspect-video w-full bg-black object-cover" controls src={artifact.previewUrl} />;
  }
  if (artifact.previewUrl) {
    return (
      <div className="relative aspect-video w-full bg-muted">
        <NextImage alt={artifactTypeLabel(artifact.type)} className="object-cover" fill sizes="(max-width: 640px) 100vw, 33vw" src={artifact.previewUrl} unoptimized />
      </div>
    );
  }
  return (
    <div className="grid aspect-video place-items-center bg-muted">
      <ImageIcon className="h-6 w-6 text-muted-foreground" />
    </div>
  );
}

function formatArtifactSummary(artifact: Artifact) {
  if (artifact.createdAt) {
    return `创建于 ${formatDateTime(artifact.createdAt)}`;
  }
  return artifact.previewUrl ? "可预览" : "待生成预览";
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未记录时间";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
