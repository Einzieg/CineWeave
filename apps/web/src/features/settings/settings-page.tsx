"use client";

import { useState } from "react";
import { Loader2, Save, UserRound } from "lucide-react";
import { toast } from "sonner";
import { AppShell, Surface, SectionTitle } from "@/components/layout/app-shell";
import { ErrorPanel } from "@/components/shared/error-panel";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { useApiMutation } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";

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
    </div>
  );
}

function errorMessage(error: unknown) {
  if (error instanceof StudioApiError || error instanceof Error) {
    return error.message;
  }
  return "更新个人资料失败";
}
