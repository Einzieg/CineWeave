"use client";

import { Loader2 } from "lucide-react";
import type { Route } from "next";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const { session, hydrated, ready, setSession, clearSession } = useStudioSession();
  const router = useRouter();
  const pathname = usePathname();
  const [checking, setChecking] = useState(true);
  const validatedKey = useRef("");

  useEffect(() => {
    if (!hydrated) {
      return;
    }

    let cancelled = false;
    const next = pathname && pathname !== "/" ? `?next=${encodeURIComponent(pathname)}` : "";

    async function refreshOrRedirect() {
      if (!session.refreshToken.trim()) {
        clearSession();
        router.replace(`/login${next}` as Route);
        return;
      }
      try {
        const response = await studioApi.refreshAuth(session.refreshToken);
        if (!cancelled) {
          validatedKey.current = response.accessToken;
          setSession(sessionFromAuthResponse(response, session.currentProjectId));
          setChecking(false);
        }
      } catch {
        if (!cancelled) {
          clearSession();
          router.replace(`/login${next}` as Route);
        }
      }
    }

    if (!ready) {
      void refreshOrRedirect();
      return () => {
        cancelled = true;
      };
    }

    const key = `${session.accessToken}:${session.organizationId}`;
    if (validatedKey.current === key) {
      return () => {
        cancelled = true;
      };
    }

    studioApi
      .me(session)
      .then((response) => {
        if (cancelled) {
          return;
        }
        validatedKey.current = key;
        setSession({
          ...session,
          organizationId: response.organizationId || session.organizationId,
          workspaceId: response.workspaceId ?? session.workspaceId,
          user: response.user,
        });
        setChecking(false);
      })
      .catch((cause: unknown) => {
        if (cancelled) {
          return;
        }
        if (cause instanceof StudioApiError && cause.status === 401) {
          void refreshOrRedirect();
          return;
        }
        validatedKey.current = key;
        setChecking(false);
      });

    return () => {
      cancelled = true;
    };
  }, [
    clearSession,
    hydrated,
    pathname,
    ready,
    router,
    session,
    session.accessToken,
    session.currentProjectId,
    session.organizationId,
    session.refreshToken,
    setSession,
  ]);

  if (!hydrated || checking || !ready) {
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
