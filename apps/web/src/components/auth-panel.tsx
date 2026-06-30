"use client";

import { Loader2, LogIn, UserPlus } from "lucide-react";
import type { FormEvent } from "react";
import { useState } from "react";
import { studioApi } from "@/lib/api-client";
import { emptySession, useStudioSession } from "@/lib/session";
import type { StudioSession } from "@/lib/types";

type AuthMode = "login" | "register";

export function AuthPanel() {
  const { setSession } = useStudioSession();
  const [mode, setMode] = useState<AuthMode>("login");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    email: "",
    password: "",
    displayName: "",
    organizationName: "",
  });

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");

    const email = form.email.trim();
    const password = form.password;
    if (!email || !password) {
      setError("请填写邮箱和密码。");
      return;
    }
    if (mode === "register" && password.length < 8) {
      setError("注册密码至少需要 8 位。");
      return;
    }

    setBusy(true);
    try {
      const auth =
        mode === "login"
          ? await studioApi.login({ email, password })
          : await studioApi.register({
              email,
              password,
              displayName: form.displayName.trim(),
              organizationName: form.organizationName.trim(),
            });

      const nextSession: StudioSession = {
        ...emptySession,
        accessToken: auth.accessToken,
        currentUserId: auth.user.id,
        organizationId: auth.organizationId ?? "",
        workspaceId: "",
        currentProjectId: "",
      };

      if (!nextSession.organizationId) {
        throw new Error("当前账号没有可用组织，请先完成组织配置后再登录。");
      }

      try {
        const workspaces = await studioApi.listWorkspaces(nextSession);
        nextSession.workspaceId = workspaces.items[0]?.id ?? "";
      } catch {
        // 登录已成功；工作区列表会在顶栏继续尝试加载。
      }

      setSession(nextSession);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "登录失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  const Icon = mode === "login" ? LogIn : UserPlus;
  const actionLabel = mode === "login" ? "登录" : "创建账号并登录";

  return (
    <div className="mx-auto grid min-h-[420px] max-w-5xl place-items-center px-4 py-10">
      <div className="w-full max-w-md rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <div className="flex items-start gap-3">
          <div className="grid h-11 w-11 place-items-center rounded-xl bg-cyan-50 text-cyan-700">
            <Icon size={20} />
          </div>
          <div>
            <h2 className="text-xl font-semibold text-slate-950">登录影织 Studio</h2>
            <p className="mt-1 text-sm leading-6 text-slate-500">使用账号密码完成鉴权，系统会自动获取访问令牌、组织和默认工作区。</p>
          </div>
        </div>

        <form className="mt-6 grid gap-4" onSubmit={submit}>
          <label className="grid gap-1.5 text-sm font-medium text-slate-700">
            邮箱
            <input
              autoComplete="email"
              className="studio-input w-full"
              onChange={(event) => setForm({ ...form, email: event.target.value })}
              placeholder="name@example.com"
              type="email"
              value={form.email}
            />
          </label>
          <label className="grid gap-1.5 text-sm font-medium text-slate-700">
            密码
            <input
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              className="studio-input w-full"
              onChange={(event) => setForm({ ...form, password: event.target.value })}
              placeholder="输入密码"
              type="password"
              value={form.password}
            />
          </label>

          {mode === "register" ? (
            <>
              <label className="grid gap-1.5 text-sm font-medium text-slate-700">
                显示名称
                <input
                  autoComplete="name"
                  className="studio-input w-full"
                  onChange={(event) => setForm({ ...form, displayName: event.target.value })}
                  placeholder="你的名字或团队名称"
                  value={form.displayName}
                />
              </label>
              <label className="grid gap-1.5 text-sm font-medium text-slate-700">
                组织名称
                <input
                  className="studio-input w-full"
                  onChange={(event) => setForm({ ...form, organizationName: event.target.value })}
                  placeholder="默认会创建一个工作区"
                  value={form.organizationName}
                />
              </label>
            </>
          ) : null}

          {error ? <p className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</p> : null}

          <button className="studio-button studio-button-primary w-full" disabled={busy} type="submit">
            {busy ? <Loader2 className="animate-spin" size={16} /> : <Icon size={16} />}
            {busy ? "正在处理" : actionLabel}
          </button>
        </form>

        <div className="mt-5 border-t border-slate-200 pt-4 text-center text-sm text-slate-500">
          {mode === "login" ? "还没有账号？" : "已经有账号？"}
          <button
            className="ml-1 font-medium text-cyan-700 hover:text-cyan-800"
            onClick={() => {
              setError("");
              setMode(mode === "login" ? "register" : "login");
            }}
            type="button"
          >
            {mode === "login" ? "创建账号" : "返回登录"}
          </button>
        </div>
      </div>
    </div>
  );
}
