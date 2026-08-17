import { describe, expect, it } from "vitest";
import { consumeSseStream, parseSseBlock } from "./sse";

describe("SSE parser", () => {
  it("parses event metadata and JSON data", () => {
    expect(
      parseSseBlock('id: 12\nevent: question\ndata: {"prompt_id":"p1","content":"hello"}'),
    ).toEqual({
      id: "12",
      event: "question",
      data: { prompt_id: "p1", content: "hello" },
    });
  });

  it("handles chunks, comments, and multiline data", async () => {
    const encoder = new TextEncoder();
    const chunks = [
      ": ping\n\nid: 4\nevent: warning\nda",
      "ta: first\ndata: second\n\n",
    ];
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        chunks.forEach((chunk) => controller.enqueue(encoder.encode(chunk)));
        controller.close();
      },
    });
    const messages: unknown[] = [];

    await consumeSseStream(stream, (message) => messages.push(message));

    expect(messages).toEqual([{ id: "4", event: "warning", data: "first\nsecond" }]);
  });
});
