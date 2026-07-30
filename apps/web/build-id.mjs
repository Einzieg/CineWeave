const immutableIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/;
const mutableIds = new Set([
  "latest",
  "main",
  "master",
  "dev",
  "development",
  "local-dev",
]);

export function resolveBuildId(rawValue = process.env.CINEWEAVE_RELEASE_ID) {
  const value = String(rawValue ?? "").trim();
  if (!value) {
    return null;
  }
  if (!immutableIdPattern.test(value) || mutableIds.has(value.toLowerCase())) {
    throw new Error("CINEWEAVE_RELEASE_ID must be an immutable release identifier");
  }
  return value;
}
