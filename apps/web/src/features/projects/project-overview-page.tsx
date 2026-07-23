"use client";

import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { AlertCircle, ArrowRight, Boxes, Clapperboard, FileCode2, FileText, Film, Play, Video } from "lucide-react";
import Link from "next/link";
import type { Route } from "next";
import { toast } from "sonner";
import { contentTypeLabel, projectTypeLabel, reviewSeverityLabel } from "@/lib/labels";
import { nextProductionAction, productionActionLabel, productionRefreshKeys, runProductionAction } from "@/features/production/production-actions";
import { projectHref } from "@/lib/routes";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import type { ProductionStatus } from "@/lib/types";

type FlowStage = {
  key: string;
  label: string;
  href: string;
  countLabel: string;
  summary: string[];
  buttonLabel: string;
  action?: string;
  icon: typeof FileText;
};

export function ProjectOverviewPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);

  // 获取项目信息
  const { data: project, isLoading: projectLoading } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });

  // 获取生产状态（轮询）
  const { data: productionStatus, isLoading: statusLoading } = useApiQuery({
    key: qk.productionStatus(projectId),
    queryFn: (session) => studioApi.getProductionStatus(session, projectId),
    refetchInterval: (query) => {
      const data = query.state.data;
      if (!data) return false;
      // 检查overall status判断是否需要轮询
      const isRunning = data.overall?.status === "running" || data.overall?.status === "processing";
      return pollingFallback && isRunning ? 5000 : false;
    },
  });

  // 获取待办审查项
  const { data: reviewItems = [] } = useApiQuery({
    key: qk.reviewItems(projectId, { status: "open" }),
    queryFn: (session) =>
      studioApi.listReviewItems(session, projectId, { status: "open" }).then(r => r.items || []),
  });

  // 执行下一步操作
  const nextActionMutation = useApiMutation({
    mutationFn: (session, action: string) =>
      runProductionAction(session, projectId, action),
    onSuccess: (response) => {
      toast.success(response.note || `${productionActionLabel(response.action)}已启动`);
      invalidate(productionRefreshKeys(projectId));
    },
    onError: (error) => {
      toast.error("操作失败：" + error.message);
    },
  });

  if (projectLoading || statusLoading) {
    return (
      <div className="grid gap-5">
        <Skeleton className="h-32" />
        <Skeleton className="h-64" />
      </div>
    );
  }

  if (!project) {
    return <div>项目不存在</div>;
  }

  const nextAction = productionStatus ? nextProductionAction(productionStatus) : "";
  const flowStages = productionStatus ? buildFlowStages(projectId, productionStatus, nextAction) : [];

  return (
    <div className="grid gap-5">
      {/* 项目信息卡片 */}
      <Surface className="p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1">
            <h2 className="text-2xl font-semibold">{project.name}</h2>
            <p className="mt-2 text-sm text-muted-foreground">{project.description || "暂无简介"}</p>
            <div className="mt-4 flex flex-wrap gap-3 text-sm">
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">类型:</span>
                <Badge variant="outline">{projectTypeLabel(project.projectType)}</Badge>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">内容:</span>
                <Badge variant="outline">{contentTypeLabel(project.contentType)}</Badge>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">画风:</span>
                <Badge variant="outline">{project.artStyle || "未设置"}</Badge>
              </div>
            </div>
          </div>
          <StatusBadge status={project.status ?? "active"} />
        </div>
      </Surface>

      {/* 主CTA：继续下一步 */}
      {nextAction !== "parse_script_scenes" ? (
        <div className="flex justify-center">
          <Button
            size="lg"
            className="h-14 gap-3 px-8 text-base font-semibold"
            onClick={() => nextAction && nextActionMutation.mutate(nextAction)}
            disabled={!nextAction || nextActionMutation.isPending}
          >
            <Play className="h-5 w-5" />
            {nextAction ? productionActionLabel(nextAction) : "开始制作"}
          </Button>
        </div>
      ) : null}

      {/* 主流程 */}
      <Surface>
        <SectionTitle title="主流程" description="内容、剧本、资产、分镜、视频、成片" />
        <div className="p-5">
          <div className="grid gap-3 xl:grid-cols-6">
            {flowStages.map((stage) => (
              <FlowStageCard
                busy={nextActionMutation.isPending}
                key={stage.key}
                onRun={(action) => nextActionMutation.mutate(action)}
                stage={stage}
              />
            ))}
          </div>
        </div>
      </Surface>

      {/* 待办事项 */}
      {reviewItems.length > 0 && (
        <Surface>
          <SectionTitle
            title="待办事项"
            description={`${reviewItems.length} 个审查项需要处理`}
          />
          <div className="divide-y">
            {reviewItems.slice(0, 5).map((item) => (
              <div key={item.id} className="flex items-start gap-3 p-4 hover:bg-muted/50">
                <AlertCircle className="h-5 w-5 shrink-0 text-amber-500" />
                <div className="flex-1">
                  <div className="font-medium">{item.title}</div>
                  <div className="text-sm text-muted-foreground">{item.description}</div>
                </div>
                <Badge variant={item.severity === "high" ? "destructive" : "secondary"}>
                  {reviewSeverityLabel(item.severity)}
                </Badge>
              </div>
            ))}
          </div>
        </Surface>
      )}
    </div>
  );
}

