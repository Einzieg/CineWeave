"use client";

import Link from "next/link";
import type { Route } from "next";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/cn";
import { globalNavItems } from "@/lib/routes";
import type { GlobalSection } from "@/lib/routes";
import { useStudioSession } from "@/lib/session";

export function MainSidebar({ active }: { active: GlobalSection }) {
  const { session } = useStudioSession();
  const visibleItems = globalNavItems.filter((item) => !item.systemOnly || session.user?.systemAdministrator);
  return (
    <aside className="hidden h-full w-16 shrink-0 border-r bg-sidebar lg:flex lg:flex-col">
      {/* Logo */}
      <Link href="/projects" className="flex h-14 items-center justify-center border-b">
        <div className="grid h-9 w-9 place-items-center rounded-lg bg-primary text-sm font-bold text-primary-foreground">
          影
        </div>
      </Link>

      {/* Nav items */}
      <nav className="flex flex-1 flex-col gap-1 p-2" aria-label="全局导航">
        {visibleItems.map((item) => {
          const Icon = item.icon;
          const isActive = active === item.section;
          return (
            <Tooltip key={item.section} delayDuration={0}>
              <TooltipTrigger asChild>
                <Link
                  href={item.href as Route}
                  className={cn(
                    "flex h-12 w-12 items-center justify-center rounded-lg transition-colors",
                    isActive
                      ? "bg-sidebar-accent text-sidebar-accent-foreground"
                      : "text-sidebar-foreground hover:bg-sidebar-accent/50 hover:text-sidebar-accent-foreground",
                  )}
                  aria-label={item.label}
                >
                  <Icon className="h-5 w-5" />
                </Link>
              </TooltipTrigger>
              <TooltipContent side="right">{item.label}</TooltipContent>
            </Tooltip>
          );
        })}
      </nav>
    </aside>
  );
}

export function MobileGlobalNav({ active }: { active: GlobalSection }) {
  const { session } = useStudioSession();
  const visibleItems = globalNavItems.filter((item) => !item.systemOnly || session.user?.systemAdministrator);
  return (
    <nav className="flex shrink-0 gap-1 overflow-x-auto border-b bg-sidebar px-2 py-2 lg:hidden" aria-label="全局导航">
      {visibleItems.map((item) => {
        const Icon = item.icon;
        const isActive = active === item.section;
        return (
          <Link
            key={item.section}
            href={item.href as Route}
            className={cn(
              "flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm transition-colors",
              isActive
                ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                : "text-sidebar-foreground hover:bg-sidebar-accent/50",
            )}
          >
            <Icon className="h-4 w-4" />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
