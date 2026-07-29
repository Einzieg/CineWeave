import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { EditionFeatureKey, EntitlementDenialCode } from "@/lib/types";

export const EDITION_ENTRY_CONTRACT_VERSION = "edition.v2" as const;

export type EditionNavigationRegistration = {
  key: string;
  label: string;
  href: `/edition/${string}`;
  icon: LucideIcon;
  section: string;
  featureKey: EditionFeatureKey;
  systemOnly: boolean;
};

export type EditionRouteRegistration = {
  key: string;
  pathname: `/edition/${string}`;
  featureKey: EditionFeatureKey;
  component: ComponentType;
};

export type EditionQueryClientRegistration = {
  namespace: string;
  featureKey: EditionFeatureKey;
  queryKey: (...segments: string[]) => readonly string[];
};

export type EditionEntitlementGuardRegistration = {
  featureKey: EditionFeatureKey;
  deniedReasons: readonly EntitlementDenialCode[];
  behavior: "not_found" | "forbidden" | "upgrade";
};

export type EditionTopBarRegistration = {
  key: string;
  featureKey: EditionFeatureKey;
  component: ComponentType;
};

export type EditionEntry = {
  contractVersion: typeof EDITION_ENTRY_CONTRACT_VERSION;
  navigation: readonly EditionNavigationRegistration[];
  routes: readonly EditionRouteRegistration[];
  queryClients: readonly EditionQueryClientRegistration[];
  entitlementGuards: readonly EditionEntitlementGuardRegistration[];
  topBarItems: readonly EditionTopBarRegistration[];
};
