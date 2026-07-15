"use client";

import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Progress } from "@/components/ui/progress";
import { Activity, CheckCircle2, Clock, XCircle, type LucideIcon } from "lucide-react";
import { productionFieldLabel, productionStageLabel } from "@/lib/labels";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import type { ProductionStatus } from "@/lib/types";

type ProductionStage = ProductionStatus["stages"][keyof ProductionStatus["stages"]];

export function ProductionPage({ projectId }: { projectId: string }) {
  const pollingFallback = useProjectPollingFallback(projectId);
  // 获取生产状态
  const { data: status, isLoading } = useApiQuery({
    key: qk.productionStatus(projectId),
    queryFn: (session) => studioApi.getProductionStatus(session, projectId),
    refetchInterval: (query) => {
      const data = query.state.data;
      const isRunning = data?.overall?.status === "running";
      return pollingFallback && isRunning ? 5000 : false;
    },
  });

  if (isLoading) {
    return <Skeleton className="h-96" />;
  }

  const overall = status?.overall;
  const stages = status?.stages;

  return (
    <Surface>
      <SectionTitle title="生产看板" description="查看项目生产状态和进度" />

      <div className="p-6 space-y-6">
        {/* 整体进度 */}
        <div className="rounded-lg border p-6">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h3 className="text-lg font-semibold">整体进度</h3>
              <p className="text-sm text-muted-foreground">
                当前阶段: {productionStageLabel(overall?.stage || "not_started")}
              </p>
            </div>
            <StatusBadge status={overall?.status || "pending"} />
          </div>
          <Progress value={overall?.progress || 0} className="h-3" />
          <div className="mt-2 text-sm text-muted-foreground text-right">
            {Math.round(overall?.progress || 0)}%
          </div>
        </div>

        {/* 各阶段统计 */}
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {stages && (
            <>
              <StageCard title="原文" icon={Activity} stage={stages.source} />
              <StageCard title="资产" icon={Activity} stage={stages.assets} />
              <StageCard title="分镜" icon={Activity} stage={stages.storyboard} />
              <StageCard title="镜头资产" icon={Activity} stage={stages.shotAssets} />
              <StageCard title="镜头图片" icon={Activity} stage={stages.shotImages} />
              <StageCard title="镜头视频" icon={Activity} stage={stages.shotVideos} />
              <StageCard title="最终成片" icon={Activity} stage={stages.finalVideo} />
            </>
          )}
        </div>
      </div>
    </Surface>
  );
}

function StageCard({ title, icon: Icon, stage }: { title: string; icon: LucideIcon; stage: ProductionStage }) {
  const getStatusIcon = (status: string) => {
    switch (status) {
      case "completed": return <CheckCircle2 className="h-4 w-4 text-emerald-500" />;
      case "running": return <Clock className="h-4 w-4 text-cyan-500 animate-pulse" />;
      case "failed": return <XCircle className="h-4 w-4 text-rose-500" />;
      default: return <Clock className="h-4 w-4 text-muted-foreground" />;
    }
  };

  return (
    <div className="rounded-lg border p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-muted-foreground" />
          <h4 className="font-medium">{title}</h4>
        </div>
        {getStatusIcon(stage?.status)}
      </div>
      <div className="space-y-1 text-sm text-muted-foreground">
        {Object.entries(stage || {})
          .filter(([key]) => !["status", "summary"].includes(key))
          .slice(0, 3)
          .map(([key, value]) => (
            <div key={key} className="flex justify-between">
              <span>{productionFieldLabel(key)}</span>
              <span className="font-medium text-foreground">{String(value)}</span>
            </div>
          ))}
      </div>
    </div>
  );
}
