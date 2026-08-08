"use client";

import { useState } from "react";
import { Check, Copy, KeyRound, Loader2, RotateCw, Save, Trash2, UserRound } from "lucide-react";
import { toast } from "sonner";
import { AppShell, Surface, SectionTitle } from "@/components/layout/app-shell";
import { ErrorPanel } from "@/components/shared/error-panel";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  CodexControlKeySecretDialog,
  codexControlKeyEnvironmentVariable,
  codexMcpConfiguration,
} from "@/features/settings/codex-control-key-secret-dialog";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import type { CodexControlKeyMetadata, CodexControlKeySecret } from "@/lib/types";

export function GlobalSettingsPage() {
  return (
    <AppShell active="settings" title="我的账号" description="维护用于协作界面展示的个人资料。">
      <SettingsContent />
    </AppShell>
  );
}

function SettingsContent() {
  const { session } = useStudioSession();
  if (!session.user) return null;
  return <ProfileForm key={session.user.id} />;
}

function ProfileForm() {
  const { session, updateSession } = useStudioSession();
  const [displayName, setDisplayName] = useState(session.user?.displayName ?? "");
  const [avatarUrl, setAvatarUrl] = useState(session.user?.avatarUrl ?? "");
  const mutation = useApiMutation({
    mutationFn: (apiSession, values: { displayName: string; avatarUrl: string }) => studioApi.updateProfile(apiSession, values),
    onSuccess: (user) => {
      updateSession({ user });
      setDisplayName(user.displayName ?? "");
      setAvatarUrl(user.avatarUrl ?? "");
      toast.success("个人资料已更新");
    },
  });
  const display = displayName.trim() || session.user?.username || session.user?.email || "用户";
  const dirty = displayName.trim() !== (session.user?.displayName ?? "") || avatarUrl.trim() !== (session.user?.avatarUrl ?? "");
  return (
    <div className="mx-auto grid max-w-4xl gap-4 lg:grid-cols-[15rem_minmax(0,1fr)]">
      <Surface className="self-start">
        <div className="grid place-items-center px-5 py-8 text-center">
          <Avatar className="h-20 w-20 border">
            <AvatarImage src={avatarUrl.trim()} alt={display} />
            <AvatarFallback><UserRound className="h-8 w-8" /></AvatarFallback>
          </Avatar>
          <p className="mt-4 max-w-full truncate text-sm font-semibold">{display}</p>
          <p className="mt-1 max-w-full truncate text-xs text-muted-foreground">@{session.user?.username || "未设置用户名"}</p>
        </div>
      </Surface>
      <div className="grid min-w-0 gap-4">
        <Surface>
          <SectionTitle title="个人资料" description="用户名与邮箱用于登录和身份识别，当前不可在此修改。" />
          <div className="grid gap-5 p-4 sm:p-6">
            <div className="grid gap-2"><Label htmlFor="profile-username">用户名</Label><Input id="profile-username" value={session.user?.username ?? ""} readOnly className="bg-muted/40 text-muted-foreground" /></div>
            <div className="grid gap-2"><Label htmlFor="profile-email">邮箱</Label><Input id="profile-email" value={session.user?.email ?? ""} readOnly className="bg-muted/40 text-muted-foreground" /></div>
            <div className="grid gap-2"><Label htmlFor="profile-display-name">显示名称</Label><Input id="profile-display-name" maxLength={100} value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="输入协作界面中显示的姓名" /></div>
            <div className="grid gap-2"><Label htmlFor="profile-avatar-url">头像地址</Label><Input id="profile-avatar-url" type="url" maxLength={2048} value={avatarUrl} onChange={(event) => setAvatarUrl(event.target.value)} placeholder="https://example.com/avatar.jpg" /></div>
            {mutation.error ? <ErrorPanel message={errorMessage(mutation.error)} /> : null}
            <div><Button disabled={!dirty || mutation.isPending} onClick={() => mutation.mutate({ displayName: displayName.trim(), avatarUrl: avatarUrl.trim() })}>{mutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}保存个人资料</Button></div>
          </div>
        </Surface>
        <CodexControlKeyPanel />
      </div>
    </div>
  );
}

