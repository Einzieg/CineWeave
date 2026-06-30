"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { StudioSession } from "./types";

const sessionKey = "cineweave.studio.session.v1";

export const emptySession: StudioSession = {
  accessToken: "",
  currentUserId: "",
  organizationId: "",
  workspaceId: "",
  currentProjectId: "",
};

export function readStoredSession(): StudioSession {
  if (typeof window === "undefined") {
    return emptySession;
  }
  try {
    const raw = window.localStorage.getItem(sessionKey);
    if (!raw) {
      return emptySession;
    }
    const parsed = JSON.parse(raw) as Partial<StudioSession>;
    return {
      accessToken: String(parsed.accessToken ?? ""),
      currentUserId: String(parsed.currentUserId ?? ""),
      organizationId: String(parsed.organizationId ?? ""),
      workspaceId: String(parsed.workspaceId ?? ""),
      currentProjectId: String(parsed.currentProjectId ?? ""),
    };
  } catch {
    return emptySession;
  }
}

export function writeStoredSession(session: StudioSession) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(sessionKey, JSON.stringify(session));
}

type StudioSessionController = ReturnType<typeof useStudioSessionState>;

const StudioSessionContext = createContext<StudioSessionController | null>(null);

function useStudioSessionState() {
  const [session, setSessionState] = useState<StudioSession>(emptySession);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSessionState(readStoredSession());
    setHydrated(true);
  }, []);

  const setSession = useCallback((next: StudioSession) => {
    setSessionState(next);
    writeStoredSession(next);
  }, []);

  const updateSession = useCallback(
    (patch: Partial<StudioSession>) => {
      setSessionState((current) => {
        const next = { ...current, ...patch };
        writeStoredSession(next);
        return next;
      });
    },
    [],
  );

  const ready = useMemo(
    () => Boolean(session.accessToken.trim() && session.organizationId.trim()),
    [session.accessToken, session.organizationId],
  );

  return { session, hydrated, ready, setSession, updateSession };
}

export function StudioSessionProvider({ children }: { children: ReactNode }) {
  const value = useStudioSessionState();
  return createElement(StudioSessionContext.Provider, { value }, children);
}

export function useStudioSession() {
  const value = useContext(StudioSessionContext);
  if (!value) {
    throw new Error("useStudioSession must be used inside StudioSessionProvider");
  }
  return value;
}

export function useBindCurrentProject(projectId?: string) {
  const { session, hydrated, updateSession } = useStudioSession();
  useEffect(() => {
    if (!hydrated) {
      return;
    }
    if (projectId && session.currentProjectId !== projectId) {
      updateSession({ currentProjectId: projectId });
    }
  }, [hydrated, projectId, session.currentProjectId, updateSession]);
}
