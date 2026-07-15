import type { StudioSession } from "./types";
import { retainUsableSignedMediaUrl } from "./signed-media-url";

const mediaCacheRoutePrefix = "/media-cache/v2";
const maxStableMediaRoutes = 4096;

type StableMediaRoute = {
  source: string;
  route: string;
};

const stableMediaRoutes = new Map<string, StableMediaRoute>();

export function wrapBrowserCachedMediaUrls<T>(value: T, session?: StudioSession): T {
  if (!browserMediaCacheAvailable() || !session) return value;
  const scope = mediaCacheScope(session);
  if (!scope) return value;
  return rewriteMediaUrls(value, scope, "") as T;
}

export function toBrowserCachedMediaUrl(source: string, scope: string) {
  if (!browserMediaCacheAvailable() || !source || !scope) return source;
  try {
    const sourceURL = new URL(source);
    if (sourceURL.origin === window.location.origin && sourceURL.pathname.startsWith(mediaCacheRoutePrefix)) return source;
    if ((sourceURL.protocol !== "http:" && sourceURL.protocol !== "https:") || !sourceURL.searchParams.has("X-Amz-Signature")) return source;
    const sourceIdentity = `${sourceURL.origin}${sourceURL.pathname}`;
    const scopeKey = base64URL(scope);
    const sourceKey = base64URL(sourceIdentity);
    const stableKey = `${scopeKey}:${sourceKey}`;
    const current = stableMediaRoutes.get(stableKey);
    if (current && retainUsableSignedMediaUrl(current.source, source) === current.source) {
      stableMediaRoutes.delete(stableKey);
      stableMediaRoutes.set(stableKey, current);
      return current.route;
    }

    const route = `${mediaCacheRoutePrefix}/${scopeKey}/${sourceKey}?source=${encodeURIComponent(source)}`;
    stableMediaRoutes.set(stableKey, { source, route });
    trimStableMediaRoutes();
    return route;
  } catch {
    return source;
  }
}

function rewriteMediaUrls(value: unknown, scope: string, key: string): unknown {
  if (typeof value === "string") {
    return isPreviewURLKey(key) ? toBrowserCachedMediaUrl(value, scope) : value;
  }
  if (Array.isArray(value)) return value.map((item) => rewriteMediaUrls(item, scope, key));
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).map(([childKey, childValue]) => [childKey, rewriteMediaUrls(childValue, scope, childKey)]));
}

function isPreviewURLKey(key: string) {
  return key.toLowerCase().endsWith("previewurl");
}

function mediaCacheScope(session: StudioSession) {
  const organizationID = session.organizationId.trim();
  const userID = session.user?.id?.trim() ?? "";
  if (!organizationID) return "";
  return `${organizationID}:${userID || "organization"}`;
}

function browserMediaCacheAvailable() {
  return typeof window !== "undefined" && window.isSecureContext && "serviceWorker" in navigator;
}

function trimStableMediaRoutes() {
  while (stableMediaRoutes.size > maxStableMediaRoutes) {
    const oldestKey = stableMediaRoutes.keys().next().value;
    if (typeof oldestKey !== "string") return;
    stableMediaRoutes.delete(oldestKey);
  }
}

function base64URL(value: string) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return window.btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}
