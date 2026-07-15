"use client";

import { useActivityStore } from "@/lib/stores/activity-store";

export function useProjectPollingFallback(projectId?: string) {
  return useActivityStore((state) => (
    !projectId || state.connectionByProject[projectId] !== "connected"
  ));
}
