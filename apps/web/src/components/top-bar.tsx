"use client";

import { LogOut, UserCircle2 } from "lucide-react";
import type { StudioSession } from "@/lib/types";

export function TopBar({
  title,
  description,
  session,
  onLogout,
}: {
  title: string;
  description?: string;
  session: StudioSession;
  onLogout: () => void;
}) {
  const displayName = session.user?.displayName?.trim() || session.user?.email || "已登录用户";

  return (
    <header className="sticky top-0 z-30 border-b border-slate-200 bg-white/90 px-4 py-3 backdrop-blur-xl">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-normal text-slate-950">{title}</h1>
          {description ? <p className="mt-1 text-sm text-slate-500">{description}</p> : null}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex min-w-0 items-center gap-2 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
            <UserCircle2 className="shrink-0 text-slate-500" size={18} />
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-slate-900">{displayName}</p>
              {session.user?.email ? <p className="truncate text-xs text-slate-500">{session.user.email}</p> : null}
            </div>
          </div>
          <button className="studio-button" onClick={onLogout} type="button">
            <LogOut size={16} />
            退出登录
          </button>
        </div>
      </div>
    </header>
  );
}
