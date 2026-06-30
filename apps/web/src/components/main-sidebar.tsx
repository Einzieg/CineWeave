"use client";

import { globalNavItems } from "@/lib/routes";
import { cn } from "@/lib/cn";
import type { GlobalSection } from "@/lib/routes";
import type { Route } from "next";
import Link from "next/link";

export function MainSidebar({ active }: { active: GlobalSection }) {
  return (
    <aside className="hidden min-h-svh w-64 shrink-0 border-r border-white/10 bg-zinc-950/96 px-3 py-4 lg:block">
      <Link className="flex h-12 items-center gap-3 rounded-lg px-3" href={"/dashboard" as Route}>
        <span className="grid h-8 w-8 place-items-center rounded-lg bg-cyan-300 text-sm font-semibold text-zinc-950">影</span>
        <span>
          <span className="block text-sm font-semibold text-zinc-50">影织 Studio</span>
          <span className="block text-xs text-zinc-500">脚本驱动创作台</span>
        </span>
      </Link>
      <nav className="mt-6 grid gap-1" aria-label="全局导航">
        {globalNavItems.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              className={cn(
                "flex h-10 items-center gap-3 rounded-lg px-3 text-sm transition",
                active === item.section ? "bg-white text-zinc-950" : "text-zinc-400 hover:bg-white/8 hover:text-zinc-100",
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
      <div className="mt-8 rounded-lg border border-white/10 bg-white/[0.03] p-3 text-xs leading-5 text-zinc-400">
        <p className="font-medium text-zinc-200">生产路径</p>
        <p className="mt-2">项目设定 → 原文/剧本 → 资产 → 分镜 → 镜头视频 → 成片</p>
      </div>
    </aside>
  );
}

export function MobileGlobalNav({ active }: { active: GlobalSection }) {
  return (
    <nav className="flex gap-1 overflow-x-auto border-b border-white/10 px-4 py-2 lg:hidden" aria-label="全局导航">
      {globalNavItems.map((item) => {
        const Icon = item.icon;
        return (
          <Link
            className={cn(
              "flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm transition",
              active === item.section ? "bg-white text-zinc-950" : "text-zinc-400 hover:bg-white/8 hover:text-zinc-100",
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
