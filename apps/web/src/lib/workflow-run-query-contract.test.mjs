import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const webRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const workflowRunConsumers = [
  "components/layout/top-bar.tsx",
  "features/activity/activity-drawer.tsx",
  "features/assets/assets-page.tsx",
  "features/sources/sources-page.tsx",
  "features/storyboard/storyboard-page.tsx",
  "features/workflows/workflows-page.tsx",
];

test("workflow run queries cache the page envelope for every shared query key", () => {
  for (const relativePath of workflowRunConsumers) {
    const source = readFileSync(join(webRoot, relativePath), "utf8");
    const queryTransforms = source.match(/studioApi\.listWorkflowRuns\([^;\n]*?\)\.then\(/g) ?? [];
    assert.deepEqual(
      queryTransforms,
      [],
      `${relativePath} must cache ListEnvelope<WorkflowRun>; derive items after useApiQuery`,
    );
  }
});
