"use client";

import { useRouter } from "next/navigation";
import type { Route } from "next";
import { X } from "lucide-react";
import { AppShell, Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";
import { useSessionDetails } from "@/lib/session-details";

export function GlobalSettingsPage() {
  return (
    <AppShell active="settings" title="设置" description="查看当前账号、组织和本机登录状态。">
      <SettingsContent />
    </AppShell>
  );
}

function SettingsContent() {
  const router = useRouter();
  const { session, clearSession } = useStudioSession();
  const details = useSessionDetails();

  async function logout() {
    if (session.refreshToken.trim()) {
      await studioApi.logout(session.refreshToken).catch(() => undefined);
    }
    clearSession();
    router.replace("/login" as Route);
  }

  return (
    <Surface>
      <SectionTitle title="账号信息" description="当前浏览器保存的是登录会话，不再需要手动维护认证信息。" />
      <div className="grid gap-4 p-4 md:grid-cols-2">
        <InfoTile label="显示名称" value={session.user?.displayName || "未设置"} />
        <InfoTile label="邮箱" value={session.user?.email || "未设置"} />
        <InfoTile label="当前组织" value={details.organizationName || (session.organizationId ? "已连接" : "未连接")} />
        <InfoTile label="当前工作区" value={details.workspaceName || (session.workspaceId ? "已连接" : "未连接")} />
        <div className="md:col-span-2">
          <Button variant="outline" onClick={logout} type="button">
            <X className="mr-2 h-4 w-4" />
            退出登录
          </Button>
        </div>
      </div>
    </Surface>
  );
}

function InfoTile({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="pt-6">
        <Label className="text-muted-foreground">{label}</Label>
        <p className="mt-2 text-sm font-medium">{value}</p>
      </CardContent>
    </Card>
  );
}
