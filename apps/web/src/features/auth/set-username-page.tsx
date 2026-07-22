"use client";

import { Loader2, UserRoundCheck } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AuthPageShell } from "@/features/auth/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";

export function SetUsernamePage() {
  const router = useRouter();
  const { hydrated, ready, session, updateSession } = useStudioSession();
  const [username, setUsername] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!hydrated) return;
    if (!ready) {
      router.replace("/login" as Route);
      return;
    }
    if (session.user?.username) {
      router.replace(nextPath() as Route);
    }
  }, [hydrated, ready, router, session.user?.username]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const user = await studioApi.setInitialUsername(session, username);
      updateSession({ user });
      router.replace(nextPath() as Route);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "用户名设置失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthPageShell title="设置用户名" description="用户名用于登录，设置后暂不支持修改。">
      <form className="grid gap-4" onSubmit={submit}>
        <div className="grid gap-2">
          <Label htmlFor="username">用户名</Label>
          <Input
            id="username"
            autoComplete="username"
            minLength={3}
            maxLength={32}
            pattern="[A-Za-z0-9](?:[A-Za-z0-9_-]{1,30}[A-Za-z0-9])?"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            required
          />
          <p className="text-xs text-muted-foreground">3–32 位字母、数字、下划线或短横线，以字母或数字开头和结尾。</p>
        </div>
        {error ? <p className="text-sm text-destructive">{error}</p> : null}
        <Button type="submit" disabled={busy || !username.trim()}>
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <UserRoundCheck className="mr-2 h-4 w-4" />}
          保存用户名
        </Button>
      </form>
    </AuthPageShell>
  );
}

function nextPath() {
  if (typeof window === "undefined") return "/projects";
  const value = new URLSearchParams(window.location.search).get("next");
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/projects";
  return value;
}
