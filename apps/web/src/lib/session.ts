"use client";

import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { AuthResponse, PendingOrganizationSelection, StudioSession } from "./types";
import {
  authorizationSessionKey,
  hasLoadedPermission,
  isAuthorizationBootstrapReady,
  normalizeSession,
  restorePersistedSession,
  toPersistedSession,
} from "./session-policy";

export { authorizationGuardState, authorizationSessionKey } from "./session-policy";

const sessionKey = "cineweave.session.v2";

export type AuthorizationBootstrapStatus = "idle" | "loading" | "ready" | "error";

type AuthorizationBootstrapState = {
  status: AuthorizationBootstrapStatus;
  sessionKey: string;
  error: string;
};

type AuthorizationSnapshot = Pick<StudioSession, "user" | "membership" | "permissions"> & {
  organizationId?: string;
  workspaceId?: string;
};

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
      return emptySession;
    }
    return restorePersistedSession(JSON.parse(raw));
  } catch {
    return emptySession;
  }
}

export function writeStoredSession(session: StudioSession) {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(sessionKey, JSON.stringify(toPersistedSession(session)));
}

export function clearStoredSession() {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.removeItem(sessionKey);
}

type StudioSessionController = ReturnType<typeof useStudioSessionState>;

const StudioSessionContext = createContext<StudioSessionController | null>(null);

function useStudioSessionState() {
  const [session, setSessionState] = useState<StudioSession>(emptySession);
  const sessionRef = useRef<StudioSession>(emptySession);
  const [authorizationBootstrap, setAuthorizationBootstrapState] = useState<AuthorizationBootstrapState>({
    status: "idle",
    sessionKey: "",
    error: "",
  });
  const authorizationBootstrapRef = useRef(authorizationBootstrap);
  const [pendingOrganizationSelection, setPendingOrganizationSelectionState] = useState<PendingOrganizationSelection | null>(null);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    const stored = readStoredSession();
    sessionRef.current = stored;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSessionState(stored);
    const bootstrap = idleAuthorizationBootstrap(stored);
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
    setHydrated(true);
  }, []);

  const setSession = useCallback((next: StudioSession) => {
    const normalized = normalizeSession(next);
    sessionRef.current = normalized;
    setSessionState(normalized);
    writeStoredSession(normalized);
    const bootstrap = idleAuthorizationBootstrap(normalized);
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
    setPendingOrganizationSelectionState(null);
  }, []);

  const setPendingOrganizationSelection = useCallback((next: PendingOrganizationSelection | null) => {
    setPendingOrganizationSelectionState(next);
  }, []);

  const updateSession = useCallback((patch: Partial<StudioSession>) => {
    const current = sessionRef.current;
    const next = normalizeSession({ ...current, ...patch });
    sessionRef.current = next;
    setSessionState(next);
    writeStoredSession(next);
    if (authorizationSessionKey(current) !== authorizationSessionKey(next)) {
      const bootstrap = idleAuthorizationBootstrap(next);
      authorizationBootstrapRef.current = bootstrap;
      setAuthorizationBootstrapState(bootstrap);
    }
  }, []);

  const clearSession = useCallback(() => {
    sessionRef.current = emptySession;
    setSessionState(emptySession);
    const bootstrap = idleAuthorizationBootstrap(emptySession);
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
    setPendingOrganizationSelectionState(null);
    clearStoredSession();
  }, []);

  const beginAuthorizationBootstrap = useCallback((expectedSessionKey: string) => {
    if (!expectedSessionKey || authorizationSessionKey(sessionRef.current) !== expectedSessionKey) {
      return false;
    }
    const current = authorizationBootstrapRef.current;
    if (current.sessionKey === expectedSessionKey && (current.status === "loading" || current.status === "ready")) {
      return false;
    }
    const next = { status: "loading" as const, sessionKey: expectedSessionKey, error: "" };
    authorizationBootstrapRef.current = next;
    setAuthorizationBootstrapState(next);
    return true;
  }, []);

  const completeAuthorizationBootstrap = useCallback((expectedSessionKey: string, snapshot: AuthorizationSnapshot) => {
    const current = sessionRef.current;
    if (!expectedSessionKey || authorizationSessionKey(current) !== expectedSessionKey) {
      return false;
    }
    const next = normalizeSession({
      ...current,
      organizationId: snapshot.organizationId || current.organizationId,
      workspaceId: snapshot.workspaceId ?? current.workspaceId,
      user: snapshot.user,
      membership: snapshot.membership,
      permissions: snapshot.permissions,
    });
    sessionRef.current = next;
    setSessionState(next);
    writeStoredSession(next);
    const bootstrap = { status: "ready" as const, sessionKey: expectedSessionKey, error: "" };
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
    return true;
  }, []);

  const failAuthorizationBootstrap = useCallback((expectedSessionKey: string, error: string) => {
    if (!expectedSessionKey || authorizationSessionKey(sessionRef.current) !== expectedSessionKey) {
      return false;
    }
    const bootstrap = { status: "error" as const, sessionKey: expectedSessionKey, error };
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
    return true;
  }, []);

  const retryAuthorizationBootstrap = useCallback(() => {
    const bootstrap = idleAuthorizationBootstrap(sessionRef.current);
    authorizationBootstrapRef.current = bootstrap;
    setAuthorizationBootstrapState(bootstrap);
  }, []);

  const ready = useMemo(() => Boolean(session.accessToken.trim() && session.organizationId.trim()), [session.accessToken, session.organizationId]);
  const authorizationReady = isAuthorizationBootstrapReady(session, authorizationBootstrap);

  return {
    session,
    hydrated,
    ready,
    authorizationStatus: authorizationBootstrap.status,
    authorizationSessionKey: authorizationBootstrap.sessionKey,
    authorizationError: authorizationBootstrap.error,
    authorizationReady,
    pendingOrganizationSelection,
    setPendingOrganizationSelection,
    setSession,
    updateSession,
    clearSession,
    beginAuthorizationBootstrap,
    completeAuthorizationBootstrap,
    failAuthorizationBootstrap,
    retryAuthorizationBootstrap,
  };
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

export function sessionHasPermission(session: StudioSession, permission: string) {
  return hasLoadedPermission(session, permission);
}

function idleAuthorizationBootstrap(session: StudioSession): AuthorizationBootstrapState {
  return {
    status: "idle",
    sessionKey: authorizationSessionKey(session),
    error: "",
  };
}
