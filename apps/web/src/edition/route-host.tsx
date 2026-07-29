"use client";

import { notFound, usePathname } from "next/navigation";
import { AlertTriangle, LoaderCircle, LockKeyhole } from "lucide-react";
import type { ReactNode } from "react";
import { editionEntry } from "@cineweave/edition-entry";
import { AppShell, Surface } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { entitlementDenialLabel } from "@/lib/labels";
import type { GlobalSection } from "@/lib/routes";
import {
  editionFeatureDecision,
  useEditionEntitlements,
} from "./use-entitlements";

export function EditionRouteHost() {
  const pathname = usePathname();
  const route = editionEntry.routes.find(
    (candidate) => candidate.pathname === pathname,
  );
  const entitlement = useEditionEntitlements(route !== undefined);

  if (!route) {
    notFound();
  }

  const navigation = editionEntry.navigation.find(
    (candidate) => candidate.href === route.pathname,
  );
  const guard = editionEntry.entitlementGuards.find(
    (candidate) => candidate.featureKey === route.featureKey,
  );
  const active = (navigation?.section ?? "settings") as GlobalSection;
  const title = navigation?.label ?? "商业功能";

  if (entitlement.isPending) {
    return (
      <EditionStatePage active={active} title={title}>
        <LoaderCircle className="h-5 w-5 animate-spin text-muted-foreground" />
        <p className="text-sm text-muted-foreground">正在验证商业授权…</p>
      </EditionStatePage>
    );
  }

  if (entitlement.isError) {
    return (
      <EditionStatePage active={active} title={title}>
        <AlertTriangle className="h-5 w-5 text-destructive" />
        <p className="text-sm">暂时无法验证商业授权，请重试。</p>
        <Button
          size="sm"
          variant="outline"
          onClick={() => void entitlement.refetch()}
        >
          重新验证
        </Button>
      </EditionStatePage>
    );
  }

  const decision = editionFeatureDecision(entitlement.data, route.featureKey);
  if (!decision?.allowed) {
    if (guard?.behavior === "not_found") {
      notFound();
    }
    return (
      <EditionStatePage active={active} title={title}>
        <LockKeyhole className="h-5 w-5 text-muted-foreground" />
        <div className="space-y-1 text-center">
          <p className="text-sm font-medium">
            {guard?.behavior === "upgrade"
              ? "当前组织尚未开通此商业能力"
              : "当前账号无权访问此商业能力"}
          </p>
          <p className="text-xs text-muted-foreground">
            授权原因：
            {entitlementDenialLabel(decision?.reason ?? "permission_denied")}
          </p>
        </div>
      </EditionStatePage>
    );
  }

  const Component = route.component;
  return <Component />;
}

function EditionStatePage({
  active,
  title,
  children,
}: {
  active: GlobalSection;
  title: string;
  children: ReactNode;
}) {
  return (
    <AppShell active={active} title={title}>
      <Surface className="grid min-h-56 place-items-center">
        <div className="flex max-w-md flex-col items-center gap-3 p-8">
          {children}
        </div>
      </Surface>
    </AppShell>
  );
}
