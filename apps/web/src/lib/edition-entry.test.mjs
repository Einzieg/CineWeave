import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { EDITION_ENTRY_CONTRACT_VERSION } from "../edition/contract.ts";

test("community edition entry is an explicit no-op contract", () => {
  const source = readFileSync(new URL("../edition/community-entry.ts", import.meta.url), "utf8");
  assert.equal(EDITION_ENTRY_CONTRACT_VERSION, "edition.v1");
  assert.match(source, /contractVersion:\s*EDITION_ENTRY_CONTRACT_VERSION/);
  for (const slot of ["navigation", "routes", "queryClients", "entitlementGuards"]) {
    assert.match(source, new RegExp(`${slot}:\\s*Object\\.freeze\\(\\[\\]\\)`));
  }
  assert.doesNotMatch(source, /process\.env|NEXT_PUBLIC_/);
});
