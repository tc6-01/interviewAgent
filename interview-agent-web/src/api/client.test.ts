import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpInterviewApi } from "./client";
import type { SseMessage } from "../types/api";

const localStorageStub = {
  getItem: vi.fn(() => null),
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("HttpInterviewApi", () => {
  it("posts to the approved review-plan retry endpoint", async () => {
    vi.stubGlobal("window", { localStorage: localStorageStub });
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      new Response(JSON.stringify({ plan_markdown: "# plan", plan: { study_plan: [] } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await new HttpInterviewApi().retryReviewPlan("interview/1");

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    if (!init) throw new Error("missing request init");
    expect(url).toBe("/api/v1/interviews/interview%2F1/review-plan/retry");
    expect(init.method).toBe("POST");
  });

  it("reconnects SSE with the latest Last-Event-ID", async () => {
    vi.stubGlobal("window", {
      localStorage: localStorageStub,
      setTimeout: (callback: () => void) => globalThis.setTimeout(callback, 0),
    });
    const encoder = new TextEncoder();
    const responses = [
      "id: 7\nevent: stage\ndata: {\"stage\":\"interview\"}\n\n",
      "id: 8\nevent: completed\ndata: {}\n\n",
    ];
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode(responses.shift() ?? ""));
            controller.close();
          },
        }),
        { status: 200, headers: { "Content-Type": "text/event-stream" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const messages: SseMessage[] = [];

    await new HttpInterviewApi().subscribe("demo", {
      onMessage: (message) => messages.push(message),
    });

    expect(messages.map((message) => message.id)).toEqual(["7", "8"]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const secondInit = fetchMock.mock.calls[1]?.[1];
    if (!secondInit) throw new Error("missing reconnect request init");
    expect(new Headers(secondInit.headers).get("Last-Event-ID")).toBe("7");
  });
});
