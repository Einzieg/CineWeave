import assert from "node:assert/strict";
import test from "node:test";

import { workflowEpisodeProgressLabel } from "./workflow-progress-label.ts";

test("source to script labels a single target without implying a full-series run", () => {
  assert.equal(
    workflowEpisodeProgressLabel({
      workflowType: "source_to_script",
      episodeIndex: 1,
      episodeTotal: 199,
      batchIndex: 1,
      batchTotal: 1,
      totalItems: 1,
    }),
    "仅生成第 1 集",
  );
});

test("source to script distinguishes batch progress from the full-series ordinal", () => {
  assert.equal(
    workflowEpisodeProgressLabel({
      workflowType: "source_to_script",
      episodeIndex: 8,
      episodeTotal: 199,
      batchIndex: 2,
      batchTotal: 5,
      totalItems: 5,
    }),
    "本次 2/5 · 全书第 8 集",
  );
});

test("other workflows retain series progress labels", () => {
  assert.equal(
    workflowEpisodeProgressLabel({
      workflowType: "script_to_storyboard",
      episodeIndex: 2,
      episodeTotal: 10,
      batchIndex: 0,
      batchTotal: 0,
      totalItems: 0,
    }),
    "第 2/10 集",
  );
});
