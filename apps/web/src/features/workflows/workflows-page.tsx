"use client";

import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { useApiQuery } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/shared/status-badge";

export function WorkflowsPage({ projectId }: { projectId: string }) {
  const { data: workflows = [], isLoading } = useApiQuery({
    key: qk.workflowRuns(projectId),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId).then(r => r.items),
  });

  return (
    <Surface>
      <SectionTitle title="工作流" description="查看项目中的所有工作流运行记录" />
      {isLoading ? (
        <div className="p-4 space-y-2">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
      ) : (
        <div className="divide-y">
          {workflows.map((run) => (
            <div key={run.id} className="flex items-center justify-between gap-4 p-4">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{run.temporalWorkflowId}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  {new Date(run.createdAt || "").toLocaleString("zh-CN")}
                </p>
              </div>
              <StatusBadge status={run.status} />
            </div>
          ))}
          {workflows.length === 0 && (
            <p className="p-4 text-sm text-muted-foreground">暂无工作流记录</p>
          )}
        </div>
      )}
    </Surface>
  );
}
