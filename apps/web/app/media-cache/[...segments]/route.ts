import { NextRequest, NextResponse } from "next/server";

export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ segments: string[] }> };

export async function GET(request: NextRequest, context: RouteContext) {
  const { segments } = await context.params;
  const source = request.nextUrl.searchParams.get("source")?.trim() ?? "";
  if (segments.length !== 3 || segments[0] !== "v2" || !source) return invalidMediaCacheRequest();

  let sourceURL: URL;
  try {
    sourceURL = new URL(source);
  } catch {
    return invalidMediaCacheRequest();
  }
  if (!validSignedMediaSource(sourceURL) || !matchesConfiguredStorageOrigin(sourceURL)) return invalidMediaCacheRequest();
  const sourceIdentity = `${sourceURL.origin}${sourceURL.pathname}`;
  if (segments[2] !== Buffer.from(sourceIdentity, "utf8").toString("base64url")) return invalidMediaCacheRequest();

  const response = NextResponse.redirect(sourceURL, 307);
  response.headers.set("Cache-Control", "private, no-store");
  response.headers.set("Referrer-Policy", "no-referrer");
  return response;
}

function validSignedMediaSource(sourceURL: URL) {
  return (sourceURL.protocol === "http:" || sourceURL.protocol === "https:") &&
    sourceURL.searchParams.has("X-Amz-Algorithm") &&
    sourceURL.searchParams.has("X-Amz-Credential") &&
    sourceURL.searchParams.has("X-Amz-Signature");
}

function matchesConfiguredStorageOrigin(sourceURL: URL) {
  const configured = process.env.S3_PUBLIC_ENDPOINT?.trim();
  if (!configured) return sourceURL.hostname === "localhost" || sourceURL.hostname === "127.0.0.1";
  try {
    return sourceURL.origin === new URL(configured).origin;
  } catch {
    return false;
  }
}

function invalidMediaCacheRequest() {
  return NextResponse.json({ error: "invalid media cache request" }, { status: 400, headers: { "Cache-Control": "no-store" } });
}
