"use client";

import { create } from "zustand";

export type ActivityRealtimeStatus = "idle" | "connected" | "reconnecting" | "disconnected";

export type ActivityRealtimeEvent = {
  id: string;
  projectId: string;
  eventType: string;
  payload: Record<string, unknown>;
  receivedAt: string;
};

type ActivityStore = {
  connectionByProject: Record<string, ActivityRealtimeStatus>;
  eventsByProject: Record<string, ActivityRealtimeEvent[]>;
  setConnectionStatus: (projectId: string, status: ActivityRealtimeStatus) => void;
  recordEvent: (projectId: string, eventType: string, payload: Record<string, unknown>, eventId?: string) => void;
  clearProjectEvents: (projectId: string) => void;
};

const maxEventsPerProject = 200;

function fallbackId(projectId: string, eventType: string) {
  return `${projectId}:${eventType}:${Date.now()}:${Math.random().toString(36).slice(2)}`;
}

export const useActivityStore = create<ActivityStore>((set) => ({
  connectionByProject: {},
  eventsByProject: {},
  setConnectionStatus: (projectId, status) =>
    set((state) => ({
      connectionByProject: {
        ...state.connectionByProject,
        [projectId]: status,
      },
    })),
  recordEvent: (projectId, eventType, payload, eventId) =>
    set((state) => {
      const id = eventId || fallbackId(projectId, eventType);
      const current = state.eventsByProject[projectId] ?? [];
      if (current.some((item) => item.id === id)) {
        return state;
      }
      const next = [
        ...current,
        {
          id,
          projectId,
          eventType,
          payload,
          receivedAt: new Date().toISOString(),
        },
      ].slice(-maxEventsPerProject);
      return {
        eventsByProject: {
          ...state.eventsByProject,
          [projectId]: next,
        },
      };
    }),
  clearProjectEvents: (projectId) =>
    set((state) => ({
      eventsByProject: {
        ...state.eventsByProject,
        [projectId]: [],
      },
    })),
}));
