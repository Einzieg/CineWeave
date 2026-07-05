"use client";

import { Loader2, LogIn } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AuthPageShell } from "@/features/auth/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export function LoginPage() {
  const router = useRouter();
  const { hydrated, ready, setSession } = useStudioSession();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loadingState, setLoadingState] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    studioApi
      .getSetupState()
      .then((state) => {
        if (cancelled) {
          return;
        }
        if (state.needsSetup) {
          router.replace("/setup" as Route);
          return;
        }
        setLoadingState(false);
      })
      .catch(() => {
        if (!cancelled) {
          setLoadingState(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

  useEffect(() => {
    if (hydrated && ready) {
      router.replace(nextPath() as Route);
    }
  }, [hydrated, ready, router]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      const response = await studioApi.login({ email, password });
      setSession(sessionFromAuthResponse(response));
      router.replace(nextPath() as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? "邮箱或密码不正确。" : "登录失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  if (loadingState || !hydrated) {
    return (
      <main className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在检查系统状态
        </span>
      </main>
    );
  }

  return (
    <AuthPageShell title="登录影织" description="进入你的 AI 视频创作工作台。">
      <form className="grid gap-4" onSubmit={submit}>
        <div className="grid gap-2">
          <Label htmlFor="email">邮箱</Label>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">密码</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </div>
        {error ? (
          <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <LogIn className="mr-2 h-4 w-4" />}
          登录
        </Button>
      </form>
    </AuthPageShell>
  );
}

function nextPath() {
  if (typeof window === "undefined") {
    return "/projects";
  }
  const value = new URLSearchParams(window.location.search).get("next");
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "/projects";
  }
  return value;
}
