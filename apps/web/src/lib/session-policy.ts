import type { StudioSession } from "./types";

export type PersistedStudioSession = Pick<
  StudioSession,
  "accessToken" | "refreshToken" | "organizationId" | "workspaceId" | "currentProjectId"
>;

export function normalizeSession(session: Partial<StudioSession>): StudioSession {
  return {
    accessToken: String(session.accessToken ?? ""),
    refreshToken: String(session.refreshToken ?? ""),
    organizationId: String(session.organizationId ?? ""),
    workspaceId: String(session.workspaceId ?? ""),
    user: session.user,
    membership: session.membership,
    permissions: Array.isArray(session.permissions) ? session.permissions.map(String) : undefined,
    currentProjectId: String(session.currentProjectId ?? ""),
  };
}

export function toPersistedSession(session: StudioSession): PersistedStudioSession {
  return {
    accessToken: session.accessToken,
    refreshToken: session.refreshToken,
    organizationId: session.organizationId,
    workspaceId: session.workspaceId ?? "",
    currentProjectId: session.currentProjectId,
  };
}

export function restorePersistedSession(value: unknown): StudioSession {
  const stored = isRecord(value) ? value : {};
  return normalizeSession({
    accessToken: storedString(stored.accessToken),
    refreshToken: storedString(stored.refreshToken),
    organizationId: storedString(stored.organizationId),
    workspaceId: storedString(stored.workspaceId),
    currentProjectId: storedString(stored.currentProjectId),
  });
}

export function hasLoadedPermission(session: StudioSession, permission: string) {
  if (!Array.isArray(session.permissions)) {
    return false;
  }
  return session.permissions.includes("admin.manage") || session.permissions.includes(permission);
}

export function authorizationSessionKey(session: StudioSession) {
  const accessToken = session.accessToken.trim();
  const organizationId = session.organizationId.trim();
  return accessToken && organizationId ? `${accessToken}:${organizationId}` : "";
}

export function isAuthorizationBootstrapReady(
  session: StudioSession,
  bootstrap: { status: string; sessionKey: string },
) {
  return bootstrap.status === "ready" && bootstrap.sessionKey === authorizationSessionKey(session);
}

export function authorizationGuardState(
  session: StudioSession,
  hydrated: boolean,
  bootstrap: { status: string; sessionKey: string },
): "loading" | "error" | "ready" {
  const currentSessionKey = authorizationSessionKey(session);
  if (hydrated && currentSessionKey && bootstrap.status === "error" && bootstrap.sessionKey === currentSessionKey) {
    return "error";
  }
  if (!hydrated || !currentSessionKey || !isAuthorizationBootstrapReady(session, bootstrap)) {
    return "loading";
  }
  return "ready";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function storedString(value: unknown) {
  return typeof value === "string" ? value : undefined;
}
