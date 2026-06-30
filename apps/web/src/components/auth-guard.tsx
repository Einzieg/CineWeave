"use client";

import { Loader2 } from "lucide-react";
import type { Route } from "next";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef } from "react";
import { studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const { session, hydrated, ready, setSession, clearSession } = useStudioSession();
  const router = useRouter();
  const pathname = usePathname();
  const refreshStarted = useRef(false);

  useEffect(() => {
    if (!hydrated || ready) {
      return;
    }
    const next = pathname && pathname !== "/" ? `?next=${encodeURIComponent(pathname)}` : "";
    if (!session.refreshToken.trim()) {
      router.replace(`/login${next}` as Route);
      return;
    }
    if (refreshStarted.current) {
      return;
    }
    refreshStarted.current = true;
    studioApi
      .refreshAuth(session.refreshToken)
      .then((response) => {
        setSession(sessionFromAuthResponse(response, session.currentProjectId));
      })
      .catch(() => {
        clearSession();
        router.replace(`/login${next}` as Route);
      });
  }, [clearSession, hydrated, pathname, ready, router, session.currentProjectId, session.refreshToken, setSession]);

  if (!hydrated || !ready) {
    return (
      <div className="grid min-h-svh place-items-center bg-slate-50 text-sm text-slate-500">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="animate-spin" size={16} />
          正在检查登录状态
        </span>
      </div>
    );
  }

  return <>{children}</>;
}
