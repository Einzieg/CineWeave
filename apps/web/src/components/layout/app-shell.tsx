"use client";

import { useRouter } from "next/navigation";
import type { Route } from "next";
import type { ReactNode } from "react";
import { AuthGuard } from "@/components/shared/auth-guard";
import { MainSidebar, MobileGlobalNav } from "@/components/layout/main-sidebar";
import { TopBar } from "@/components/layout/top-bar";
import { studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";
import type { GlobalSection } from "@/lib/routes";

/**
 * 全局应用外壳。保持与旧 AppShell 的 props 接口兼容,
 * studio-pages.tsx 中的旧页面包装可继续工作。
 */
export function AppShell({
  active,
  title,
  description,
  children,
}: {
  active: GlobalSection;
  title: string;
  description?: string;
  projectId?: string;
  projectSection?: string;
  children: ReactNode;
}) {
  return (
    <AuthGuard>
      <AppShellContent active={active} title={title} description={description}>
        {children}
      </AppShellContent>
    </AuthGuard>
  );
}

function AppShellContent({
  active,
  title,
  description,
  children,
}: {
  active: GlobalSection;
  title: string;
  description?: string;
  children: ReactNode;
}) {
  const router = useRouter();
  const { session, clearSession } = useStudioSession();

  async function logout() {
    if (session.refreshToken.trim()) {
      await studioApi.logout(session.refreshToken).catch(() => undefined);
    }
    clearSession();
    router.replace("/login" as Route);
  }

  return (
    <div className="flex min-h-screen bg-background">
      <MainSidebar active={active} />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar title={title} description={description} session={session} onLogout={logout} hideProjectActions />
        <MobileGlobalNav active={active} />
        <main className="flex-1">
          <div className="mx-auto w-full max-w-screen-2xl p-4 md:p-6">{children}</div>
        </main>
      </div>
    </div>
  );
}

/** 旧版 Surface 组件,保持接口兼容 */
export function Surface({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <section className={`rounded-lg border bg-card shadow-sm ${className}`}>{children}</section>;
}

/** 旧版 SectionTitle 组件,保持接口兼容 */
export function SectionTitle({ title, description }: { title: string; description?: string }) {
  return (
    <div className="border-b px-4 py-3">
      <h2 className="text-sm font-semibold">{title}</h2>
      {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
    </div>
  );
}
