"use client";

import { Building2, CheckCircle2, Loader2, LogIn, UserPlus } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AuthPageShell } from "@/features/auth/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";
import type { AuthResponse, OrganizationChoice, OrganizationInvitation, StudioSession } from "@/lib/types";

export function AcceptInvitationPage() {
  const router = useRouter();
  const { hydrated, ready, session, setSession, clearSession } = useStudioSession();
  const initialized = useRef(false);
  const [invitationToken, setInvitationToken] = useState("");
  const [invitation, setInvitation] = useState<OrganizationInvitation | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [registrationPassword, setRegistrationPassword] = useState("");
  const [organizationSelection, setOrganizationSelection] = useState<{
    token: string;
    organizations: OrganizationChoice[];
  } | null>(null);

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
    const token = fragment.get("token")?.trim() ?? "";
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
    void Promise.resolve().then(() => {
      if (!token) {
        setError("邀请链接缺少有效令牌，请向组织管理员获取新链接。");
        setLoading(false);
        return;
      }
      setInvitationToken(token);
      studioApi
        .resolveOrganizationInvitation(token)
        .then((resolved) => {
          setInvitation(resolved);
          setLoading(false);
        })
        .catch((cause) => {
          setError(apiErrorMessage(cause));
          setLoading(false);
        });
    });
  }, []);

  async function finishAcceptance(activeSession: StudioSession) {
    const response = await studioApi.acceptOrganizationInvitation(activeSession, invitationToken);
    finish(response);
  }

  function finish(response: AuthResponse) {
    setSession(sessionFromAuthResponse(response));
    router.replace((response.user.username ? "/projects" : "/set-username") as Route);
  }

  async function acceptCurrentAccount() {
    setBusy(true);
    setError("");
    try {
      await finishAcceptance(session);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function loginAndAccept(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const response = await studioApi.login({ identifier, password });
      if (response.requiresOrganizationSelection) {
        setOrganizationSelection({
          token: response.organizationSelectionToken,
          organizations: response.organizations,
        });
        return;
      }
      await finishAcceptance(sessionFromAuthResponse(response));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function selectOrganizationAndAccept(organizationId: string) {
    if (!organizationSelection) return;
    setBusy(true);
    setError("");
    try {
      const response = await studioApi.selectOrganization(organizationSelection.token, organizationId);
      await finishAcceptance(sessionFromAuthResponse(response));
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function registerAndAccept(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const response = await studioApi.registerWithInvitation({
        invitationToken,
        email,
        username,
        password: registrationPassword,
        displayName,
      });
      finish(response);
    } catch (cause) {
      setError(apiErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  if (loading || !hydrated) {
    return (
      <AuthPageShell title="验证邀请" description="正在确认邀请状态与目标组织。">
        <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          正在验证邀请
        </div>
      </AuthPageShell>
    );
  }

  if (!invitation) {
    return (
      <AuthPageShell title="邀请不可用" description="此链接可能已过期、被撤销或已经使用。">
        <ErrorMessage message={error} />
      </AuthPageShell>
    );
  }

  return (
    <AuthPageShell title={`加入${invitation.organizationName}`} description={`受邀账号：${invitation.email}`}>
      <div className="mb-5 flex items-center gap-3 rounded-lg border bg-muted/35 px-4 py-3">
        <span className="grid h-9 w-9 place-items-center rounded-md bg-primary/10 text-primary">
          <Building2 className="h-4 w-4" />
        </span>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{invitation.organizationName}</p>
          <p className="text-xs text-muted-foreground">邀请有效期至 {formatDate(invitation.expiresAt)}</p>
        </div>
      </div>

      {invitation.requiresRegistration ? (
        <form className="grid gap-4" onSubmit={registerAndAccept}>
          <Field label="受邀邮箱" htmlFor="invitation-email">
            <Input id="invitation-email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
          </Field>
          <Field label="用户名" htmlFor="invitation-username">
            <Input id="invitation-username" autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} minLength={3} maxLength={32} pattern="[A-Za-z0-9][A-Za-z0-9_-]{1,30}[A-Za-z0-9]" required />
          </Field>
          <Field label="姓名" htmlFor="invitation-display-name">
            <Input id="invitation-display-name" autoComplete="name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
          </Field>
          <Field label="密码" htmlFor="invitation-password">
            <Input id="invitation-password" type="password" autoComplete="new-password" value={registrationPassword} onChange={(event) => setRegistrationPassword(event.target.value)} minLength={8} required />
          </Field>
          <ErrorMessage message={error} />
          <Button type="submit" disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <UserPlus className="mr-2 h-4 w-4" />}
            创建账号并加入
          </Button>
        </form>
      ) : ready ? (
        <div className="grid gap-4">
          <div className="rounded-lg border px-4 py-3 text-sm">
            <p className="font-medium">以当前账号加入</p>
            <p className="mt-1 text-muted-foreground">{session.user?.displayName || session.user?.username || session.user?.email}</p>
          </div>
          <ErrorMessage message={error} />
          <Button type="button" disabled={busy} onClick={acceptCurrentAccount}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <CheckCircle2 className="mr-2 h-4 w-4" />}
            接受邀请
          </Button>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => clearSession()}>
            使用其他账号
          </Button>
        </div>
      ) : organizationSelection ? (
        <div className="grid gap-2">
          <p className="mb-2 text-sm text-muted-foreground">选择一个现有组织完成身份验证，接受后将自动切换到受邀组织。</p>
          {organizationSelection.organizations.map((organization) => (
            <Button key={organization.id} type="button" variant="outline" className="justify-start" disabled={busy} onClick={() => selectOrganizationAndAccept(organization.id)}>
              {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Building2 className="mr-2 h-4 w-4" />}
              {organization.name}
            </Button>
          ))}
          <ErrorMessage message={error} />
        </div>
      ) : (
        <form className="grid gap-4" onSubmit={loginAndAccept}>
          <Field label="用户名或邮箱" htmlFor="invitation-identifier">
            <Input id="invitation-identifier" autoComplete="username" value={identifier} onChange={(event) => setIdentifier(event.target.value)} required />
          </Field>
          <Field label="密码" htmlFor="invitation-login-password">
            <Input id="invitation-login-password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
          </Field>
          <ErrorMessage message={error} />
          <Button type="submit" disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <LogIn className="mr-2 h-4 w-4" />}
            登录并接受
          </Button>
        </form>
      )}
    </AuthPageShell>
  );
}

function Field({ label, htmlFor, children }: { label: string; htmlFor: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  );
}

function ErrorMessage({ message }: { message: string }) {
  return message ? <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">{message}</p> : null;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function apiErrorMessage(cause: unknown) {
  if (cause instanceof StudioApiError) return cause.message;
  return "操作失败，请稍后重试或联系组织管理员。";
}
