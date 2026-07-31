import type { DeploymentEdition } from "@/lib/types";

export function usesSystemManagedProviders(
  deploymentEdition: DeploymentEdition | undefined,
  commercialEntryCompiled: boolean,
) {
  return deploymentEdition === "cloud" || commercialEntryCompiled;
}