function FlowStageCard({
  stage,
  busy,
  onRun,
}: {
  stage: FlowStage;
  busy: boolean;
  onRun: (action: string) => void;
}) {
  const Icon = stage.icon;
  return (
    <div className="grid min-h-64 content-between gap-4 rounded-lg border p-4">
      <div className="grid gap-3">
        <div>
          <div className="grid h-9 w-9 place-items-center rounded-md bg-muted">
            <Icon className="h-4 w-4" />
          </div>
        </div>
        <div>
          <div className="font-semibold">{stage.label}</div>
          <div className="mt-1 text-xs text-muted-foreground">{stage.countLabel}</div>
        </div>
        <div className="grid gap-1.5">
          {stage.summary.slice(0, 3).map((line) => (
            <div className="text-xs leading-5 text-muted-foreground" key={line}>
              {line}
            </div>
          ))}
        </div>
      </div>
      {stage.action ? (
        <Button className="w-full" disabled={busy} onClick={() => onRun(stage.action || "")} size="sm">
          <Play className="mr-1.5 h-3.5 w-3.5" />
          {stage.buttonLabel}
        </Button>
      ) : (
        <Button asChild className="w-full" size="sm" variant="outline">
          <Link href={stage.href as Route}>
            {stage.buttonLabel}
            <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
          </Link>
        </Button>
      )}
    </div>
  );
}

