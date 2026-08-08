"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { toast } from "sonner";
import { consumeServerSentEvents } from "@/lib/realtime/fetch-sse";
import { keysForProjectEvent, projectEventNames, toastForProjectEvent } from "@/lib/realtime/event-map";
import { projectEventDefinitions, type ProjectEventName } from "@/lib/realtime/generated-events";
import { qk } from "@/lib/query/keys";
import { orgScopedKey } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import { useActivityStore } from "@/lib/stores/activity-store";
import { useUiStore } from "@/lib/stores/ui-store";

const configuredRealtimeBase = process.env.NEXT_PUBLIC_REALTIME_URL?.trim() ?? "";
const knownProjectEvents = new Set<string>(projectEventNames);

type RealtimeErrorBody = {
  error?: { code?: string };
};

export function useProjectEvents(projectId: string) {
  const queryClient = useQueryClient();
  const { session, ready } = useStudioSession();
  const organizationId = session.organizationId;
  const accessToken = session.accessToken;
  const recordActivityEvent = useActivityStore((state) => state.recordEvent);
  const setConnectionStatus = useActivityStore((state) => state.setConnectionStatus);
  const markGeneratedScript = useUiStore((state) => state.markGeneratedScript);

  useEffect(() => {
    if (!ready || !projectId || !accessToken.trim()) {
      return;
    }
    const controller = new AbortController();
    const cursorKey = `cineweave.realtime.cursor.v1:${organizationId}:${projectId}`;
    const pending = new Set<string>();
    let flushTimer: number | null = null;
    let stopped = false;
    let retryDelay = 1000;
    let toastArmedAt = Number.POSITIVE_INFINITY;
    let lastStreamPosition = parseStreamPosition(window.sessionStorage.getItem(cursorKey));
    const latestRevisionByAggregate = new Map<string, number>();

    const flush = () => {
      flushTimer = null;
      for (const raw of pending) {
        void queryClient.invalidateQueries({ queryKey: JSON.parse(raw) as unknown[] });
      }
      pending.clear();
    };

    const scheduleKeys = (keys: readonly (readonly unknown[])[]) => {
      for (const key of keys) {
        pending.add(JSON.stringify(orgScopedKey(organizationId, key)));
      }
      if (flushTimer === null && pending.size > 0) {
        flushTimer = window.setTimeout(flush, 250);
      }
    };

    const resyncAuthoritativeState = () => {
      scheduleKeys([
        qk.project(projectId),
        qk.productionStatus(projectId),
        qk.workflowRuns(projectId),
        qk.artifacts(projectId),
        qk.assetsRoot(projectId),
        qk.requirements(projectId),
        qk.agentTasks(projectId),
        qk.shotProductionPrefix(projectId),
      ]);
    };

    const run = async () => {
      while (!stopped && !controller.signal.aborted) {
        setConnectionStatus(projectId, "reconnecting");
        const cursor = window.sessionStorage.getItem(cursorKey)?.trim() ?? "";
        const headers = new Headers({
          Accept: "text/event-stream",
          Authorization: `Bearer ${accessToken.trim()}`,
          "Cache-Control": "no-cache",
        });
        if (cursor) {
          headers.set("Last-Event-ID", cursor);
        }
        try {
          const response = await fetch(`${resolveRealtimeBase()}?projectId=${encodeURIComponent(projectId)}`, {
            method: "GET",
            headers,
            cache: "no-store",
            signal: controller.signal,
          });
          if (response.status === 410) {
            window.sessionStorage.removeItem(cursorKey);
            lastStreamPosition = 0;
            latestRevisionByAggregate.clear();
            resyncAuthoritativeState();
            retryDelay = 250;
            continue;
          }
          if (response.status === 401 || response.status === 403 || response.status === 404) {
            setConnectionStatus(projectId, "disconnected");
            return;
          }
          if (!response.ok) {
            const body = await readRealtimeError(response);
            throw new Error(body.error?.code || `实时事件连接失败：HTTP ${response.status}`);
          }
          if (!response.headers.get("content-type")?.includes("text/event-stream")) {
            throw new Error("实时事件服务返回了无效的响应格式");
          }

          await consumeServerSentEvents(response, (event) => {
            if (event.retry !== undefined) {
              retryDelay = Math.min(Math.max(event.retry, 250), 15_000);
            }
            if (event.event === "stream.ready") {
              setConnectionStatus(projectId, "connected");
              resyncAuthoritativeState();
              toastArmedAt = Date.now() + 1000;
              return;
            }
            if (event.event === "stream.error") {
              throw new Error("实时事件读取失败");
            }
            let payload: Record<string, unknown> = {};
            try {
              payload = JSON.parse(event.data) as Record<string, unknown>;
            } catch {
              payload = {};
            }
            if (event.id) {
              const position = parseStreamPosition(event.id);
              if (position > 0 && position <= lastStreamPosition) {
                return;
              }
              if (position > 0) {
                lastStreamPosition = position;
              }
              window.sessionStorage.setItem(cursorKey, event.id);
            }
            if (!knownProjectEvents.has(event.event)) {
              resyncAuthoritativeState();
              return;
            }
            const definition = projectEventDefinitions[event.event as ProjectEventName];
            if (numberPayload(payload, "schemaVersion") !== definition.schemaVersion) {
              resyncAuthoritativeState();
              return;
            }
            const aggregateType = stringPayload(payload, "aggregateType");
            const aggregateId = stringPayload(payload, "aggregateId");
            const aggregateRevision = numberPayload(payload, "aggregateRevision");
            if (aggregateType && aggregateId && aggregateRevision >= 0) {
              const aggregateKey = `${aggregateType}:${aggregateId}`;
              const currentRevision = latestRevisionByAggregate.get(aggregateKey);
              if (currentRevision !== undefined && aggregateRevision <= currentRevision) {
                return;
              }
              latestRevisionByAggregate.set(aggregateKey, aggregateRevision);
            }
            recordActivityEvent(projectId, event.event, activitySignalPayload(payload), event.id);
            if (event.event === "script.episode.generated") {
              const scriptId = stringPayload(payload, "scriptId");
              const scriptVersionId = stringPayload(payload, "scriptVersionId");
              if (scriptId && scriptVersionId) {
                markGeneratedScript(projectId, scriptId, scriptVersionId);
              }
            }
            scheduleKeys(keysForProjectEvent(event.event, projectId, payload));
            if (Date.now() >= toastArmedAt) {
              const notice = toastForProjectEvent(event.event, payload);
              if (notice?.kind === "error") {
                toast.error(notice.text);
              } else if (notice) {
                toast.success(notice.text);
              }
            }
          });
          retryDelay = 1000;
        } catch (error) {
          if (controller.signal.aborted || stopped) {
            return;
          }
          setConnectionStatus(projectId, "reconnecting");
          if (error instanceof DOMException && error.name === "AbortError") {
            return;
          }
        }
        await abortableDelay(retryDelay, controller.signal);
        retryDelay = Math.min(Math.round(retryDelay * 1.6), 15_000);
      }
    };

    void run();
    return () => {
      stopped = true;
      controller.abort();
      setConnectionStatus(projectId, "disconnected");
      if (flushTimer !== null) {
        window.clearTimeout(flushTimer);
      }
    };
  }, [accessToken, markGeneratedScript, organizationId, projectId, queryClient, ready, recordActivityEvent, setConnectionStatus]);
}

