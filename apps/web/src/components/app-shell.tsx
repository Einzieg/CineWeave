"use client";

import { MainSidebar, MobileGlobalNav } from "@/components/main-sidebar";
import { ProjectNav } from "@/components/project-nav";
import { TopBar } from "@/components/top-bar";
import { AuthGuard } from "@/components/auth-guard";
import { studioApi } from "@/lib/api-client";
import { useBindCurrentProject, useStudioSession } from "@/lib/session";
import type { GlobalSection, ProjectSection } from "@/lib/routes";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";

export function AppShell({
  active,
  title,
  description,
  projectId,
  projectSection,
  children,
}: {
  active: GlobalSection;
  title: string;
  description?: string;
  projectId?: string;
  projectSection?: ProjectSection;
  children: ReactNode;
}) {
  return (
    <AuthGuard>
      <AppShellContent active={active} title={title} description={description} projectId={projectId} projectSection={projectSection}>
        {children}
      </AppShellContent>
    </AuthGuard>
  );
}

function AppShellContent({
  active,
  title,
  description,
  projectId,
  projectSection,
  children,
}: {
  active: GlobalSection;
  title: string;
  description?: string;
  projectId?: string;
  projectSection?: ProjectSection;
  children: ReactNode;
}) {
  const router = useRouter();
  const { session, clearSession } = useStudioSession();
  useBindCurrentProject(projectId);

  async function logout() {
    if (session.refreshToken.trim()) {
      await studioApi.logout(session.refreshToken).catch(() => undefined);
    }
    clearSession();
    router.replace("/login" as Route);
  }

  return (
    <div className="min-h-svh bg-slate-50 text-slate-950">
      <div className="flex min-h-svh">
        <MainSidebar active={active} />
        <div className="min-w-0 flex-1">
          <TopBar title={title} description={description} session={session} onLogout={logout} />
          <MobileGlobalNav active={active} />
          {projectId && projectSection !== undefined ? <ProjectNav projectId={projectId} active={projectSection} /> : null}
          <main className="mx-auto w-full max-w-7xl px-4 py-6">{children}</main>
        </div>
      </div>
    </div>
  );
}

export function Surface({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <section className={`rounded-lg border border-slate-200 bg-white shadow-sm shadow-slate-200/60 ${className}`}>{children}</section>;
}

export function SectionTitle({ title, description }: { title: string; description?: string }) {
  return (
    <div className="border-b border-slate-200 px-4 py-3">
      <h2 className="text-sm font-semibold text-slate-950">{title}</h2>
      {description ? <p className="mt-1 text-sm text-slate-500">{description}</p> : null}
    </div>
  );
}
