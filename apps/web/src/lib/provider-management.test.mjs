import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { usesSystemManagedProviders } from "../edition/provider-management.ts";

test("cloud organizations always use system-managed providers", () => {
  assert.equal(usesSystemManagedProviders("cloud", false), true);
  assert.equal(usesSystemManagedProviders("cloud", true), true);
});

test("commercial assembly fails closed before edition data loads", () => {
  assert.equal(usesSystemManagedProviders(undefined, true), true);
  assert.equal(usesSystemManagedProviders("community", true), true);
});

test("community self-hosting keeps tenant-managed providers", () => {
  assert.equal(usesSystemManagedProviders(undefined, false), false);
  assert.equal(usesSystemManagedProviders("community", false), false);
});

test("provider navigation mode does not depend on system administrator identity", () => {
  for (const relativePath of [
    "../components/layout/main-sidebar.tsx",
    "../features/providers/providers-page.tsx",
  ]) {
    const source = readFileSync(new URL(relativePath, import.meta.url), "utf8");
    assert.match(source, /usesSystemManagedProviders/);
    assert.doesNotMatch(source, /organizationModelOnly[\s\S]{0,160}systemAdministrator/);
  }
});

test("system-managed provider mode keeps organization-safe model details editing", () => {
  const pageSource = readFileSync(
    new URL("../features/providers/providers-page.tsx", import.meta.url),
    "utf8",
  );
  const clientSource = readFileSync(new URL("api-client.ts", import.meta.url), "utf8");

  assert.match(pageSource, /模型详细配置/);
  assert.match(pageSource, /studioApi\.updateAvailableProviderModel/);
  assert.match(pageSource, /模型标识（平台维护）/);
  assert.match(clientSource, /updateAvailableProviderModel/);
  assert.match(clientSource, /`\/api\/provider-models\/\$\{modelId\}`/);
});
