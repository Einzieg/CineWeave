"use client";

import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronDown, ChevronRight, Play, AlertCircle, CheckCircle2, Clock } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { contentTypeLabel, productionFieldLabel, projectTypeLabel, reviewSeverityLabel, statusLabel } from "@/lib/labels";
import { nextProductionAction, productionActionLabel, productionRefreshKeys, runProductionAction } from "@/features/production/production-actions";

type StageRow = {
  stage: string;
  label: string;
  status?: string;
} & Record<string, unknown>;

export function ProjectOverviewPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [expandedStage, setExpandedStage] = useState<string | null>(null);

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
      return isRunning ? 5000 : false; // 有运行中的任务时5秒轮询
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

  // 将stages对象转为数组用于渲染
  const stagesArray: StageRow[] = productionStatus?.stages ? [
    { stage: "source", label: "原文与事件", ...productionStatus.stages.source },
    { stage: "assets", label: "资产管理", ...productionStatus.stages.assets },
    { stage: "storyboard", label: "分镜设计", ...productionStatus.stages.storyboard },
    { stage: "shotAssets", label: "镜头资产", ...productionStatus.stages.shotAssets },
    { stage: "shotImages", label: "镜头图片", ...productionStatus.stages.shotImages },
    { stage: "shotVideos", label: "镜头视频", ...productionStatus.stages.shotVideos },
    { stage: "finalVideo", label: "最终成片", ...productionStatus.stages.finalVideo },
  ] : [];

  const overallProgress = productionStatus?.overall?.progress || 0;
  const nextAction = productionStatus ? nextProductionAction(productionStatus) : "";

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

      {/* 生产流水线 */}
      <Surface>
        <SectionTitle title="生产流水线" description="点击阶段查看详情" />
        <div className="p-5">
          {/* 总体进度 */}
          <div className="mb-6">
            <div className="mb-2 flex items-center justify-between text-sm">
              <span className="font-medium">整体进度</span>
              <span className="text-muted-foreground">{Math.round(overallProgress)}%</span>
            </div>
            <Progress value={overallProgress} className="h-2" />
          </div>

          {/* 阶段列表 */}
          <div className="space-y-2">
            {stagesArray.map((stage, index) => (
              <Collapsible
                key={stage.stage || index}
                open={expandedStage === stage.stage}
                onOpenChange={(open) => setExpandedStage(open ? stage.stage : null)}
              >
                <CollapsibleTrigger asChild>
                  <button
                    className={cn(
                      "flex w-full items-center gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/50",
                      expandedStage === stage.stage && "bg-muted/50"
                    )}
                  >
                    {expandedStage === stage.stage ? (
                      <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                    )}
                    <StageIcon status={stage.status || "pending"} />
                    <div className="flex-1">
                      <div className="font-medium">{stage.label}</div>
                      <div className="text-xs text-muted-foreground">
                        {statusLabel(stage.status || "pending")}
                      </div>
                    </div>
                    <StatusBadge status={stage.status || "pending"} />
                  </button>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className="ml-11 mt-2 space-y-2 rounded-lg border bg-muted/20 p-4">
                    {/* 显示阶段的统计信息 */}
                    {Object.entries(stage)
                      .filter(([key]) => !["stage", "label", "status"].includes(key))
                      .map(([key, value]) => (
                        <div key={key} className="flex justify-between text-sm">
                          <span className="text-muted-foreground">{productionFieldLabel(key)}</span>
                          <span className="font-medium">{String(value)}</span>
                        </div>
                      ))}
                  </div>
                </CollapsibleContent>
              </Collapsible>
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

function StageIcon({ status }: { status: string }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-500" />;
    case "running":
      return <Clock className="h-5 w-5 shrink-0 animate-pulse text-cyan-500" />;
    default:
      return <div className="h-5 w-5 shrink-0 rounded-full border-2 border-muted-foreground" />;
  }
}
