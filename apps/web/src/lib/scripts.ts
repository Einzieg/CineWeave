import type { Script } from "@/lib/types";

export function currentProjectScript(scripts: Script[]): Script | null {
  return (
    scripts.find((script) => script.isCurrent && script.status === "active" && script.currentVersionId) ??
    scripts.find((script) => script.isCurrent && script.currentVersionId) ??
    scripts.find((script) => script.status === "active" && script.currentVersionId) ??
    scripts.find((script) => script.currentVersionId) ??
    null
  );
}
