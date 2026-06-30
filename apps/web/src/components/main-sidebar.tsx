"use client";

import { globalNavItems } from "@/lib/routes";
import { cn } from "@/lib/cn";
import type { GlobalSection } from "@/lib/routes";
import type { Route } from "next";
import Link from "next/link";

export function MainSidebar({ active }: { active: GlobalSection }) {
  return (
    <aside className="hidden min-h-svh w-64 shrink-0 border-r border-slate-200 bg-white px-3 py-4 lg:block">
      <Link className="flex h-12 items-center gap-3 rounded-lg px-3" href={"/dashboard" as Route}>
        <span className="grid h-8 w-8 place-items-center rounded-lg bg-blue-600 text-sm font-semibold text-white">影</span>
        <span>
          <span className="block text-sm font-semibold text-slate-950">影织</span>
          <span className="block text-xs text-slate-500">AI 视频创作工作台</span>
        </span>
      </Link>
      <nav className="mt-6 grid gap-1" aria-label="全局导航">
        {globalNavItems.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              className={cn(
                "flex h-10 items-center gap-3 rounded-lg px-3 text-sm transition",
                active === item.section ? "bg-slate-950 text-white" : "text-slate-600 hover:bg-slate-100 hover:text-slate-950",
              )}
              href={item.href as Route}
              key={item.section}
            >
              <Icon size={16} />
              {item.label}
            </Link>
          );
        })}
      </nav>
      <div className="mt-8 rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-600">
        <p className="font-medium text-slate-900">生产路径</p>
        <p className="mt-2">项目设定 → 原文/剧本 → 资产 → 分镜 → 镜头视频 → 成片</p>
      </div>
    </aside>
  );
}

export function MobileGlobalNav({ active }: { active: GlobalSection }) {
  return (
    <nav className="flex gap-1 overflow-x-auto border-b border-slate-200 bg-white px-4 py-2 lg:hidden" aria-label="全局导航">
      {globalNavItems.map((item) => {
        const Icon = item.icon;
        return (
          <Link
            className={cn(
                "flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm transition",
                active === item.section ? "bg-slate-950 text-white" : "text-slate-600 hover:bg-slate-100 hover:text-slate-950",
              )}
            href={item.href as Route}
            key={item.section}
          >
            <Icon size={15} />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
