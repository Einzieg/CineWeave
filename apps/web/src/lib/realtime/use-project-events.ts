"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { toast } from "sonner";
import { keysForProjectEvent, projectEventNames, toastForProjectEvent } from "@/lib/realtime/event-map";
import { orgScopedKey } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";

const realtimeBase = process.env.NEXT_PUBLIC_REALTIME_URL ?? "http://localhost:8081/api/realtime/events";

/**
 * 订阅项目级 SSE 事件,按 event-map 失效对应 query 并弹完成/失败 toast。
 *
 * 注意:realtime 网关每次连接会重放 event_outbox 历史(无游标),
 * 因此连接后的前几秒视为“重放窗口”,只静默失效缓存、不弹 toast;
 * 条件轮询是第一可靠机制,SSE 只做加速。
 */
export function useProjectEvents(projectId: string) {
  const queryClient = useQueryClient();
  const { session, ready } = useStudioSession();
  const organizationId = session.organizationId;

  useEffect(() => {
    if (!ready || !projectId) {
      return;
    }
    const source = new EventSource(`${realtimeBase}?projectId=${encodeURIComponent(projectId)}`);
    const toastArmedAt = Date.now() + 3000;
    const pending = new Set<string>();
    let flushTimer: number | null = null;
    let closed = false;

    const flush = () => {
      flushTimer = null;
      for (const raw of pending) {
        void queryClient.invalidateQueries({ queryKey: JSON.parse(raw) as unknown[] });
      }
      pending.clear();
    };

    const listeners = projectEventNames.map((eventName) => {
      const listener = (event: MessageEvent) => {
        if (closed) {
          return;
        }
        let payload: Record<string, unknown> = {};
        try {
          payload = JSON.parse(event.data) as Record<string, unknown>;
        } catch {
          payload = {};
        }
        for (const key of keysForProjectEvent(eventName, projectId)) {
          pending.add(JSON.stringify(orgScopedKey(organizationId, key)));
        }
        if (flushTimer === null && pending.size > 0) {
          flushTimer = window.setTimeout(flush, 500);
        }
        if (Date.now() > toastArmedAt) {
          const notice = toastForProjectEvent(eventName, payload);
          if (notice) {
            if (notice.kind === "error") {
              toast.error(notice.text);
            } else {
              toast.success(notice.text);
            }
          }
        }
      };
      source.addEventListener(eventName, listener);
      return [eventName, listener] as const;
    });

    return () => {
      closed = true;
      for (const [eventName, listener] of listeners) {
        source.removeEventListener(eventName, listener);
      }
      source.close();
      if (flushTimer !== null) {
        window.clearTimeout(flushTimer);
      }
    };
  }, [organizationId, projectId, queryClient, ready]);
}
