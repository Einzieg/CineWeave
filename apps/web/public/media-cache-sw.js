const CACHE_VERSION = "v2";
const MEDIA_ROUTE_PREFIX = `/media-cache/${CACHE_VERSION}/`;
const STATUS_PATH = "/media-cache/status";
const FULL_CACHE = `cineweave-media-full-${CACHE_VERSION}`;
const CHUNK_CACHE = `cineweave-media-chunks-${CACHE_VERSION}`;
const CACHE_NAME_PREFIX = "cineweave-media-";
const CHUNK_BYTES = 4 * 1024 * 1024;
const MAX_RANGE_BYTES = 8 * 1024 * 1024;
const MAX_FULL_ENTRIES = 600;
const MAX_CHUNK_ENTRIES = 512;
const STREAM_EXTENSIONS = /\.(?:mp4|m4v|mov|webm|mkv|mp3|m4a|aac|wav|flac|ogg)$/iu;
const inFlightChunks = new Map();
const cacheInsertCounts = new Map();

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const current = new Set([FULL_CACHE, CHUNK_CACHE]);
    const names = await caches.keys();
    await Promise.all(names.filter((name) => name.startsWith(CACHE_NAME_PREFIX) && !current.has(name)).map((name) => caches.delete(name)));
    await self.clients.claim();
  })());
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === STATUS_PATH) {
    event.respondWith(mediaCacheStatus());
    return;
  }
  if (!url.pathname.startsWith(MEDIA_ROUTE_PREFIX)) return;
  event.respondWith(handleMediaRequest(event));
});

async function handleMediaRequest(event) {
  if (event.request.method !== "GET") return new Response("method not allowed", { status: 405 });
  const route = parseMediaRoute(event.request.url);
  if (!route) return new Response("invalid media cache request", { status: 400 });

  const range = event.request.headers.get("range");
  if (range) {
    try {
      return await handleRangeRequest(route, range, event.request);
    } catch {
      return fetchSource(route.source, range, event.request);
    }
  }
  if (event.request.destination === "video" || event.request.destination === "audio" || STREAM_EXTENSIONS.test(route.source.pathname)) {
    try {
      // Chromium often starts media playback without a Range header when a
      // service worker owns the URL. Return the first bounded range so the
      // player learns the total size and all subsequent reads use the same
      // chunk cache instead of downloading the complete object every time.
      return await handleRangeRequest(route, "bytes=0-", event.request);
    } catch {
      return fetchSource(route.source, "", event.request);
    }
  }
  return handleFullObject(route, event);
}

function parseMediaRoute(rawURL) {
  try {
    const url = new URL(rawURL);
    const parts = url.pathname.slice(MEDIA_ROUTE_PREFIX.length).split("/").filter(Boolean);
    const source = new URL(url.searchParams.get("source") || "");
    if (parts.length !== 2 || (source.protocol !== "http:" && source.protocol !== "https:")) return null;
    if (!source.searchParams.has("X-Amz-Signature")) return null;
    if (parts[1] !== base64URL(`${source.origin}${source.pathname}`)) return null;
    return {
      scopeKey: parts[0],
      sourceKey: parts[1],
      source,
      cachePath: `${MEDIA_ROUTE_PREFIX}${parts[0]}/${parts[1]}`,
    };
  } catch {
    return null;
  }
}

async function handleFullObject(route, event) {
  const cache = await caches.open(FULL_CACHE);
  const cacheKey = new Request(new URL(route.cachePath, self.location.origin), { method: "GET" });
  const cached = await cache.match(cacheKey);
  if (cached) return withCacheHeader(cached, "hit");

  const response = await fetchSource(route.source, "", event.request);
  if (!response.ok || response.status !== 200 || response.type === "opaque") return response;
  const cacheCopy = response.clone();
  event.waitUntil(putBounded(FULL_CACHE, cacheKey, cacheCopy, MAX_FULL_ENTRIES));
  return withCacheHeader(response, "miss");
}