function resolveRealtimeBase() {
  const sameOriginBase = new URL("/api/realtime/events", window.location.origin);
  if (!configuredRealtimeBase) {
    return sameOriginBase.toString();
  }
  const configuredUrl = new URL(configuredRealtimeBase, window.location.origin);
  if (!isLoopbackHost(window.location.hostname) && isLoopbackHost(configuredUrl.hostname)) {
    return sameOriginBase.toString();
  }
  return configuredUrl.toString();
}

function isLoopbackHost(hostname: string) {
  const normalized = hostname.trim().toLowerCase().replace(/^\[|\]$/g, "");
  return normalized === "localhost" || normalized === "127.0.0.1" || normalized === "::1";
}

function parseStreamPosition(value: string | null | undefined): number {
  const parsed = Number.parseInt(value?.trim() ?? "", 10);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}

function stringPayload(payload: Record<string, unknown>, key: string): string {
  return typeof payload[key] === "string" ? payload[key] : "";
}

function numberPayload(payload: Record<string, unknown>, key: string): number {
  return typeof payload[key] === "number" && Number.isSafeInteger(payload[key]) ? payload[key] : -1;
}

const activitySignalFields = [
  "aggregateId",
  "aggregateRevision",
  "aggregateType",
  "agentStepId",
  "agentTaskId",
  "commandId",
  "eventId",
  "nodeKey",
  "nodeRunId",
  "projectControlCommandId",
  "schemaVersion",
  "sessionId",
  "shotId",
  "storyboardShotId",
  "streamPosition",
  "workflowRunId",
  "workflowType",
] as const;

function activitySignalPayload(payload: Record<string, unknown>): Record<string, unknown> {
  const signal: Record<string, unknown> = {};
  for (const field of activitySignalFields) {
    const value = payload[field];
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
      signal[field] = value;
    }
  }
  return signal;
}

async function readRealtimeError(response: Response): Promise<RealtimeErrorBody> {
  try {
    return (await response.json()) as RealtimeErrorBody;
  } catch {
    return {};
  }
}

async function abortableDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) {
    return;
  }
  await new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, milliseconds);
    signal.addEventListener("abort", () => {
      window.clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}
