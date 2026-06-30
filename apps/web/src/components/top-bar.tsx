"use client";

import { Save } from "lucide-react";
import type { StudioSession } from "@/lib/types";

export function TopBar({
  title,
  description,
  session,
  onSessionChange,
}: {
  title: string;
  description?: string;
  session: StudioSession;
  onSessionChange: (patch: Partial<StudioSession>) => void;
}) {
  return (
    <header className="sticky top-0 z-30 border-b border-white/10 bg-zinc-950/86 px-4 py-3 backdrop-blur-xl">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-normal text-zinc-50">{title}</h1>
          {description ? <p className="mt-1 text-sm text-zinc-400">{description}</p> : null}
        </div>
        <div className="grid gap-2 sm:grid-cols-2 xl:w-[860px] xl:grid-cols-[minmax(180px,1fr)_minmax(150px,1fr)_minmax(150px,1fr)_minmax(150px,1fr)]">
          <SessionInput label="访问令牌" type="password" value={session.accessToken} onChange={(accessToken) => onSessionChange({ accessToken })} />
          <SessionInput label="用户标识" value={session.currentUserId} onChange={(currentUserId) => onSessionChange({ currentUserId })} />
          <SessionInput label="组织 ID" value={session.organizationId} onChange={(organizationId) => onSessionChange({ organizationId })} />
          <SessionInput label="工作区 ID" value={session.workspaceId} onChange={(workspaceId) => onSessionChange({ workspaceId })} />
        </div>
      </div>
      <div className="mt-2 flex items-center gap-2 text-[12px] text-zinc-500">
        <Save size={13} />
        会话信息保存在本机浏览器，用于调用 CineWeave 接口。{session.currentProjectId ? `当前项目：${session.currentProjectId}` : ""}
      </div>
    </header>
  );
}

function SessionInput({ label, value, onChange, type = "text" }: { label: string; value: string; onChange: (value: string) => void; type?: string }) {
  return (
    <label className="grid gap-1 text-[12px] text-zinc-500">
      <span>{label}</span>
      <input
        className="h-9 rounded-md border border-white/10 bg-white/[0.04] px-3 text-sm text-zinc-100 outline-none transition placeholder:text-zinc-600 focus:border-cyan-300/60"
        onChange={(event) => onChange(event.target.value)}
        placeholder={`填写${label}`}
        type={type}
        value={value}
      />
    </label>
  );
}
