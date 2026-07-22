"use client";

import { CheckCircle2, KeyRound, Loader2 } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AuthPageShell } from "@/features/auth/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";

export function ResetPasswordPage() {
  const router = useRouter();
  const initialized = useRef(false);
  const [resetToken, setResetToken] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [completed, setCompleted] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
    const token = fragment.get("token")?.trim() ?? "";
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
    void Promise.resolve().then(() => {
      if (!token) {
        setError("重置链接缺少有效令牌，请向组织管理员获取新链接。");
        return;
      }
      setResetToken(token);
    });
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (password.length < 8 || password.length > 72) {
      setError("新密码需包含 8 至 72 个字符。");
      return;
    }
    if (password !== confirmation) {
      setError("两次输入的密码不一致。");
      return;
    }
    if (!resetToken) {
      setError("重置链接无效，请向组织管理员获取新链接。");
      return;
    }
    setBusy(true);
    try {
      await studioApi.completePasswordReset(resetToken, password);
      setResetToken("");
      setPassword("");
      setConfirmation("");
      setCompleted(true);
    } catch (cause) {
      setError(cause instanceof StudioApiError ? cause.message : "密码重置失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthPageShell title="设置新密码" description="完成后请使用用户名或邮箱重新登录。">
      {completed ? (
        <div className="grid gap-5 text-center">
          <CheckCircle2 className="mx-auto h-10 w-10 text-emerald-600" />
          <div><p className="text-sm font-medium">密码已更新</p><p className="mt-1 text-xs leading-5 text-muted-foreground">该重置链接已经失效，所有旧会话均无法继续使用。</p></div>
          <Button onClick={() => router.replace("/login" as Route)}>返回登录</Button>
        </div>
      ) : (
        <form className="grid gap-4" onSubmit={submit}>
          <div className="grid gap-2">
            <Label htmlFor="new-password">新密码</Label>
            <Input id="new-password" type="password" autoComplete="new-password" minLength={8} maxLength={72} value={password} onChange={(event) => setPassword(event.target.value)} required />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="confirm-password">确认新密码</Label>
            <Input id="confirm-password" type="password" autoComplete="new-password" minLength={8} maxLength={72} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required />
          </div>
          {error ? <p className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p> : null}
          <Button type="submit" disabled={busy || !resetToken} className="w-full">
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <KeyRound className="mr-2 h-4 w-4" />}
            保存新密码
          </Button>
        </form>
      )}
    </AuthPageShell>
  );
}
