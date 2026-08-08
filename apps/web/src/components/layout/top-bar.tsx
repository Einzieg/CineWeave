"use client";

import { Building2, ListChecks, LogOut, UserCircle2, Bot, PanelRightClose } from "lucide-react";
import { editionEntry } from "@cineweave/edition-entry";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { ThemeToggle } from "@/components/layout/theme-toggle";
import {
  editionFeatureAllowed,
  useEditionEntitlements,
} from "@/edition/use-entitlements";
import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiQuery } from "@/lib/query/use-api";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import { useWorkflowTerminalRefresh } from "@/lib/realtime/use-workflow-terminal-refresh";
import { useSessionDetails } from "@/lib/session-details";
import { useUiStore } from "@/lib/stores/ui-store";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import { isActiveWorkflowStatus } from "@/lib/workflow-status";
import type { ProjectControlCommandStatus, StudioSession } from "@/lib/types";

const activeWorkflowListShape = { status: "active", view: "activity", limit: 100 } as const;
const activeProjectControlStatuses: ProjectControlCommandStatus[] = ["queued", "running", "waiting_workflow", "waiting_input"];
const activeProjectControlListShape = { statuses: activeProjectControlStatuses, view: "activity", limit: 50 } as const;

export function TopBar({
  title,
  description,
  session,
  onLogout,
  projectId: activeProjectId,
  hideProjectActions,
}: {
  title: string;
  description?: string;
  session: StudioSession;
  onLogout: () => void;
  projectId?: string;
  hideProjectActions?: boolean;
}) {
  const displayName = session.user?.displayName?.trim() || session.user?.email || "已登录用户";
  const details = useSessionDetails();
  const { setActivityOpen } = useUiStore();
  const isAgentOpen = useAgentDrawerStore((state) => state.isOpen);
  const toggleAgent = useAgentDrawerStore((state) => state.toggle);
  const projectId = activeProjectId || session.currentProjectId;
  const pollingFallback = useProjectPollingFallback(projectId);
  const { data: workflowRunPage, isFetched: workflowRunsReady } = useApiQuery({
    key: qk.workflowRuns(projectId || "none", activeWorkflowListShape),
    queryFn: (apiSession) => studioApi.listWorkflowRuns(apiSession, projectId, activeWorkflowListShape),
    enabled: !hideProjectActions && !!projectId,
    refetchInterval: (query) =>
      pollingFallback && query.state.data?.items.some((run) => isActiveWorkflowStatus(run.status)) ? 5000 : false,
  });
  const { data: projectControlCommandPage } = useApiQuery({
    key: qk.projectControlCommands(projectId || "none", activeProjectControlListShape),
    queryFn: (apiSession) => studioApi.listProjectControlCommands(apiSession, projectId || "", activeProjectControlListShape),
    enabled: !hideProjectActions && !!projectId,
    refetchInterval: (query) => pollingFallback && (query.state.data?.items.length ?? 0) > 0 ? 5000 : false,
  });
  const workflowRuns = workflowRunPage?.items ?? [];
  useWorkflowTerminalRefresh(projectId || "", workflowRuns, workflowRunsReady);
  const activeCommands = projectControlCommandPage?.items ?? [];
  const linkedWorkflowRunIds = new Set(activeCommands.flatMap((command) => command.workflowRunIds ?? []));
  const activeWorkflowCount = workflowRuns.filter((run) => isActiveWorkflowStatus(run.status) && !linkedWorkflowRunIds.has(run.id)).length;
  const activeActivityCount = activeCommands.length + activeWorkflowCount;
  const entitlements = useEditionEntitlements(editionEntry.topBarItems.length > 0);
  const topBarItems = editionEntry.topBarItems.filter((item) =>
    editionFeatureAllowed(entitlements.data, item.featureKey),
  );

  return (
    <header className="sticky top-0 z-30 shrink-0 border-b bg-card/80 backdrop-blur-xl">
      <div className="mx-auto flex h-14 max-w-screen-2xl items-center justify-between gap-4 px-4">
        <div className="flex min-w-0 items-center gap-3">
          <h1 className="truncate text-base font-semibold tracking-tight">{title}</h1>
          {description ? <span className="hidden text-sm text-muted-foreground lg:inline">{description}</span> : null}
        </div>

        <div className="flex items-center gap-2">
          {!hideProjectActions && projectId && (
            <>
              <Button
                variant={isAgentOpen ? "secondary" : "ghost"}
                size="sm"
                className="gap-2"
                onClick={() => toggleAgent()}
              >
                {isAgentOpen ? <PanelRightClose className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
                <span className="hidden sm:inline">AI助手</span>
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="gap-2"
                onClick={() => setActivityOpen(true)}
              >
                <ListChecks className="h-4 w-4" />
                <span className="hidden sm:inline">任务活动</span>
                <Badge variant="secondary" className="ml-1 h-5 px-1.5 text-xs">
                  {activeActivityCount}
                </Badge>
              </Button>
              <Separator orientation="vertical" className="h-6" />
            </>
          )}

          <div className="hidden items-center gap-2 rounded-md border bg-muted/40 px-2.5 py-1.5 text-xs lg:flex">
            <Building2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="font-medium">{details.organizationName || "组织"}</span>
          </div>

          {topBarItems.map((item) => {
            const Component = item.component;
            return <Component key={item.key} />;
          })}

          <div className="hidden items-center gap-2 rounded-md border bg-muted/40 px-2.5 py-1.5 text-xs lg:flex">
            <UserCircle2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="font-medium">{displayName}</span>
          </div>

          <ThemeToggle />

          <Button variant="ghost" size="sm" onClick={onLogout} className="gap-2">
            <LogOut className="h-4 w-4" />
            <span className="hidden sm:inline">退出</span>
          </Button>
        </div>
      </div>
    </header>
  );
}
