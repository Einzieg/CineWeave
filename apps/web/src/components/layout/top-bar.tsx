"use client";

import { Building2, ListChecks, LogOut, UserCircle2, Bot } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { ThemeToggle } from "@/components/layout/theme-toggle";
import { useSessionDetails } from "@/lib/session-details";
import { useUiStore } from "@/lib/stores/ui-store";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import type { StudioSession } from "@/lib/types";

export function TopBar({
  title,
  description,
  session,
  onLogout,
  hideProjectActions,
}: {
  title: string;
  description?: string;
  session: StudioSession;
  onLogout: () => void;
  hideProjectActions?: boolean;
}) {
  const displayName = session.user?.displayName?.trim() || session.user?.email || "已登录用户";
  const details = useSessionDetails();
  const { setActivityOpen } = useUiStore();
  const { open: openAgent } = useAgentDrawerStore();

  return (
    <header className="sticky top-0 z-30 border-b bg-card/80 backdrop-blur-xl">
      <div className="mx-auto flex h-14 max-w-screen-2xl items-center justify-between gap-4 px-4">
        <div className="flex min-w-0 items-center gap-3">
          <h1 className="truncate text-base font-semibold tracking-tight">{title}</h1>
          {description ? <span className="hidden text-sm text-muted-foreground lg:inline">{description}</span> : null}
        </div>

        <div className="flex items-center gap-2">
          {!hideProjectActions && session.currentProjectId && (
            <>
              <Button
                variant="ghost"
                size="sm"
                className="gap-2"
                onClick={() => openAgent()}
              >
                <Bot className="h-4 w-4" />
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
                  0
                </Badge>
              </Button>
              <Separator orientation="vertical" className="h-6" />
            </>
          )}

          <div className="hidden items-center gap-2 rounded-md border bg-muted/40 px-2.5 py-1.5 text-xs lg:flex">
            <Building2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="font-medium">{details.organizationName || "组织"}</span>
          </div>

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
