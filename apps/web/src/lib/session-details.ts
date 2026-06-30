"use client";

import { useEffect, useState } from "react";
import { studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";

type SessionDetails = {
  organizationName: string;
  workspaceName: string;
};

export function useSessionDetails(): SessionDetails {
  const { session, hydrated, ready, updateSession } = useStudioSession();
  const [details, setDetails] = useState<SessionDetails>({ organizationName: "", workspaceName: "" });

  useEffect(() => {
    if (!hydrated || !ready) {
      return;
    }

    let cancelled = false;
    Promise.allSettled([studioApi.listOrganizations(session), studioApi.listWorkspaces(session)]).then(([organizationsResult, workspacesResult]) => {
      if (cancelled) {
        return;
      }

      const organizations = organizationsResult.status === "fulfilled" ? organizationsResult.value.items : [];
      const workspaces = workspacesResult.status === "fulfilled" ? workspacesResult.value.items : [];
      const organization = organizations.find((item) => item.id === session.organizationId) ?? organizations[0];
      const workspace = workspaces.find((item) => item.id === session.workspaceId) ?? workspaces[0];

      setDetails({
        organizationName: organization?.name ?? "",
        workspaceName: workspace?.name ?? "",
      });

      if (!session.workspaceId && workspace?.id) {
        updateSession({ workspaceId: workspace.id });
      }
    });

    return () => {
      cancelled = true;
    };
  }, [hydrated, ready, session, updateSession]);

  return details;
}
