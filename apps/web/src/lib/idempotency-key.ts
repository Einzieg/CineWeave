const SAFE_HEADER_VALUE = /^[\x21-\x7e]+$/;
const MAX_PLAIN_KEY_LENGTH = 180;

export async function normalizeIdempotencyKey(value?: string): Promise<string> {
  const source = value?.trim() ?? "";
  if (!source) return "";
  if (source.length <= MAX_PLAIN_KEY_LENGTH && SAFE_HEADER_VALUE.test(source)) {
    return source;
  }

  if (!globalThis.crypto?.subtle) {
    throw new Error("Web Crypto is unavailable");
  }

  const digest = await globalThis.crypto.subtle.digest("SHA-256", new TextEncoder().encode(source));
  const hex = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `cw-sha256-${hex}`;
}