async function handleRangeRequest(route, rangeHeader, originalRequest) {
  const range = parseSingleRange(rangeHeader);
  if (!range || range.suffixLength !== null) return fetchSource(route.source, rangeHeader, originalRequest);

  const firstIndex = Math.floor(range.start / CHUNK_BYTES);
  const firstChunk = await loadChunk(route, firstIndex, originalRequest);
  const total = firstChunk.total;
  if (range.start >= total) {
    return new Response(null, { status: 416, headers: { "Content-Range": `bytes */${total}`, "Accept-Ranges": "bytes" } });
  }

  let end = range.end === null ? range.start + CHUNK_BYTES - 1 : range.end;
  end = Math.min(end, range.start + MAX_RANGE_BYTES - 1, total - 1);
  if (end < range.start) return new Response(null, { status: 416, headers: { "Content-Range": `bytes */${total}` } });

  const lastIndex = Math.floor(end / CHUNK_BYTES);
  const chunks = [firstChunk];
  for (let index = firstIndex + 1; index <= lastIndex; index += 1) {
    chunks.push(await loadChunk(route, index, originalRequest));
  }

  const body = new Uint8Array(end - range.start + 1);
  for (const chunk of chunks) {
    const copyStart = Math.max(range.start, chunk.start);
    const copyEnd = Math.min(end, chunk.end);
    if (copyEnd < copyStart) continue;
    body.set(chunk.bytes.subarray(copyStart - chunk.start, copyEnd - chunk.start + 1), copyStart - range.start);
  }

  const cacheState = chunks.every((chunk) => chunk.cacheHit) ? "hit" : chunks.some((chunk) => chunk.cacheHit) ? "partial" : "miss";
  const headers = new Headers({
    "Accept-Ranges": "bytes",
    "Cache-Control": "private, max-age=31536000, immutable",
    "Content-Length": String(body.byteLength),
    "Content-Range": `bytes ${range.start}-${end}/${total}`,
    "Content-Type": firstChunk.contentType || "application/octet-stream",
    "X-CineWeave-Media-Cache": cacheState,
  });
  if (firstChunk.etag) headers.set("ETag", firstChunk.etag);
  if (firstChunk.lastModified) headers.set("Last-Modified", firstChunk.lastModified);
  return new Response(body, { status: 206, headers });
}

async function loadChunk(route, index, originalRequest) {
  const chunkPath = `/media-cache-chunk/${CACHE_VERSION}/${route.scopeKey}/${route.sourceKey}/${index}`;
  const cacheKey = new Request(new URL(chunkPath, self.location.origin), { method: "GET" });
  const cache = await caches.open(CHUNK_CACHE);
  const cached = await cache.match(cacheKey);
  if (cached) return readChunkResponse(cached, true);

  const flightKey = cacheKey.url;
  const existing = inFlightChunks.get(flightKey);
  if (existing) return existing;

  const promise = fetchAndStoreChunk(route.source, index, cacheKey, originalRequest).finally(() => inFlightChunks.delete(flightKey));
  inFlightChunks.set(flightKey, promise);
  return promise;
}

async function fetchAndStoreChunk(source, index, cacheKey, originalRequest) {
  const start = index * CHUNK_BYTES;
  const end = start + CHUNK_BYTES - 1;
  const response = await fetchSource(source, `bytes=${start}-${end}`, originalRequest);
  if (response.status !== 206 || response.type === "opaque") throw new Error("range response is not cacheable");

  const contentRange = parseContentRange(response.headers.get("content-range"));
  if (!contentRange || contentRange.total === null) throw new Error("range response has no total length");
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength !== contentRange.end - contentRange.start + 1) throw new Error("range response length does not match Content-Range");

  const headers = new Headers({
    "Content-Length": String(bytes.byteLength),
    "Content-Range": `bytes ${contentRange.start}-${contentRange.end}/${contentRange.total}`,
    "Content-Type": response.headers.get("content-type") || "application/octet-stream",
    "X-CineWeave-Cached-At": String(Date.now()),
  });
  const etag = response.headers.get("etag") || "";
  const lastModified = response.headers.get("last-modified") || "";
  if (etag) headers.set("ETag", etag);
  if (lastModified) headers.set("Last-Modified", lastModified);
  // CacheStorage rejects responses whose status is 206. The cache entry is an
  // internal chunk record, so persist it as 200 and retain the original range
  // in Content-Range; handleRangeRequest reconstructs the public 206 response.
  await putBounded(CHUNK_CACHE, cacheKey, new Response(bytes.slice(), { status: 200, headers }), MAX_CHUNK_ENTRIES);
  return {
    start: contentRange.start,
    end: contentRange.end,
    total: contentRange.total,
    contentType: headers.get("Content-Type") || "",
    etag,
    lastModified,
    bytes,
    cacheHit: false,
  };
}

