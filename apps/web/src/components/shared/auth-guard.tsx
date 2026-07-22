"use client";

import { Loader2, LogOut, RefreshCw } from "lucide-react";
import type { Route } from "next";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { authorizationGuardState, authorizationSessionKey, sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const {
    session,
    hydrated,
    ready,
    authorizationStatus,
    authorizationSessionKey: bootstrapSessionKey,
    authorizationError,
    setSession,
    clearSession,
    beginAuthorizationBootstrap,
    completeAuthorizationBootstrap,
    failAuthorizationBootstrap,
    retryAuthorizationBootstrap,
  } = useStudioSession();
  const router = useRouter();
  const pathname = usePathname();
  const refreshInFlight = useRef("");
  const currentSessionKey = authorizationSessionKey(session);
  const guardState = authorizationGuardState(session, hydrated, {
    status: authorizationStatus,
    sessionKey: bootstrapSessionKey,
  });
  const next = pathname && pathname !== "/" ? `?next=${encodeURIComponent(pathname)}` : "";

  useEffect(() => {
    if (!hydrated) {
      return;
    }

    let cancelled = false;
    if (!ready) {
      const refreshToken = session.refreshToken.trim();
      if (!refreshToken) {
        clearSession();
        router.replace(`/login${next}` as Route);
        return;
      }
      if (refreshInFlight.current === refreshToken) {
        return;
      }
      refreshInFlight.current = refreshToken;
      void studioApi.refreshAuth(refreshToken).then((response) => {
        if (!cancelled) {
          refreshInFlight.current = "";
          setSession(sessionFromAuthResponse(response, session.currentProjectId));
        }
      }).catch(() => {
        if (!cancelled) {
          refreshInFlight.current = "";
          clearSession();
          router.replace(`/login${next}` as Route);
        }
      });
      return () => {
        cancelled = true;
      };
    }

    if (authorizationStatus === "ready" && bootstrapSessionKey === currentSessionKey) {
      if (!session.user?.username) {
        router.replace(`/set-username${next}` as Route);
      }
      return;
    }
    if (authorizationStatus === "idle" || bootstrapSessionKey !== currentSessionKey) {
      beginAuthorizationBootstrap(currentSessionKey);
    }
    return () => {
      cancelled = true;
    };
  }, [
    authorizationStatus,
    beginAuthorizationBootstrap,
    bootstrapSessionKey,
    clearSession,
    currentSessionKey,
    hydrated,
    next,
    ready,
    router,
    session.currentProjectId,
    session.refreshToken,
    session.user?.username,
    setSession,
  ]);

  useEffect(() => {
    if (!hydrated || !ready || authorizationStatus !== "loading" || bootstrapSessionKey !== currentSessionKey) {
      return;
    }

    let cancelled = false;
    studioApi.me(session).then((response) => {
      if (cancelled) {
        return;
      }
      const completed = completeAuthorizationBootstrap(currentSessionKey, {
        organizationId: response.organizationId || session.organizationId,
        workspaceId: response.workspaceId ?? session.workspaceId,
        user: response.user,
        membership: response.membership,
        permissions: response.permissions,
      });
      if (completed && !response.user.username) {
        router.replace(`/set-username${next}` as Route);
      }
    }).catch((cause: unknown) => {
      if (cancelled) {
        return;
      }
      if (cause instanceof StudioApiError && cause.status === 401) {
        const refreshToken = session.refreshToken.trim();
        if (!refreshToken) {
          clearSession();
          router.replace(`/login${next}` as Route);
          return;
        }
        void studioApi.refreshAuth(refreshToken).then((response) => {
          if (!cancelled) {
            setSession(sessionFromAuthResponse(response, session.currentProjectId));
          }
        }).catch(() => {
          if (!cancelled) {
            clearSession();
            router.replace(`/login${next}` as Route);
          }
        });
        return;
      }
      failAuthorizationBootstrap(currentSessionKey, "暂时无法验证当前账号权限，请检查网络后重试");
    });

    return () => {
      cancelled = true;
    };
  }, [
    authorizationStatus,
    bootstrapSessionKey,
    clearSession,
    completeAuthorizationBootstrap,
    currentSessionKey,
    failAuthorizationBootstrap,
    hydrated,
    next,
    ready,
    router,
    session,
    session.currentProjectId,
    session.refreshToken,
    setSession,
  ]);

  if (guardState === "error") {
    return (
      <div className="grid min-h-svh place-items-center bg-background p-6">
        <div className="w-full max-w-md rounded-lg border bg-card p-6 text-center shadow-sm">
          <h1 className="text-base font-semibold">账号权限加载失败</h1>
          <p className="mt-2 text-sm text-muted-foreground">{authorizationError || "暂时无法验证当前账号权限"}</p>
          <div className="mt-5 flex justify-center gap-2">
            <Button variant="outline" onClick={() => {
              clearSession();
              router.replace(`/login${next}` as Route);
            }}>
              <LogOut className="h-4 w-4" />
              重新登录
            </Button>
            <Button onClick={retryAuthorizationBootstrap}>
              <RefreshCw className="h-4 w-4" />
              重试
            </Button>
          </div>
        </div>
      </div>
    );
  }

  if (guardState !== "ready") {
    return (
      <div className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在检查登录状态
        </span>
      </div>
    );
  }

  return <>{children}</>;
}
