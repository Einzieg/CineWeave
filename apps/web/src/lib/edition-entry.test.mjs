import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { EDITION_ENTRY_CONTRACT_VERSION } from "../edition/contract.ts";

test("community edition entry is an explicit no-op contract", () => {
  const source = readFileSync(new URL("../edition/community-entry.ts", import.meta.url), "utf8");
  const selectedSource = readFileSync(new URL("../edition/selected-entry.ts", import.meta.url), "utf8");
  assert.equal(EDITION_ENTRY_CONTRACT_VERSION, "edition.v1");
  assert.match(source, /contractVersion:\s*EDITION_ENTRY_CONTRACT_VERSION/);
  for (const slot of ["navigation", "routes", "queryClients", "entitlementGuards", "topBarItems"]) {
    assert.match(source, new RegExp(`${slot}:\\s*Object\\.freeze\\(\\[\\]\\)`));
  }
  assert.doesNotMatch(source, /process\.env|NEXT_PUBLIC_/);
  assert.match(
    selectedSource,
    /communityEditionEntry as selectedEditionEntry/,
    "Community source must select only the no-op Edition entry",
  );
});

test("edition denial reasons are localized before rendering", () => {
  const routeHost = readFileSync(
    new URL("../edition/route-host.tsx", import.meta.url),
    "utf8",
  );
  const labels = readFileSync(new URL("./labels.ts", import.meta.url), "utf8");
  const editionContract = JSON.parse(
    readFileSync(
      new URL("../../../../packages/edition/edition.v1.json", import.meta.url),
      "utf8",
    ),
  );
  assert.match(routeHost, /entitlementDenialLabel\(decision\?\.reason/);
  assert.doesNotMatch(routeHost, /授权原因：\{decision\?\.reason/);
  for (const denialCode of editionContract.denialCodes) {
    assert.match(
      labels,
      new RegExp(`\\b${denialCode}:`),
      `missing Chinese label for ${denialCode}`,
    );
  }
});