async function readChunkResponse(response, cacheHit) {
  const contentRange = parseContentRange(response.headers.get("content-range"));
  if (!contentRange || contentRange.total === null) throw new Error("cached range metadata is invalid");
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength !== contentRange.end - contentRange.start + 1) throw new Error("cached range length is invalid");
  return {
    start: contentRange.start,
    end: contentRange.end,
    total: contentRange.total,
    contentType: response.headers.get("content-type") || "",
    etag: response.headers.get("etag") || "",
    lastModified: response.headers.get("last-modified") || "",
    bytes,
    cacheHit,
  };
}

function fetchSource(source, range, originalRequest) {
  const headers = new Headers();
  const accept = originalRequest.headers.get("accept");
  if (accept) headers.set("Accept", accept);
  if (range) headers.set("Range", range);
  return fetch(new Request(source, {
    method: "GET",
    headers,
    mode: "cors",
    credentials: "omit",
    redirect: "follow",
  }));
}

function parseSingleRange(value) {
  const match = value.trim().match(/^bytes=(\d*)-(\d*)$/iu);
  if (!match || (!match[1] && !match[2])) return null;
  if (!match[1]) return { start: 0, end: null, suffixLength: Number(match[2]) };
  const start = Number(match[1]);
  const end = match[2] ? Number(match[2]) : null;
  if (!Number.isSafeInteger(start) || start < 0 || (end !== null && (!Number.isSafeInteger(end) || end < start))) return null;
  return { start, end, suffixLength: null };
}

function parseContentRange(value) {
  const match = value?.match(/^bytes\s+(\d+)-(\d+)\/(\d+|\*)$/iu);
  if (!match) return null;
  const start = Number(match[1]);
  const end = Number(match[2]);
  const total = match[3] === "*" ? null : Number(match[3]);
  if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || end < start || (total !== null && (!Number.isSafeInteger(total) || total <= end))) return null;
  return { start, end, total };
}

async function putBounded(cacheName, key, response, maximumEntries) {
  const cache = await caches.open(cacheName);
  const retryResponse = response.clone();
  try {
    await cache.put(key, response);
  } catch {
    await trimCache(cacheName, Math.max(1, Math.floor(maximumEntries * 0.75)));
    await cache.put(key, retryResponse);
  }
  const insertCount = (cacheInsertCounts.get(cacheName) || 0) + 1;
  cacheInsertCounts.set(cacheName, insertCount);
  if (insertCount % 16 === 0) await trimCache(cacheName, maximumEntries);
}

async function trimCache(cacheName, maximumEntries) {
  const cache = await caches.open(cacheName);
  const keys = await cache.keys();
  const removeCount = Math.max(0, keys.length - maximumEntries);
  await Promise.all(keys.slice(0, removeCount).map((key) => cache.delete(key)));
}

function withCacheHeader(response, state) {
  const headers = new Headers(response.headers);
  headers.set("X-CineWeave-Media-Cache", state);
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
}

async function mediaCacheStatus() {
  const full = await caches.open(FULL_CACHE);
  const chunks = await caches.open(CHUNK_CACHE);
  const [fullKeys, chunkKeys] = await Promise.all([full.keys(), chunks.keys()]);
  return Response.json({ version: CACHE_VERSION, fullEntries: fullKeys.length, chunkEntries: chunkKeys.length }, {
    headers: { "Cache-Control": "no-store", "Content-Type": "application/json; charset=utf-8" },
  });
}

function base64URL(value) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}
