"use client";

import { use } from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { usePathname } from "next/navigation";
import Link from "next/link";
import { AuthGuard } from "@/components/shared/auth-guard";
import { MainSidebar, MobileGlobalNav } from "@/components/layout/main-sidebar";
import { TopBar } from "@/components/layout/top-bar";
import { cn } from "@/lib/cn";
import { studioApi } from "@/lib/api-client";
import { useStudioSession, useBindCurrentProject } from "@/lib/session";
import { projectNavItems, projectHref } from "@/lib/routes";
import { useProjectEvents } from "@/lib/realtime/use-project-events";
import { ActivityDrawer } from "@/features/activity/activity-drawer";
import { AgentDrawer } from "@/features/assistant/agent-drawer";

export default function ProjectLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = use(params);

  return (
    <AuthGuard>
      <ProjectShellContent projectId={projectId}>{children}</ProjectShellContent>
    </AuthGuard>
  );
}

function ProjectShellContent({ projectId, children }: { projectId: string; children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { session, clearSession } = useStudioSession();

  useBindCurrentProject(projectId);
  useProjectEvents(projectId);

  async function logout() {
    if (session.refreshToken.trim()) {
      await studioApi.logout(session.refreshToken).catch(() => undefined);
    }
    clearSession();
    router.replace("/login" as Route);
  }

  // 从pathname提取当前segment
  const segments = pathname.split("/").filter(Boolean);
  const currentSegment = segments.length > 2 ? segments[2] : "";

  return (
    <div className="flex h-dvh overflow-hidden bg-background">
      <MainSidebar active="projects" />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <TopBar title="项目工作台" session={session} projectId={projectId} onLogout={logout} />
        <MobileGlobalNav active="projects" />

        {/* 项目内导航 */}
        <nav className="shrink-0 flex gap-1 overflow-x-auto border-b px-4 pt-3" aria-label="项目内部导航">
          {projectNavItems.map((item) => {
            const Icon = item.icon;
            const isActive = currentSegment === item.segment;
            return (
              <Link
                key={item.segment || "overview"}
                href={projectHref(projectId, item.segment) as Route}
                className={cn(
                  "flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-sm transition",
                  isActive
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground",
                )}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </Link>
            );
          })}
        </nav>

        <main className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-screen-2xl p-4 md:p-6">{children}</div>
        </main>
      </div>

      {/* AI助手常驻面板 */}
      <AgentDrawer projectId={projectId} />
      <ActivityDrawer projectId={projectId} />
    </div>
  );
}
