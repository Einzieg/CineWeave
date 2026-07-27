import type { CommercePromptLengthConstraint } from "./types";

export function maximumExecutableDuration(values: readonly number[]): number {
  return values.reduce((maximum, value) => (
    Number.isInteger(value) && value > maximum ? value : maximum
  ), 0);
}

export function measureCommerceScriptLength(
  content: string,
  unit: CommercePromptLengthConstraint["unit"],
): number {
  const normalized = content.trim();
  if (unit === "utf8_bytes") {
    return new TextEncoder().encode(normalized).length;
  }
  return Array.from(normalized).length;
}

export function commercePromptLengthUnitLabel(
  unit: CommercePromptLengthConstraint["unit"],
): string {
  return unit === "utf8_bytes" ? "UTF-8 字节" : "Unicode 字符";
}

export function commerceScriptExceedsPromptLimit(
  content: string,
  constraint?: CommercePromptLengthConstraint,
): boolean {
  return Boolean(
    constraint
    && constraint.maxLength > 0
    && measureCommerceScriptLength(content, constraint.unit) > constraint.maxLength,
  );
}
