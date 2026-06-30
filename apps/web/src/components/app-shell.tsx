"use client";

import { MainSidebar, MobileGlobalNav } from "@/components/main-sidebar";
import { ProjectNav } from "@/components/project-nav";
import { TopBar } from "@/components/top-bar";
import { StudioSessionProvider, useBindCurrentProject, useStudioSession } from "@/lib/session";
import type { GlobalSection, ProjectSection } from "@/lib/routes";
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
    <StudioSessionProvider>
      <AppShellContent active={active} title={title} description={description} projectId={projectId} projectSection={projectSection}>
        {children}
      </AppShellContent>
    </StudioSessionProvider>
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
  const { session, updateSession } = useStudioSession();
  useBindCurrentProject(projectId);
  return (
    <div className="min-h-svh bg-zinc-950 text-zinc-100">
      <div className="flex min-h-svh">
        <MainSidebar active={active} />
        <div className="min-w-0 flex-1">
          <TopBar title={title} description={description} session={session} onSessionChange={updateSession} />
          <MobileGlobalNav active={active} />
          {projectId && projectSection !== undefined ? <ProjectNav projectId={projectId} active={projectSection} /> : null}
          <main className="mx-auto w-full max-w-7xl px-4 py-6">{children}</main>
        </div>
      </div>
    </div>
  );
}

export function Surface({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <section className={`rounded-lg border border-white/10 bg-white/[0.04] ${className}`}>{children}</section>;
}

export function SectionTitle({ title, description }: { title: string; description?: string }) {
  return (
    <div className="border-b border-white/10 px-4 py-3">
      <h2 className="text-sm font-semibold text-zinc-100">{title}</h2>
      {description ? <p className="mt-1 text-sm text-zinc-500">{description}</p> : null}
    </div>
  );
}
