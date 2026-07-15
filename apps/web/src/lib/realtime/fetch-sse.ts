export type ServerSentEvent = {
  event: string;
  data: string;
  id?: string;
  retry?: number;
};

const maxBufferedEventBytes = 4 * 1024 * 1024;

export async function consumeServerSentEvents(
  response: Response,
  onEvent: (event: ServerSentEvent) => void | Promise<void>,
): Promise<void> {
  if (!response.body) {
    throw new Error("SSE response body is unavailable");
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    for (;;) {
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      if (buffer.length > maxBufferedEventBytes) {
        throw new Error("SSE event exceeds the maximum buffered size");
      }
      buffer = buffer.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
      let boundary = buffer.indexOf("\n\n");
      while (boundary >= 0) {
        const frame = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const event = parseServerSentEvent(frame);
        if (event) {
          await onEvent(event);
        }
        boundary = buffer.indexOf("\n\n");
      }
      if (done) {
        if (buffer.trim()) {
          const event = parseServerSentEvent(buffer);
          if (event) {
            await onEvent(event);
          }
        }
        return;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function parseServerSentEvent(frame: string): ServerSentEvent | null {
  let event = "message";
  let id: string | undefined;
  let retry: number | undefined;
  const data: string[] = [];
  for (const line of frame.split("\n")) {
    if (!line || line.startsWith(":")) {
      continue;
    }
    const separator = line.indexOf(":");
    const field = separator >= 0 ? line.slice(0, separator) : line;
    let value = separator >= 0 ? line.slice(separator + 1) : "";
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }
    switch (field) {
      case "event":
        event = value || "message";
        break;
      case "data":
        data.push(value);
        break;
      case "id":
        if (!value.includes("\0")) {
          id = value;
        }
        break;
      case "retry": {
        const parsed = Number.parseInt(value, 10);
        if (Number.isSafeInteger(parsed) && parsed >= 0) {
          retry = parsed;
        }
        break;
      }
    }
  }
  if (data.length === 0 && !id && event === "message" && retry === undefined) {
    return null;
  }
  return { event, data: data.join("\n"), id, retry };
}
