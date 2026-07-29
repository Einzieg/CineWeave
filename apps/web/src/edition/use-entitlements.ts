"use client";

import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiQuery } from "@/lib/query/use-api";
import type {
  EditionFeatureKey,
  EntitlementDecision,
  EntitlementSnapshot,
} from "@/lib/types";

export function useEditionEntitlements(enabled = true) {
  return useApiQuery({
    key: qk.editionEntitlements(),
    queryFn: studioApi.getMyEntitlements,
    enabled,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}

export function editionFeatureDecision(
  snapshot: EntitlementSnapshot | undefined,
  featureKey: EditionFeatureKey,
): EntitlementDecision | undefined {
  return snapshot?.decisions.find(
    (decision) => decision.featureKey === featureKey,
  );
}

export function editionFeatureAllowed(
  snapshot: EntitlementSnapshot | undefined,
  featureKey: EditionFeatureKey,
) {
  return editionFeatureDecision(snapshot, featureKey)?.allowed === true;
}
