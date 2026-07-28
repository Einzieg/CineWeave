import {
  EDITION_ENTRY_CONTRACT_VERSION,
  type EditionEntry,
} from "./contract";

export const communityEditionEntry: EditionEntry = Object.freeze({
  contractVersion: EDITION_ENTRY_CONTRACT_VERSION,
  navigation: Object.freeze([]),
  routes: Object.freeze([]),
  queryClients: Object.freeze([]),
  entitlementGuards: Object.freeze([]),
});
