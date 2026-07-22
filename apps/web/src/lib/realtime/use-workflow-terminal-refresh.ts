"use client";

import { useEffect, useRef } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { useInvalidateKeys } from "@/lib/query/use-api";
import { keysForTerminalWorkflowRun } from "@/lib/realtime/event-map";
import type { WorkflowRun } from "@/lib/types";
import { isTerminalWorkflowStatus } from "@/lib/workflow-status";

type WorkflowStatusSnapshot = {
  projectId: string;
  statuses: Map<string, string>;
};

export function useWorkflowTerminalRefresh(projectId: string, workflowRuns: WorkflowRun[], ready: boolean) {
  const invalidate = useInvalidateKeys();
  const snapshotRef = useRef<WorkflowStatusSnapshot | null>(null);

  useEffect(() => {
    if (!ready || !projectId) return;

    const nextStatuses = new Map(workflowRuns.map((run) => [run.id, run.status]));
    const previous = snapshotRef.current;
    snapshotRef.current = { projectId, statuses: nextStatuses };
    if (!previous || previous.projectId !== projectId) return;

    const keys: QueryKey[] = [];
    for (const run of workflowRuns) {
      if (!isTerminalWorkflowStatus(run.status)) continue;
      const previousStatus = previous.statuses.get(run.id);
      if (previousStatus && isTerminalWorkflowStatus(previousStatus)) continue;
      keys.push(...keysForTerminalWorkflowRun(projectId, run.workflowType, run.output));
    }
    if (keys.length > 0) {
      invalidate(uniqueQueryKeys(keys));
    }
  }, [invalidate, projectId, ready, workflowRuns]);
}

function uniqueQueryKeys(keys: QueryKey[]) {
  const seen = new Set<string>();
  return keys.filter((key) => {
    const serialized = JSON.stringify(key);
    if (seen.has(serialized)) return false;
    seen.add(serialized);
    return true;
  });
}
