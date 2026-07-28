import assert from "node:assert/strict";
import test from "node:test";

import { qk } from "./query/keys.ts";

test("canonical asset list keys normalize defaults and isolate preview projections", () => {
  const defaultMetadata = qk.assets("project-1");
  const explicitMetadata = qk.assets("project-1", {
    status: "active",
    includePreviewUrl: false,
    previewExpiresSeconds: 1200,
  });
  const preview = qk.assetPreviews("project-1", {
    status: "active",
    includePreviewUrl: true,
    previewExpiresSeconds: 900,
  });

  assert.deepEqual(defaultMetadata, explicitMetadata);
  assert.notDeepEqual(defaultMetadata, preview);
  assert.ok([...defaultMetadata, ...preview].every((part) => typeof part !== "object"));
});

test("canonical asset list keys include every supported filter", () => {
  assert.notDeepEqual(
    qk.assets("project-1", { status: "active", assetType: "character" }),
    qk.assets("project-1", { status: "active", assetType: "scene" }),
  );
  assert.notDeepEqual(
    qk.assets("project-1", { status: "active" }),
    qk.assets("project-1", { status: "archived" }),
  );
  assert.notDeepEqual(
    qk.assetPreviews("project-1", { previewExpiresSeconds: 900 }),
    qk.assetPreviews("project-1", { previewExpiresSeconds: 1200 }),
  );
});

test("asset roots are stable prefixes for every list and detail shape", () => {
  const root = qk.assetsRoot("project-1");
  for (const key of [
    qk.assets("project-1"),
    qk.assets("project-1", { status: "archived", assetType: "prop" }),
    qk.assetPreviews("project-1"),
    qk.asset("project-1", "asset-1", true, 900),
  ]) {
    assert.deepEqual(key.slice(0, root.length), root);
  }
});

test("asset reference metadata and preview keys cannot collide", () => {
  const root = qk.assetReferencesRoot("project-1", "asset-1");
  const metadata = qk.assetReferences("project-1", "asset-1");
  const preview = qk.assetReferences("project-1", "asset-1", true, 900);

  assert.notDeepEqual(metadata, preview);
  assert.deepEqual(metadata.slice(0, root.length), root);
  assert.deepEqual(preview.slice(0, root.length), root);
});

test("workflow run list keys isolate activity pages while preserving the invalidation root", () => {
  const root = qk.workflowRuns("project-1");
  const active = qk.workflowRuns("project-1", { status: "active", view: "activity", limit: 100 });
  const firstTerminalPage = qk.workflowRuns("project-1", { status: "terminal", view: "activity", limit: 20 });
  const nextTerminalPage = qk.workflowRuns("project-1", {
    status: "terminal",
    view: "activity",
    limit: 20,
    cursor: "cursor-2",
  });

  assert.deepEqual(active.slice(0, root.length), root);
  assert.deepEqual(firstTerminalPage.slice(0, root.length), root);
  assert.notDeepEqual(active, firstTerminalPage);
  assert.notDeepEqual(firstTerminalPage, nextTerminalPage);
});
