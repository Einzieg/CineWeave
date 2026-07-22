"use client";

import { Loader2, WandSparkles } from "lucide-react";
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

export function SetupPage() {
  const router = useRouter();
  const { setSession } = useStudioSession();
  const [loadingState, setLoadingState] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    email: "",
    username: "",
    displayName: "",
    password: "",
    confirmPassword: "",
    organizationName: "影织组织",
    workspaceName: "默认工作区",
  });

  useEffect(() => {
    let cancelled = false;
    studioApi
      .getSetupState()
      .then((state) => {
        if (cancelled) {
          return;
        }
        if (!state.needsSetup) {
          router.replace("/login" as Route);
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

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (form.password.length < 8) {
      setError("密码至少需要 8 位。");
      return;
    }
    if (form.password !== form.confirmPassword) {
      setError("两次输入的密码不一致。");
      return;
    }
    setBusy(true);
    try {
      const response = await studioApi.setupSystem({
        email: form.email,
        username: form.username,
        password: form.password,
        displayName: form.displayName,
        organizationName: form.organizationName,
        workspaceName: form.workspaceName,
      });
      setSession(sessionFromAuthResponse(response));
      router.replace("/projects" as Route);
    } catch (cause) {
      if (cause instanceof StudioApiError && cause.code === "SETUP_ALREADY_COMPLETED") {
        router.replace("/login" as Route);
        return;
      }
      setError(cause instanceof StudioApiError ? cause.message : "初始化失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  if (loadingState) {
    return (
      <main className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在检查初始化状态
        </span>
      </main>
    );
  }

  return (
    <AuthPageShell title="初始化影织" description="首次启动需要创建管理员账号，之后请使用该账号登录。">
      <form className="grid gap-4" onSubmit={submit}>
        <div className="grid gap-2">
          <Label htmlFor="email">管理员邮箱</Label>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="username">管理员用户名</Label>
          <Input
            id="username"
            autoComplete="username"
            minLength={3}
            maxLength={32}
            pattern="[A-Za-z0-9](?:[A-Za-z0-9_-]{1,30}[A-Za-z0-9])?"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
            required
          />
          <p className="text-xs text-muted-foreground">3–32 位字母、数字、下划线或短横线，以字母或数字开头和结尾。</p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="displayName">管理员姓名</Label>
          <Input
            id="displayName"
            autoComplete="name"
            value={form.displayName}
            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="organizationName">组织名称</Label>
          <Input
            id="organizationName"
            value={form.organizationName}
            onChange={(e) => setForm({ ...form, organizationName: e.target.value })}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="workspaceName">默认工作区名称</Label>
          <Input
            id="workspaceName"
            value={form.workspaceName}
            onChange={(e) => setForm({ ...form, workspaceName: e.target.value })}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="password">登录密码</Label>
          <Input
            id="password"
            type="password"
            autoComplete="new-password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="confirmPassword">确认密码</Label>
          <Input
            id="confirmPassword"
            type="password"
            autoComplete="new-password"
            value={form.confirmPassword}
            onChange={(e) => setForm({ ...form, confirmPassword: e.target.value })}
            required
          />
        </div>
        {error ? (
          <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <WandSparkles className="mr-2 h-4 w-4" />}
          创建管理员并进入
        </Button>
      </form>
    </AuthPageShell>
  );
}
