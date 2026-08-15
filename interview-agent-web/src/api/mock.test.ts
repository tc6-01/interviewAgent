import { describe, expect, it } from "vitest";
import { MockInterviewApi } from "./mock";
import { InterviewApiError } from "./errors";
import type { InterviewSnapshot, SseMessage } from "../types/api";

async function waitForSnapshot(
  api: MockInterviewApi,
  id: string,
  predicate: (snapshot: InterviewSnapshot) => boolean,
): Promise<InterviewSnapshot> {
  const deadline = Date.now() + 1_000;
  while (Date.now() < deadline) {
    const snapshot = await api.getInterview(id);
    if (predicate(snapshot)) return snapshot;
    await new Promise((resolve) => setTimeout(resolve, 1));
  }
  throw new Error("mock session did not reach the expected state");
}

async function createStartedSession(api: MockInterviewApi) {
  const created = await api.createInterview({
    jd_text: "高级 Go 后端工程师",
    resume_text: "6 年 Go 开发经验",
    options: { question_count: 3 },
  });
  const events: SseMessage[] = [];
  const controller = new AbortController();
  const subscription = api.subscribe(created.interview_id, {
    signal: controller.signal,
    onMessage: (message) => events.push(message),
  });
  const snapshot = await waitForSnapshot(
    api,
    created.interview_id,
    (value) => value.awaiting_answer?.prompt_id === "prompt_1_main",
  );
  return { created, events, controller, subscription, snapshot };
}

describe("MockInterviewApi", () => {
  it("locks the Alpha flow to 15 questions and rejects duplicate answers", async () => {
    const api = new MockInterviewApi({ delayScale: 0 });
    const session = await createStartedSession(api);

    expect(session.snapshot.progress.total).toBe(15);
    await api.submitAnswer(session.created.interview_id, "prompt_1_main", "完整的项目回答，包含背景、行动和量化结果。".repeat(3));
    await expect(
      api.submitAnswer(session.created.interview_id, "prompt_1_main", "重复提交"),
    ).rejects.toMatchObject({
      code: "answer_already_submitted",
      status: 409,
    } satisfies Partial<InterviewApiError>);

    const second = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.awaiting_answer?.prompt_id === "prompt_2_main",
    );
    expect(second.progress.answered).toBe(1);
    session.controller.abort();
    await session.subscription;
  });

  it("replays authoritative events after reconnecting with Last-Event-ID", async () => {
    const api = new MockInterviewApi({ delayScale: 0 });
    const session = await createStartedSession(api);
    session.controller.abort();
    await session.subscription;

    const resumeAfter = session.events.find((event) => event.id)?.id ?? "0";
    const replayed: SseMessage[] = [];
    const reconnectController = new AbortController();
    const reconnect = api.subscribe(session.created.interview_id, {
      lastEventId: resumeAfter,
      signal: reconnectController.signal,
      onMessage: (message) => replayed.push(message),
    });
    reconnectController.abort();
    await reconnect;

    expect(replayed.length).toBeGreaterThan(0);
    expect(replayed.every((event) => !event.id || Number(event.id) > Number(resumeAfter))).toBe(true);
    expect(replayed.some((event) => event.event === "question")).toBe(true);
  });

  it("supports quit and retries only a failed review-plan stage", async () => {
    const api = new MockInterviewApi({ delayScale: 0, failReviewPlanOnce: true });
    const session = await createStartedSession(api);
    await api.submitAnswer(session.created.interview_id, "prompt_1_main", "我会先说明背景，再描述行动、取舍和量化结果。".repeat(3));
    await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.awaiting_answer?.prompt_id === "prompt_2_main",
    );

    await api.quitInterview(session.created.interview_id);
    const completed = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.status === "completed",
    );
    expect(completed.report_ready).toBe(true);
    expect(completed.review_plan_ready).toBe(false);
    await expect(api.getReviewPlan(session.created.interview_id)).rejects.toMatchObject({
      code: "review_plan_not_ready",
    });

    const plan = await api.retryReviewPlan(session.created.interview_id);
    expect(plan.plan.study_plan.length).toBeGreaterThan(0);
    expect((await api.getInterview(session.created.interview_id)).review_plan_ready).toBe(true);
    await session.subscription;
  });
});
