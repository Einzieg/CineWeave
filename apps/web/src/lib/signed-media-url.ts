export function retainUsableSignedMediaUrl(current?: string, candidate?: string, now = Date.now()) {
  if (!candidate) return current;
  if (!current || current === candidate || mediaUrlIdentity(current) !== mediaUrlIdentity(candidate)) return candidate;
  return shouldKeepCurrentSignedUrl(current, now) ? current : candidate;
}

function mediaUrlIdentity(rawUrl: string) {
  if (!rawUrl) return "";
  try {
    const url = new URL(rawUrl);
    return `${url.origin}${url.pathname}`;
  } catch {
    return rawUrl.split("?", 1)[0] ?? rawUrl;
  }
}

function shouldKeepCurrentSignedUrl(rawUrl: string, now: number) {
  const expiresAt = awsSignedUrlExpiresAt(rawUrl);
  return expiresAt !== null && expiresAt - now > 5 * 60_000;
}

function awsSignedUrlExpiresAt(rawUrl: string) {
  try {
    const url = new URL(rawUrl);
    const signedAt = url.searchParams.get("X-Amz-Date");
    const expiresSeconds = Number(url.searchParams.get("X-Amz-Expires"));
    const match = signedAt?.match(/^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/);
    if (!match || !Number.isFinite(expiresSeconds) || expiresSeconds <= 0) return null;
    const [, year, month, day, hour, minute, second] = match;
    return Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second)) + expiresSeconds * 1000;
  } catch {
    return null;
  }
}
