import assert from "node:assert/strict";
import test from "node:test";
import {
  authorizationSessionKey,
  authorizationGuardState,
  hasLoadedPermission,
  isAuthorizationBootstrapReady,
  restorePersistedSession,
  toPersistedSession,
} from "./session-policy.ts";

const baseSession = {
  accessToken: "access",
  refreshToken: "refresh",
  organizationId: "org-a",
  workspaceId: "workspace-a",
  currentProjectId: "project-a",
};

test("permissions fail closed until the bootstrap snapshot is loaded", () => {
  assert.equal(hasLoadedPermission(baseSession, "provider.manage"), false);
  assert.equal(hasLoadedPermission({ ...baseSession, permissions: [] }, "provider.manage"), false);
  const providerReader = { ...baseSession, permissions: ["provider.read"] };
  assert.equal(hasLoadedPermission(providerReader, "provider.read"), true);
  assert.equal(hasLoadedPermission(providerReader, "provider.manage"), false);
  assert.equal(hasLoadedPermission({ ...baseSession, permissions: ["provider.manage"] }, "provider.manage"), true);
  assert.equal(hasLoadedPermission({ ...baseSession, permissions: ["admin.manage"] }, "provider.manage"), true);
});

test("persisted sessions exclude authorization and account snapshots", () => {
  const persisted = toPersistedSession({
    ...baseSession,
    user: { id: "user-a", email: "user@example.com" },
    permissions: ["admin.manage"],
    membership: {
      organizationId: "org-a",
      user: { id: "user-a", email: "user@example.com" },
      status: "active",
      accountManagementAllowed: true,
      createdAt: "2026-07-21T00:00:00Z",
      updatedAt: "2026-07-21T00:00:00Z",
      teams: [],
      roles: [],
    },
  });
  assert.deepEqual(Object.keys(persisted).sort(), [
    "accessToken",
    "currentProjectId",
    "organizationId",
    "refreshToken",
    "workspaceId",
  ]);
});

test("restoring local storage ignores forged permissions and identity data", () => {
  const restored = restorePersistedSession({
    ...baseSession,
    permissions: ["admin.manage"],
    user: { id: "attacker", email: "attacker@example.com" },
    membership: { status: "active" },
  });
  assert.equal(restored.permissions, undefined);
  assert.equal(restored.user, undefined);
  assert.equal(restored.membership, undefined);
  assert.equal(hasLoadedPermission(restored, "provider.manage"), false);
});

test("authorization identity changes with access token or organization", () => {
  const key = authorizationSessionKey(baseSession);
  assert.notEqual(key, authorizationSessionKey({ ...baseSession, accessToken: "next-access" }));
  assert.notEqual(key, authorizationSessionKey({ ...baseSession, organizationId: "org-b" }));
  assert.equal(authorizationSessionKey({ ...baseSession, accessToken: "" }), "");
});

test("authorization bootstrap remains closed for loading, error, and stale identities", () => {
  const sessionKey = authorizationSessionKey(baseSession);
  assert.equal(isAuthorizationBootstrapReady(baseSession, { status: "loading", sessionKey }), false);
  assert.equal(isAuthorizationBootstrapReady(baseSession, { status: "error", sessionKey }), false);
  assert.equal(isAuthorizationBootstrapReady(baseSession, { status: "ready", sessionKey: `${sessionKey}:stale` }), false);
  assert.equal(isAuthorizationBootstrapReady(baseSession, { status: "ready", sessionKey }), true);
  assert.equal(authorizationGuardState(baseSession, true, { status: "error", sessionKey }), "error");
  assert.equal(authorizationGuardState(baseSession, true, { status: "loading", sessionKey }), "loading");
  assert.equal(authorizationGuardState(baseSession, true, { status: "ready", sessionKey: `${sessionKey}:stale` }), "loading");
  assert.equal(authorizationGuardState(baseSession, true, { status: "ready", sessionKey }), "ready");
});
