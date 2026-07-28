"use client";

import { useEffect, useRef } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { useInvalidateKeys } from "@/lib/query/use-api";
import { keysForTerminalWorkflowRun } from "@/lib/realtime/event-map";
import type { WorkflowRun } from "@/lib/types";
import { isTerminalWorkflowStatus } from "@/lib/workflow-status";

type WorkflowStatusSnapshot = {
  projectId: string;
  runs: Map<string, WorkflowRun>;
};

export function useWorkflowTerminalRefresh(projectId: string, workflowRuns: WorkflowRun[], ready: boolean) {
  const invalidate = useInvalidateKeys();
  const snapshotRef = useRef<WorkflowStatusSnapshot | null>(null);

  useEffect(() => {
    if (!ready || !projectId) return;

    const nextRuns = new Map(workflowRuns.map((run) => [run.id, run]));
    const previous = snapshotRef.current;
    snapshotRef.current = { projectId, runs: nextRuns };
    if (!previous || previous.projectId !== projectId) return;

    const keys: QueryKey[] = [];
    for (const run of workflowRuns) {
      if (!isTerminalWorkflowStatus(run.status)) continue;
      const previousRun = previous.runs.get(run.id);
      if (previousRun && isTerminalWorkflowStatus(previousRun.status)) continue;
      keys.push(...keysForTerminalWorkflowRun(projectId, run.workflowType, run.output));
    }
    for (const previousRun of previous.runs.values()) {
      if (isTerminalWorkflowStatus(previousRun.status) || nextRuns.has(previousRun.id)) continue;
      keys.push(...keysForTerminalWorkflowRun(projectId, previousRun.workflowType, previousRun.output));
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
