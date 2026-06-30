"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { AuthResponse, StudioSession } from "./types";

const sessionKey = "cineweave.session.v2";
const legacySessionKey = "cineweave.studio.session.v1";

export const emptySession: StudioSession = {
  accessToken: "",
  refreshToken: "",
  organizationId: "",
  workspaceId: "",
  currentProjectId: "",
};

export function sessionFromAuthResponse(response: AuthResponse, currentProjectId = ""): StudioSession {
  return {
    accessToken: response.accessToken,
    refreshToken: response.refreshToken,
    organizationId: response.organizationId,
    workspaceId: response.workspaceId ?? "",
    user: response.user,
    currentProjectId,
  };
}

export function readStoredSession(): StudioSession {
  if (typeof window === "undefined") {
    return emptySession;
  }
  try {
    const raw = window.localStorage.getItem(sessionKey);
    if (!raw) {
      window.localStorage.removeItem(legacySessionKey);
      return emptySession;
    }
    const parsed = JSON.parse(raw) as Partial<StudioSession>;
    return normalizeSession(parsed);
  } catch {
    return emptySession;
  }
}

export function writeStoredSession(session: StudioSession) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(sessionKey, JSON.stringify(session));
  window.localStorage.removeItem(legacySessionKey);
}

export function clearStoredSession() {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.removeItem(sessionKey);
  window.localStorage.removeItem(legacySessionKey);
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
    const normalized = normalizeSession(next);
    setSessionState(normalized);
    writeStoredSession(normalized);
  }, []);

  const updateSession = useCallback((patch: Partial<StudioSession>) => {
    setSessionState((current) => {
      const next = normalizeSession({ ...current, ...patch });
      writeStoredSession(next);
      return next;
    });
  }, []);

  const clearSession = useCallback(() => {
    setSessionState(emptySession);
    clearStoredSession();
  }, []);

  const ready = useMemo(() => Boolean(session.accessToken.trim() && session.organizationId.trim()), [session.accessToken, session.organizationId]);

  return { session, hydrated, ready, setSession, updateSession, clearSession };
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

function normalizeSession(session: Partial<StudioSession>): StudioSession {
  return {
    accessToken: String(session.accessToken ?? ""),
    refreshToken: String(session.refreshToken ?? ""),
    organizationId: String(session.organizationId ?? ""),
    workspaceId: String(session.workspaceId ?? ""),
    user: session.user,
    currentProjectId: String(session.currentProjectId ?? ""),
  };
}
