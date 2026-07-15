"use client";

import { useEffect } from "react";
import { useStudioSession } from "@/lib/session";

const cacheOwnerKey = "cineweave.media-cache.owner.v1";
const cacheNamePrefix = "cineweave-media-";

export function MediaCacheRegistration() {
  const { session, hydrated } = useStudioSession();
  const organizationID = session.organizationId.trim();
  const userID = session.user?.id?.trim() ?? "";

  useEffect(() => {
    if (!hydrated || !window.isSecureContext || !("serviceWorker" in navigator)) return;
    let cancelled = false;
    const owner = organizationID ? `${organizationID}:${userID || "organization"}` : "";

    async function register() {
      const registration = await navigator.serviceWorker.register("/media-cache-sw.js?revision=2", { scope: "/" });
      await navigator.serviceWorker.ready;
      if (cancelled) return;

      const previousOwner = window.localStorage.getItem(cacheOwnerKey) ?? "";
      if (previousOwner && previousOwner !== owner) await clearMediaCaches();
      if (owner) window.localStorage.setItem(cacheOwnerKey, owner);
      else window.localStorage.removeItem(cacheOwnerKey);
      void registration.update();
    }

    void register().catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [hydrated, organizationID, userID]);

  return null;
}

async function clearMediaCaches() {
  const names = await window.caches.keys();
  await Promise.all(names.filter((name) => name.startsWith(cacheNamePrefix)).map((name) => window.caches.delete(name)));
}
