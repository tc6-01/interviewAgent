import type {
  CreateInterviewInput,
  CreateInterviewResponse,
  InterviewSnapshot,
  ParsedDocument,
  ReportResponse,
  ReviewPlanResponse,
  SseMessage,
} from "../types/api";
import { consumeSseStream } from "./sse";
import { MockInterviewApi } from "./mock";

const configuredBase = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");
const API_ROOT = `${configuredBase}/api/v1`;
const TERMINAL_EVENTS = new Set(["completed", "terminated", "failed"]);

export class InterviewApiError extends Error {
  code: string;
  status: number;
  details?: Record<string, unknown>;

  constructor(message: string, code: string, status: number, details?: Record<string, unknown>) {
    super(message);
    this.name = "InterviewApiError";
    this.code = code;
    this.status = status;
    this.details = details;
  }
}

export interface ParseDocumentInput {
  kind: "jd" | "resume";
  text?: string;
  url?: string;
  file?: File;
}

export interface SubscribeOptions {
  lastEventId?: string;
  signal?: AbortSignal;
  onMessage: (message: SseMessage) => void;
  onConnectionChange?: (connected: boolean) => void;
}

export interface InterviewApi {
  readonly mode: "mock" | "real";
  parseDocument(input: ParseDocumentInput): Promise<ParsedDocument>;
  createInterview(input: CreateInterviewInput): Promise<CreateInterviewResponse>;
  getInterview(id: string): Promise<InterviewSnapshot>;
  submitAnswer(id: string, promptId: string, text: string): Promise<void>;
  quitInterview(id: string): Promise<void>;
  getReport(id: string): Promise<ReportResponse>;
  getReviewPlan(id: string): Promise<ReviewPlanResponse>;
  subscribe(id: string, options: SubscribeOptions): Promise<void>;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = window.localStorage.getItem("interview-agent-token");
  const headers = new Headers(init.headers);
  if (!(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const response = await fetch(`${API_ROOT}${path}`, {
    ...init,
    headers,
    credentials: "include",
  });
  if (response.ok) {
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  let message = `请求失败（${response.status}）`;
  let code = "request_failed";
  let details: Record<string, unknown> | undefined;
  try {
    const payload = (await response.json()) as {
      error?: { message?: string; code?: string; details?: Record<string, unknown> };
    };
    message = payload.error?.message ?? message;
    code = payload.error?.code ?? code;
    details = payload.error?.details;
  } catch {
    // Keep the status-based message when the response is not JSON.
  }
  throw new InterviewApiError(message, code, response.status, details);
}

class HttpInterviewApi implements InterviewApi {
  readonly mode = "real" as const;

  async parseDocument(input: ParseDocumentInput): Promise<ParsedDocument> {
    if (input.file) {
      const body = new FormData();
      body.append("kind", input.kind);
      body.append("file", input.file);
      return request<ParsedDocument>("/documents/parse", { method: "POST", body });
    }
    return request<ParsedDocument>("/documents/parse", {
      method: "POST",
      body: JSON.stringify({ kind: input.kind, text: input.text, url: input.url }),
    });
  }

  createInterview(input: CreateInterviewInput): Promise<CreateInterviewResponse> {
    return request<CreateInterviewResponse>("/interviews", {
      method: "POST",
      body: JSON.stringify(input),
      headers: { "Idempotency-Key": crypto.randomUUID() },
    });
  }

  getInterview(id: string): Promise<InterviewSnapshot> {
    return request<InterviewSnapshot>(`/interviews/${encodeURIComponent(id)}`);
  }

  async submitAnswer(id: string, promptId: string, text: string): Promise<void> {
    await request(`/interviews/${encodeURIComponent(id)}/answers`, {
      method: "POST",
      body: JSON.stringify({ prompt_id: promptId, text }),
    });
  }

  async quitInterview(id: string): Promise<void> {
    await request(`/interviews/${encodeURIComponent(id)}/quit`, {
      method: "POST",
      body: JSON.stringify({ reason: "user_requested" }),
    });
  }

  getReport(id: string): Promise<ReportResponse> {
    return request<ReportResponse>(`/interviews/${encodeURIComponent(id)}/report`);
  }

  getReviewPlan(id: string): Promise<ReviewPlanResponse> {
    return request<ReviewPlanResponse>(`/interviews/${encodeURIComponent(id)}/review-plan`);
  }

  async subscribe(id: string, options: SubscribeOptions): Promise<void> {
    let lastEventId = options.lastEventId;
    let retryCount = 0;
    const token = window.localStorage.getItem("interview-agent-token");

    while (!options.signal?.aborted) {
      try {
        const headers = new Headers({ Accept: "text/event-stream" });
        if (token) headers.set("Authorization", `Bearer ${token}`);
        if (lastEventId) headers.set("Last-Event-ID", lastEventId);
        const response = await fetch(`${API_ROOT}/interviews/${encodeURIComponent(id)}/events`, {
          headers,
          credentials: "include",
          signal: options.signal,
        });
        if (!response.ok || !response.body) {
          throw new InterviewApiError("实时连接建立失败", "sse_connection_failed", response.status);
        }

        options.onConnectionChange?.(true);
        retryCount = 0;
        let terminal = false;
        await consumeSseStream(
          response.body,
          (message) => {
            if (message.id) lastEventId = message.id;
            if (TERMINAL_EVENTS.has(message.event)) terminal = true;
            options.onMessage(message);
          },
          options.signal,
        );
        options.onConnectionChange?.(false);
        if (terminal || options.signal?.aborted) return;
      } catch (error) {
        options.onConnectionChange?.(false);
        if (options.signal?.aborted || (error instanceof DOMException && error.name === "AbortError")) {
          return;
        }
        retryCount += 1;
        if (retryCount > 5) throw error;
        await new Promise((resolve) => window.setTimeout(resolve, Math.min(1000 * 2 ** retryCount, 8000)));
      }
    }
  }
}

export const api: InterviewApi =
  import.meta.env.VITE_API_MODE === "real" ? new HttpInterviewApi() : new MockInterviewApi();