function buildFlowStages(projectId: string, status: ProductionStatus, nextAction: string): FlowStage[] {
  const source = status.stages.source;
  const assets = status.stages.assets;
  const storyboard = status.stages.storyboard;
  const shotAssets = status.stages.shotAssets;
  const shotImages = status.stages.shotImages;
  const shotVideos = status.stages.shotVideos;
  const finalVideo = status.stages.finalVideo;
  const sourceCount = source.novelSourceCount + source.scriptSourceCount + source.briefSourceCount;

  return [
    {
      key: "content",
      label: "内容",
      href: projectHref(projectId, "content"),
      countLabel: `${sourceCount} 个内容 · ${source.chapterCount} 个分集`,
      summary: summaryOrFallback(source.summary, sourceCount > 0 ? "内容已添加，可继续生成剧本" : "先添加小说原文、剧本或创意文案"),
      buttonLabel: "管理内容",
      icon: FileText,
    },
    {
      key: "scripts",
      label: "剧本",
      href: projectHref(projectId, "scripts"),
      countLabel: source.activeScriptId ? `已激活 · ${source.scriptSceneCount ?? 0} 场` : "未激活剧本",
      summary: [
        source.activeScriptTitle ? `当前剧本：${source.activeScriptTitle}` : "根据内容生成或导入剧本",
        source.pendingScriptSceneCount ? `还有 ${source.pendingScriptSceneCount} 个场景待确认` : "剧本场景可查看和编辑",
      ],
      buttonLabel: nextAction === "generate_script" || nextAction === "generate_script_from_plan" ? productionActionLabel(nextAction) : "管理剧本",
      action: nextAction === "generate_script" || nextAction === "generate_script_from_plan" ? nextAction : undefined,
      icon: FileCode2,
    },
    {
      key: "assets",
      label: "资产",
      href: projectHref(projectId, "assets"),
      countLabel: `${assets.characterCount} 角色 · ${assets.sceneCount} 场景 · ${assets.propCount} 道具`,
      summary: [
        assets.missingAssetCardCount ? `${assets.missingAssetCardCount} 个资产卡待补齐` : "资产卡已归档到项目",
        assets.missingReferenceImageCount ? `${assets.missingReferenceImageCount} 张主资产图待生成` : "主资产图可查看和重生成",
      ],
      buttonLabel: nextAction === "analyze_assets" || nextAction === "generate_asset_images" ? productionActionLabel(nextAction) : "管理资产",
      action: nextAction === "analyze_assets" || nextAction === "generate_asset_images" ? nextAction : undefined,
      icon: Boxes,
    },
    {
      key: "storyboard",
      label: "分镜",
      href: projectHref(projectId, "storyboard"),
      countLabel: `${storyboard.shotCount} 个镜头 · ${shotAssets.requirementCount} 个资产需求`,
      summary: [
        storyboard.staleShotCount ? `${storyboard.staleShotCount} 个镜头需要重生成` : "分镜表可查看、编辑和删除",
        shotAssets.missingDerivedImageCount ? `${shotAssets.missingDerivedImageCount} 张衍生资产图待生成` : "镜头资产需求已同步",
      ],
      buttonLabel: nextAction === "generate_storyboard" || nextAction === "analyze_shot_assets" || nextAction === "generate_derived_asset_images" ? productionActionLabel(nextAction) : "管理分镜",
      action: nextAction === "generate_storyboard" || nextAction === "analyze_shot_assets" || nextAction === "generate_derived_asset_images" ? nextAction : undefined,
      icon: Clapperboard,
    },
    {
      key: "video",
      label: "视频",
      href: projectHref(projectId, "video"),
      countLabel: `${shotVideos.succeeded}/${shotVideos.total} 个镜头视频完成`,
      summary: [
        shotImages.pending + shotImages.stale ? `${shotImages.pending + shotImages.stale} 个镜头图片待生成` : "镜头图片已满足视频生成条件",
        shotVideos.failed ? `${shotVideos.failed} 个镜头视频失败，可重试` : "镜头视频可批量生成和查看",
      ],
      buttonLabel: nextAction === "generate_shot_images" || nextAction === "generate_shot_videos" ? productionActionLabel(nextAction) : "管理视频",
      action: nextAction === "generate_shot_images" || nextAction === "generate_shot_videos" ? nextAction : undefined,
      icon: Video,
    },
    {
      key: "final",
      label: "成片",
      href: projectHref(projectId, "final"),
      countLabel: `${finalVideo.enabledClipCount ?? 0} 个片段 · ${finalVideo.timelineCount ?? 0} 条时间线`,
      summary: [
        finalVideo.finalVideoVersionId ? "已有可用成片版本" : "镜头视频完成后可合成成片",
        finalVideo.stale ? "成片已过期，需要重新合成" : "可设置当前版本并下载",
      ],
      buttonLabel: "管理成片",
      icon: Film,
    },
  ];
}

function summaryOrFallback(summary: string[] | undefined, fallback: string) {
  return summary && summary.length > 0 ? summary : [fallback];
}
