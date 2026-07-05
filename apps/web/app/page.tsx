"use client";

import { Loader2 } from "lucide-react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { studioApi } from "@/lib/api-client";
import { useStudioSession } from "@/lib/session";

export default function Home() {
  const router = useRouter();
  const { hydrated, ready } = useStudioSession();

  useEffect(() => {
    if (!hydrated) {
      return;
    }
    let cancelled = false;
    studioApi
      .getSetupState()
      .then((state) => {
        if (cancelled) {
          return;
        }
        if (state.needsSetup) {
          router.replace("/setup" as Route);
          return;
        }
        router.replace((ready ? "/projects" : "/login") as Route);
      })
      .catch(() => {
        if (!cancelled) {
          router.replace((ready ? "/projects" : "/login") as Route);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [hydrated, ready, router]);

  return (
    <main className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
      <span className="inline-flex items-center gap-2">
        <Loader2 className="h-4 w-4 animate-spin" />
        正在进入影织
      </span>
    </main>
  );
}
