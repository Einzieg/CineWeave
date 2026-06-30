"use client";

import { Loader2, LogIn } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { AuthError, AuthField, AuthForm, AuthPageShell } from "@/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export default function LoginPage() {
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
      <main className="grid min-h-svh place-items-center bg-slate-50 text-sm text-slate-500">
        <span className="inline-flex items-center gap-2">
          <Loader2 className="animate-spin" size={16} />
          正在检查系统状态
        </span>
      </main>
    );
  }

  return (
    <AuthPageShell title="登录影织" description="进入你的 AI 视频创作工作台。">
      <AuthForm onSubmit={submit}>
        <AuthField autoComplete="email" label="邮箱" onChange={setEmail} type="email" value={email} />
        <AuthField autoComplete="current-password" label="密码" onChange={setPassword} type="password" value={password} />
        <AuthError message={error} />
        <button className="studio-button studio-button-primary w-full" disabled={busy} type="submit">
          {busy ? <Loader2 className="animate-spin" size={16} /> : <LogIn size={16} />}
          登录
        </button>
        <p className="text-center text-sm text-slate-500">
          首次启动？
          <Link className="font-medium text-blue-700 hover:text-blue-800" href={"/setup" as Route}>
            请先初始化管理员账号
          </Link>
        </p>
      </AuthForm>
    </AuthPageShell>
  );
}

function nextPath() {
  if (typeof window === "undefined") {
    return "/dashboard";
  }
  const value = new URLSearchParams(window.location.search).get("next");
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "/dashboard";
  }
  return value;
}
