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

test("model capability editor uses structured multi-select controls and defaults", () => {
  const pageSource = readFileSync(
    new URL("../features/providers/providers-page.tsx", import.meta.url),
    "utf8",
  );

  assert.match(pageSource, /function MultiSelectField/);
  assert.match(pageSource, /allowCustom\?: boolean/);
  assert.match(pageSource, /function uniqueCapabilityValues/);
  assert.match(pageSource, /function matchingCapabilityValue/);
  assert.match(pageSource, /\{ value: "video\.generate", label: "视频生成" \}/);
  assert.match(pageSource, /label="任务类型"[\s\S]{0,240}taskTypeOptionsForModality/);
  assert.match(pageSource, /label="支持输入类型"[\s\S]{0,300}inputTypeOptions[\s\S]{0,180}allowCustom/);
  assert.match(pageSource, /label="可用思考等级"[\s\S]{0,240}reasoningLevelOptions/);
  assert.match(pageSource, /<Label>默认图片质量<\/Label>/);
  assert.match(pageSource, /xCapabilities\.defaultQuality = defaultImageQuality/);
  assert.match(pageSource, /label="图片返回方式"[\s\S]{0,220}imageResponseFormatOptions/);
  assert.match(pageSource, /label="图片文件格式"[\s\S]{0,220}imageOutputFormatOptions/);
  assert.match(pageSource, /outputLimits\.outputFormats = imageOutputFormats/);
  assert.match(pageSource, /requestModes = uniqueStrings\(\[\.\.\.requestModes, \.\.\.imageRequestModes\]\)/);
  assert.match(pageSource, /requestModes = uniqueStrings\(\[\.\.\.requestModes, \.\.\.videoRequestModes\]\)/);
  assert.doesNotMatch(pageSource, /xCapabilities\.requestModes = videoRequestModes/);
  assert.match(pageSource, /function isFamilySpecificRequestMode/);
  assert.match(pageSource, /supportsCapabilityFamily\(modelForm\.modality, taskTypes, "image"\)/);
  assert.match(pageSource, /delete xCapabilities\.videoGenerationVariants/);
  assert.match(pageSource, /multimodal: \["text\.generate", "text\.stream"\]/);
  assert.match(pageSource, /sm:max-w-6xl/);
});
