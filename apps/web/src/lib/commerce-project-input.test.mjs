import assert from "node:assert/strict";
import test from "node:test";

import { commerceScriptDraftMetrics } from "./commerce-script-metrics.ts";
import {
  commerceScriptExceedsPromptLimit,
  maximumExecutableDuration,
  measureCommerceScriptLength,
} from "./commerce-direct-video.ts";
import { commerceSetupPreparationGate } from "./commerce-setup-gate.ts";
import { commerceStoryboardStrategyAction } from "./commerce-storyboard-strategy.ts";
import { commerceTimingAdvisory } from "./commerce-timing-advisory.ts";
import { localizePlatformError } from "./error-localization.ts";
import { normalizeIdempotencyKey } from "./idempotency-key.ts";

test("short visible ASCII idempotency keys are kept unchanged", async () => {
  assert.equal(await normalizeIdempotencyKey("commerce:project:123"), "commerce:project:123");
});

test("Unicode idempotency keys become deterministic header-safe hashes", async () => {
  const source = "request:product-image:商品主图.png:1024";
  const first = await normalizeIdempotencyKey(source);
  const second = await normalizeIdempotencyKey(source);

  assert.equal(first, second);
  assert.match(first, /^cw-sha256-[0-9a-f]{64}$/);
  assert.doesNotThrow(() => new Headers({ "Idempotency-Key": first }));
});

test("long idempotency keys are hashed instead of exceeding the plain-key limit", async () => {
  const normalized = await normalizeIdempotencyKey(`request:${"a".repeat(300)}`);
  assert.match(normalized, /^cw-sha256-[0-9a-f]{64}$/);
});

test("empty idempotency keys stay empty", async () => {
  assert.equal(await normalizeIdempotencyKey("  "), "");
});

test("automatic script metrics count English words without treating the draft as Chinese", () => {
  const metrics = commerceScriptDraftMetrics(`
    SCENE 4 (9-12s)
    She naturally puts on the helmet.
    The motorcycle remains in the background.
    No subtitles. No text overlay.
  `);

  assert.deepEqual(metrics, {
    units: 20,
    unitLabel: "词",
    detectedLanguageLabel: "英文",
  });
});

test("Chinese script metrics count Han characters", () => {
  const metrics = commerceScriptDraftMetrics("镜头一：角色拿起产品。旁白：轻巧又耐用。", "zh-CN");

  assert.equal(metrics.unitLabel, "字");
  assert.equal(metrics.detectedLanguageLabel, "中文");
  assert.ok(metrics.units > 10);
});

test("Commerce direct video defaults to the maximum executable duration", () => {
  assert.equal(maximumExecutableDuration([6, 16, 10, 12]), 16);
  assert.equal(maximumExecutableDuration([]), 0);
});

test("Commerce script limits follow the configured model length unit", () => {
  assert.equal(measureCommerceScriptLength("蛊真人", "characters"), 3);
  assert.equal(measureCommerceScriptLength("蛊真人", "utf8_bytes"), 9);
  assert.equal(
    commerceScriptExceedsPromptLimit("蛊真人", { maxLength: 9, unit: "utf8_bytes" }),
    false,
  );
  assert.equal(
    commerceScriptExceedsPromptLimit("蛊真人啊", { maxLength: 9, unit: "utf8_bytes" }),
    true,
  );
});

test("commerce timing overrun remains an advisory against the selected target", () => {
  assert.deepEqual(commerceTimingAdvisory(29.8, 15), {
    estimatedSeconds: 29.8,
    targetSeconds: 15,
    deltaSeconds: 14.8,
    overTarget: true,
  });
  assert.deepEqual(commerceTimingAdvisory(12, 15), {
    estimatedSeconds: 12,
    targetSeconds: 15,
    deltaSeconds: 3,
    overTarget: false,
  });
});

test("script preparation redirects users to pending project setup actions", () => {
  assert.deepEqual(
    commerceSetupPreparationGate("setup-1", "waiting_user_confirmation"),
    { blocked: true, actionLabel: "正在自动识别视频语言" },
  );
  assert.deepEqual(
    commerceSetupPreparationGate("setup-1", "failed"),
    { blocked: true, actionLabel: "先重试项目准备" },
  );
  assert.deepEqual(
    commerceSetupPreparationGate("setup-1", "completed"),
    { blocked: false, actionLabel: "准备并生成分镜" },
  );
});

test("Temporal activity timeout details are hidden from project setup users", () => {
  assert.equal(
    localizePlatformError(
      "activity error (type: ExecuteCommerceProjectSetup, scheduledEventID: 5, startedEventID: 6, identity: worker): activity StartToClose timeout (type: StartToClose)",
      "ACTIVITY_FAILED",
    ),
    "任务步骤等待响应超时，请重试",
  );
});

test("legacy Commerce generations must rebuild before storyboard preview", () => {
  assert.equal(commerceStoryboardStrategyAction(undefined, "smart"), "rebuild");
  assert.equal(commerceStoryboardStrategyAction(undefined, "single_take"), "rebuild");
});

test("storyboard preview is available only for the frozen selected strategy", () => {
  assert.equal(commerceStoryboardStrategyAction("smart", "smart"), "preview");
  assert.equal(commerceStoryboardStrategyAction("smart", "single_take"), "rebuild");
  assert.equal(commerceStoryboardStrategyAction("single_take", "single_take"), "preview");
});
