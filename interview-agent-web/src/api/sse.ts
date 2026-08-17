import type { SseMessage } from "../types/api";

export function parseSseBlock(block: string): SseMessage | null {
  let id: string | undefined;
  let event = "message";
  const data: string[] = [];

  for (const rawLine of block.split(/\r?\n/)) {
    if (!rawLine || rawLine.startsWith(":")) continue;
    const separator = rawLine.indexOf(":");
    const field = separator === -1 ? rawLine : rawLine.slice(0, separator);
    let value = separator === -1 ? "" : rawLine.slice(separator + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    if (field === "id") id = value;
    if (field === "event") event = value || "message";
    if (field === "data") data.push(value);
  }

  if (data.length === 0) return null;
  const rawData = data.join("\n");
  let parsedData: unknown = rawData;
  try {
    parsedData = JSON.parse(rawData);
  } catch {
    // Plain-text data is valid SSE. Keep it intact for forward compatibility.
  }

  return { id, event, data: parsedData };
}

export async function consumeSseStream(
  stream: ReadableStream<Uint8Array>,
  onMessage: (message: SseMessage) => void,
  signal?: AbortSignal,
): Promise<void> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      if (signal?.aborted) return;
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");
      let boundary = buffer.indexOf("\n\n");
      while (boundary !== -1) {
        const block = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const message = parseSseBlock(block);
        if (message) onMessage(message);
        boundary = buffer.indexOf("\n\n");
      }
    }

    buffer += decoder.decode();
    const finalMessage = parseSseBlock(buffer);
    if (finalMessage) onMessage(finalMessage);
  } finally {
    reader.releaseLock();
  }
}
