"use client";

import { projectHref, projectNavItems } from "@/lib/routes";
import { cn } from "@/lib/cn";
import type { ProjectSection } from "@/lib/routes";
import type { Route } from "next";
import Link from "next/link";

export function ProjectNav({ projectId, active }: { projectId: string; active: ProjectSection }) {
  return (
    <nav className="flex gap-1 overflow-x-auto border-b border-slate-200 px-4 pt-3" aria-label="项目内部导航">
      {projectNavItems.map((item) => {
        const Icon = item.icon;
        return (
          <Link
            className={cn(
              "flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-sm transition",
              active === item.segment ? "border-blue-600 text-slate-950" : "border-transparent text-slate-500 hover:text-slate-900",
            )}
            href={projectHref(projectId, item.segment) as Route}
            key={item.segment || "overview"}
          >
            <Icon size={15} />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
