"use client";

import { Loader2, WandSparkles } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { AuthError, AuthField, AuthForm, AuthPageShell } from "@/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export default function SetupPage() {
  const router = useRouter();
  const { setSession } = useStudioSession();
  const [loadingState, setLoadingState] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    email: "",
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
        password: form.password,
        displayName: form.displayName,
        organizationName: form.organizationName,
        workspaceName: form.workspaceName,
      });
      setSession(sessionFromAuthResponse(response));
      router.replace("/dashboard" as Route);
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
      <main className="grid min-h-svh place-items-center bg-slate-50 text-sm text-slate-500">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="animate-spin" size={16} />
          正在检查初始化状态
        </span>
      </main>
    );
  }

  return (
    <AuthPageShell title="初始化影织" description="首次启动需要创建管理员账号，之后请使用该账号登录。">
      <AuthForm onSubmit={submit}>
        <AuthField autoComplete="email" label="管理员邮箱" onChange={(email) => setForm({ ...form, email })} type="email" value={form.email} />
        <AuthField autoComplete="name" label="管理员姓名" onChange={(displayName) => setForm({ ...form, displayName })} value={form.displayName} />
        <AuthField label="组织名称" onChange={(organizationName) => setForm({ ...form, organizationName })} value={form.organizationName} />
        <AuthField label="默认工作区名称" onChange={(workspaceName) => setForm({ ...form, workspaceName })} value={form.workspaceName} />
        <AuthField autoComplete="new-password" label="登录密码" onChange={(password) => setForm({ ...form, password })} type="password" value={form.password} />
        <AuthField autoComplete="new-password" label="确认密码" onChange={(confirmPassword) => setForm({ ...form, confirmPassword })} type="password" value={form.confirmPassword} />
        <AuthError message={error} />
        <button className="studio-button studio-button-primary w-full" disabled={busy} type="submit">
          {busy ? <Loader2 className="animate-spin" size={16} /> : <WandSparkles size={16} />}
          创建管理员并进入
        </button>
      </AuthForm>
    </AuthPageShell>
  );
}
