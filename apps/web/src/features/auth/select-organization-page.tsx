"use client";

import { Building2, Loader2 } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { AuthPageShell } from "@/features/auth/components/auth-page-shell";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";

export function SelectOrganizationPage() {
  const router = useRouter();
  const { pendingOrganizationSelection, setPendingOrganizationSelection, setSession } = useStudioSession();
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!pendingOrganizationSelection) {
      router.replace("/login" as Route);
    }
  }, [pendingOrganizationSelection, router]);

  async function select(organizationId: string) {
    if (!pendingOrganizationSelection) return;
    setBusyId(organizationId);
    setError("");
    try {
      const response = await studioApi.selectOrganization(pendingOrganizationSelection.token, organizationId);
      setSession(sessionFromAuthResponse(response));
      router.replace((response.user.username ? nextPath() : usernamePath()) as Route);
    } catch (cause) {
      setPendingOrganizationSelection(null);
      setError(cause instanceof StudioApiError ? cause.message : "组织选择失败，请重新登录。");
    } finally {
      setBusyId("");
    }
  }

  return (
    <AuthPageShell title="选择组织" description="选择本次进入的工作空间，稍后可在组织设置中切换。">
      <div className="divide-y rounded-lg border">
        {pendingOrganizationSelection?.organizations.map((organization) => (
          <Button
            key={organization.id}
            type="button"
            variant="ghost"
            className="h-auto w-full justify-start rounded-none px-4 py-3 first:rounded-t-lg last:rounded-b-lg"
            disabled={Boolean(busyId)}
            onClick={() => select(organization.id)}
          >
            {busyId === organization.id ? <Loader2 className="mr-3 h-4 w-4 animate-spin" /> : <Building2 className="mr-3 h-4 w-4" />}
            <span className="truncate">{organization.name}</span>
          </Button>
        ))}
      </div>
      {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
    </AuthPageShell>
  );
}

function nextPath() {
  const value = typeof window === "undefined" ? null : new URLSearchParams(window.location.search).get("next");
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/projects";
  return value;
}

function usernamePath() {
  const next = nextPath();
  return next === "/projects" ? "/set-username" : `/set-username?next=${encodeURIComponent(next)}`;
}
