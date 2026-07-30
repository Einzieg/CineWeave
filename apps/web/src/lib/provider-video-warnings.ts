import type { JsonRecord, ProviderVideoOutputWarning } from "@/lib/types";

export const providerVideoLayoutMismatchCode = "PROVIDER_VIDEO_LAYOUT_MISMATCH";

export function parseProviderVideoWarnings(value: unknown): ProviderVideoOutputWarning[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    const record = asRecord(item);
    const code = stringValue(record?.code);
    if (!record || !code) return [];
    return [{
      code,
      message: stringValue(record.message),
      category: stringValue(record.category),
      expectedAspectRatio: stringValue(record.expectedAspectRatio),
      actualAspectRatio: stringValue(record.actualAspectRatio),
      requestedSize: stringValue(record.requestedSize),
      providerSize: stringValue(record.providerSize),
      width: numberValue(record.width),
      height: numberValue(record.height),
    }];
  });
}

export function videoArtifactWarnings(metadata?: JsonRecord): ProviderVideoOutputWarning[] {
  const direct = parseProviderVideoWarnings(metadata?.warnings);
  if (direct.length > 0) return direct;
  const mediaProbe = asRecord(metadata?.mediaProbe);
  return parseProviderVideoWarnings(mediaProbe?.warnings);
}

export function providerVideoWarningMessage(warning: ProviderVideoOutputWarning) {
  if (warning.code === providerVideoLayoutMismatchCode) {
    const actual = warning.providerSize
      || (warning.width && warning.height ? `${warning.width}x${warning.height}` : "")
      || warning.actualAspectRatio
      || "未知尺寸";
    const expected = warning.expectedAspectRatio || warning.requestedSize || "项目画幅";
    return `供应商实际生成 ${actual}，与预期 ${expected} 不一致。视频已保留，可正常查看和使用。`;
  }
  return warning.message || "供应商输出存在能力偏差，视频已保留。";
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
