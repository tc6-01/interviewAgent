import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpInterviewApi } from "./client";
import type { InterviewSnapshot, SseMessage } from "../types/api";

const localStorageStub = {
  getItem: vi.fn(() => null),
};

function snapshot(
  lastEventId: string,
  status: InterviewSnapshot["status"] = "interviewing",
): InterviewSnapshot {
  return {
    interview_id: "demo",
    status,
    stage: "interview",
    awaiting_answer: null,
    current_question: null,
    progress: { answered: 1, total: 15 },
    qa_history: [],
    ended_reason: null,
    report_status: status === "completed" ? "ready" : "not_started",
    review_plan_status: status === "completed" ? "ready" : "not_started",
    report_ready: status === "completed",
    review_plan_ready: status === "completed",
    last_event_id: lastEventId,
    created_at: "2026-08-15T00:00:00.000Z",
    updated_at: "2026-08-15T00:00:00.000Z",
  };
}

function sseResponse(body: string): Response {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode(body));
        controller.close();
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("HttpInterviewApi", () => {
  it("treats report and review-plan retries as 202 accepted commands", async () => {
    vi.stubGlobal("window", { localStorage: localStorageStub });
    const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
      const artifact = String(input).includes("review-plan") ? "review_plan" : "report";
      return new Response(JSON.stringify({ accepted: true, interview_id: "interview/1", artifact }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(new HttpInterviewApi().retryReport("interview/1")).resolves.toMatchObject({
      accepted: true,
      artifact: "report",
    });
    await expect(new HttpInterviewApi().retryReviewPlan("interview/1")).resolves.toMatchObject({
      accepted: true,
      artifact: "review_plan",
    });

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/interviews/interview%2F1/report/retry",
      "/api/v1/interviews/interview%2F1/review-plan/retry",
    ]);
    expect(fetchMock.mock.calls.every(([, init]) => init?.method === "POST")).toBe(true);
  });

  it("gets the latest snapshot before every SSE connection", async () => {
    vi.stubGlobal("window", {
      localStorage: localStorageStub,
      setTimeout: (callback: () => void) => globalThis.setTimeout(callback, 0),
    });
    const responses = [
      new Response(JSON.stringify(snapshot("4")), { status: 200 }),
      sseResponse("id: 7\nevent: stage\ndata: {\"stage\":\"interview\"}\n\n"),
      new Response(JSON.stringify(snapshot("9")), { status: 200 }),
      sseResponse("id: 10\nevent: completed\ndata: {}\n\n"),
    ];
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => responses.shift() ?? new Response(null, { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);
    const messages: SseMessage[] = [];
    const snapshots: InterviewSnapshot[] = [];

    await new HttpInterviewApi().subscribe("demo", {
      onSnapshot: (value) => snapshots.push(value),
      onMessage: (message) => messages.push(message),
    });

    expect(messages.map((message) => message.id)).toEqual(["7", "10"]);
    expect(snapshots.map((value) => value.last_event_id)).toEqual(["4", "9"]);
    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/interviews/demo",
      "/api/v1/interviews/demo/events",
      "/api/v1/interviews/demo",
      "/api/v1/interviews/demo/events",
    ]);
    const firstStreamInit = fetchMock.mock.calls[1]?.[1];
    const secondStreamInit = fetchMock.mock.calls[3]?.[1];
    expect(new Headers(firstStreamInit?.headers).get("Last-Event-ID")).toBe("4");
    expect(new Headers(secondStreamInit?.headers).get("Last-Event-ID")).toBe("9");
  });

  it("stops reconnecting when the recovery snapshot is already terminal", async () => {
    vi.stubGlobal("window", {
      localStorage: localStorageStub,
      setTimeout: (callback: () => void) => globalThis.setTimeout(callback, 0),
    });
    const responses = [
      new Response(JSON.stringify(snapshot("4")), { status: 200 }),
      sseResponse("id: 5\nevent: stage\ndata: {\"stage\":\"evaluation\"}\n\n"),
      new Response(JSON.stringify(snapshot("8", "completed")), { status: 200 }),
    ];
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => responses.shift() ?? new Response(null, { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);
    const snapshots: InterviewSnapshot[] = [];

    await new HttpInterviewApi().subscribe("demo", {
      onSnapshot: (value) => snapshots.push(value),
      onMessage: () => undefined,
    });

    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(snapshots.at(-1)?.status).toBe("completed");
  });
});