function CodexControlKeyPanel() {
  const invalidate = useInvalidateKeys();
  const [revealedKey, setRevealedKey] = useState<CodexControlKeySecret>();
  const [copiedValue, setCopiedValue] = useState<"environment" | "configuration">();
  const [confirmation, setConfirmation] = useState<"rotate" | "revoke">();
  const keyQuery = useApiQuery({
    key: qk.codexControlKey(),
    queryFn: (session) => studioApi.getCodexControlKey(session),
  });
  const refreshStatus = () => invalidate([qk.codexControlKey()]);
  const createMutation = useApiMutation({
    mutationFn: (session) => studioApi.createCodexControlKey(session),
    onSuccess: ({ codexControlKey }) => {
      setRevealedKey(codexControlKey);
      setCopiedValue(undefined);
      refreshStatus();
      toast.success("Codex 项目控制密钥已创建");
    },
  });
  const rotateMutation = useApiMutation({
    mutationFn: (session) => studioApi.rotateCodexControlKey(session),
    onSuccess: ({ codexControlKey }) => {
      setRevealedKey(codexControlKey);
      setCopiedValue(undefined);
      setConfirmation(undefined);
      refreshStatus();
      toast.success("Codex 项目控制密钥已轮换");
    },
  });
  const revokeMutation = useApiMutation({
    mutationFn: (session) => studioApi.revokeCodexControlKey(session),
    onSuccess: () => {
      setConfirmation(undefined);
      refreshStatus();
      toast.success("Codex 项目控制密钥已撤销");
    },
  });
  const key = keyQuery.data?.key;
  const mutationError = createMutation.error || rotateMutation.error || revokeMutation.error;
  const pending = createMutation.isPending || rotateMutation.isPending || revokeMutation.isPending;

  async function copyValue(value: string, target: "environment" | "configuration") {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedValue(target);
      toast.success("已复制");
    } catch {
      toast.error("复制失败，请手动复制");
    }
  }

  return (
    <Surface>
      <SectionTitle title="Codex 项目控制" description="管理 Codex App 连接 CineWeave 时使用的个人凭据。" />
      <div className="grid gap-5 p-4 sm:p-6">
        {keyQuery.isLoading ? (
          <div className="flex h-20 items-center justify-center"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div>
        ) : keyQuery.error ? (
          <ErrorPanel message={errorMessage(keyQuery.error)} />
        ) : (
          <ControlKeyStatus keyMetadata={key} requiresSetup={keyQuery.data?.requiresSetup === true} />
        )}

        <div className="flex flex-wrap gap-2">
          {!key || key.status === "revoked" ? (
            <Button disabled={pending || keyQuery.isLoading} onClick={() => createMutation.mutate()}>
              {createMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <KeyRound className="mr-2 h-4 w-4" />}
              创建密钥
            </Button>
          ) : (
            <>
              <Button variant="outline" disabled={pending || !key.canRotate} onClick={() => setConfirmation("rotate")}>
                {rotateMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RotateCw className="mr-2 h-4 w-4" />}
                轮换密钥
              </Button>
              <Button variant="destructive" disabled={pending || !key.canRevoke} onClick={() => setConfirmation("revoke")}>
                {revokeMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Trash2 className="mr-2 h-4 w-4" />}
                撤销密钥
              </Button>
            </>
          )}
        </div>

        <div className="grid gap-3 border-t pt-5">
          <CopyField
            label="环境变量"
            value={codexControlKeyEnvironmentVariable}
            copied={copiedValue === "environment"}
            onCopy={() => copyValue(codexControlKeyEnvironmentVariable, "environment")}
          />
          <CopyField
            label="Codex MCP 配置"
            value={codexMcpConfiguration}
            multiline
            copied={copiedValue === "configuration"}
            onCopy={() => copyValue(codexMcpConfiguration, "configuration")}
          />
        </div>

        {mutationError ? <ErrorPanel message={errorMessage(mutationError)} /> : null}
      </div>

      <CodexControlKeySecretDialog secret={revealedKey} onClose={() => setRevealedKey(undefined)} />

      <AlertDialog open={Boolean(confirmation)} onOpenChange={(open) => { if (!open && !pending) setConfirmation(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmation === "rotate" ? "轮换 Codex 密钥" : "撤销 Codex 密钥"}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirmation === "rotate"
                ? "旧密钥会立即失效。新的密钥明文只显示一次。"
                : "撤销后，正在使用该密钥的 Codex App 会立即失去访问权限。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={pending}
              variant={confirmation === "revoke" ? "destructive" : "default"}
              onClick={(event) => {
                event.preventDefault();
                if (confirmation === "rotate") rotateMutation.mutate();
                if (confirmation === "revoke") revokeMutation.mutate();
              }}
            >
              {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {confirmation === "rotate" ? "确认轮换" : "确认撤销"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Surface>
  );
}

function ControlKeyStatus({
  keyMetadata,
  requiresSetup,
}: {
  keyMetadata?: CodexControlKeyMetadata;
  requiresSetup: boolean;
}) {
  if (!keyMetadata) {
    return (
      <div className="flex items-center gap-3 rounded-md border p-4">
        <KeyRound className="h-5 w-5 text-muted-foreground" />
        <div className="min-w-0"><p className="font-medium">尚未创建密钥</p><p className="text-xs text-muted-foreground">{requiresSetup ? "创建后即可连接 Codex App。" : "当前没有可用密钥。"}</p></div>
      </div>
    );
  }
  return (
    <div className="grid gap-3 rounded-md border p-4 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="font-medium">{keyMetadata.name}</p>
          <Badge variant={keyMetadata.status === "active" ? "default" : keyMetadata.status === "revoked" ? "destructive" : "outline"}>
            {controlKeyStatusLabel(keyMetadata.status)}
          </Badge>
        </div>
        <p className="mt-2 truncate font-mono text-xs text-muted-foreground">{keyMetadata.prefix}</p>
      </div>
      <div className="grid content-start gap-1 text-xs text-muted-foreground sm:text-right">
        <span>创建于 {formatKeyDate(keyMetadata.createdAt)}</span>
        <span>最近使用 {keyMetadata.lastUsedAt ? formatKeyDate(keyMetadata.lastUsedAt) : "暂无"}</span>
      </div>
    </div>
  );
}

function CopyField({
  label,
  value,
  copied,
  multiline = false,
  onCopy,
}: {
  label: string;
  value: string;
  copied: boolean;
  multiline?: boolean;
  onCopy: () => void;
}) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <div className="flex min-w-0 items-start gap-2">
        <pre className={`min-w-0 flex-1 overflow-x-auto rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs ${multiline ? "whitespace-pre" : "truncate"}`}>{value}</pre>
        <Button type="button" size="icon" variant="outline" aria-label={`复制${label}`} onClick={onCopy}>
          {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
        </Button>
      </div>
    </div>
  );
}

function controlKeyStatusLabel(status: CodexControlKeyMetadata["status"]) {
  if (status === "active") return "可用";
  if (status === "requires_rotation") return "需要轮换";
  return "已撤销";
}

function formatKeyDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString("zh-CN", { hour12: false });
}

function errorMessage(error: unknown) {
  if (error instanceof StudioApiError || error instanceof Error) {
    return error.message;
  }
  return "更新个人资料失败";
}
