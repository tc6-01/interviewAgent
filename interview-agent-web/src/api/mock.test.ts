import { describe, expect, it } from "vitest";
import { MockInterviewApi } from "./mock";
import { InterviewApiError } from "./errors";
import type { InterviewSnapshot, SseMessage } from "../types/api";

async function waitForSnapshot(
  api: MockInterviewApi,
  id: string,
  predicate: (snapshot: InterviewSnapshot) => boolean,
): Promise<InterviewSnapshot> {
  const deadline = Date.now() + 2_000;
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

const strongAnswer = "我会先说明背景、约束与判断依据，再描述具体行动、量化结果、失败边界以及回滚方案。".repeat(4);

describe("MockInterviewApi", () => {
  it("runs 15 primary questions with 8/5/2 distribution and one follow-up", async () => {
    const api = new MockInterviewApi({ delayScale: 0 });
    const session = await createStartedSession(api);

    expect(session.snapshot.progress.total).toBe(15);
    await api.submitAnswer(session.created.interview_id, "prompt_1_main", "不会");
    const followUp = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.awaiting_answer?.prompt_id === "prompt_1_followup",
    );
    expect(followUp.progress.answered).toBe(1);
    await api.submitAnswer(session.created.interview_id, "prompt_1_followup", strongAnswer);

    for (let questionNo = 2; questionNo <= 15; questionNo += 1) {
      const promptId = `prompt_${questionNo}_main`;
      await waitForSnapshot(
        api,
        session.created.interview_id,
        (snapshot) => snapshot.awaiting_answer?.prompt_id === promptId,
      );
      await api.submitAnswer(session.created.interview_id, promptId, strongAnswer);
    }

    const completed = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.status === "completed",
    );
    const plan = session.events.find((event) => event.event === "question_plan");
    const questions = session.events.filter((event) => event.event === "question");
    const deltas = session.events
      .filter((event) => event.event === "question_delta" && (event.data as { prompt_id?: string }).prompt_id === "prompt_1_main")
      .map((event) => String((event.data as { delta?: string }).delta ?? ""))
      .join("");
    const firstQuestion = questions.find(
      (event) => (event.data as { prompt_id?: string }).prompt_id === "prompt_1_main",
    );

    expect(plan?.data).toEqual({ total_questions: 15, distribution: { basic: 8, experience: 5, design: 2 } });
    expect(questions.filter((event) => (event.data as { kind?: string }).kind === "primary")).toHaveLength(15);
    expect(questions.filter((event) => (event.data as { kind?: string }).kind === "followup")).toHaveLength(1);
    expect(deltas).toBe((firstQuestion?.data as { content?: string }).content);
    expect(completed.progress.answered).toBe(15);
    expect(completed.report_status).toBe("ready");
    expect(completed.review_plan_status).toBe("ready");
    await session.subscription;
  });

  it("returns distinct 409 codes for duplicate, mismatched and busy answers", async () => {
    const api = new MockInterviewApi({ delayScale: 0 });
    const session = await createStartedSession(api);

    await expect(
      api.submitAnswer(session.created.interview_id, "wrong_prompt", "回答"),
    ).rejects.toMatchObject({ code: "prompt_mismatch", status: 409 } satisfies Partial<InterviewApiError>);

    const accepted = api.submitAnswer(session.created.interview_id, "prompt_1_main", strongAnswer);
    await expect(
      api.submitAnswer(session.created.interview_id, "prompt_2_main", "抢答"),
    ).rejects.toMatchObject({ code: "interview_not_awaiting_answer", status: 409 } satisfies Partial<InterviewApiError>);
    await accepted;
    await expect(
      api.submitAnswer(session.created.interview_id, "prompt_1_main", "重复提交"),
    ).rejects.toMatchObject({ code: "answer_already_submitted", status: 409 } satisfies Partial<InterviewApiError>);

    session.controller.abort();
    await session.subscription;
  });

  it("terminates without artifacts when quitting before the first answer", async () => {
    const api = new MockInterviewApi({ delayScale: 0 });
    const session = await createStartedSession(api);

    await api.quitInterview(session.created.interview_id);
    const terminated = await api.getInterview(session.created.interview_id);

    expect(terminated.status).toBe("terminated");
    expect(terminated.report_status).toBe("not_started");
    expect(terminated.report_ready).toBe(false);
    await expect(
      api.submitAnswer(session.created.interview_id, "prompt_1_main", "迟到的回答"),
    ).rejects.toMatchObject({ code: "interview_finished", status: 409 } satisfies Partial<InterviewApiError>);
    await session.subscription;
  });

  it("returns 202 for artifact retries and exposes completion through snapshots and GET", async () => {
    const api = new MockInterviewApi({ delayScale: 0, failReviewPlanOnce: true });
    const session = await createStartedSession(api);
    await api.submitAnswer(session.created.interview_id, "prompt_1_main", strongAnswer);
    await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.awaiting_answer?.prompt_id === "prompt_2_main",
    );

    await api.quitInterview(session.created.interview_id);
    const failedPlan = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.review_plan_status === "failed",
    );
    expect(failedPlan.report_status).toBe("ready");
    await expect(api.getReviewPlan(session.created.interview_id)).rejects.toMatchObject({
      code: "review_plan_not_ready",
    });

    await expect(api.retryReviewPlan(session.created.interview_id)).resolves.toEqual({
      accepted: true,
      interview_id: session.created.interview_id,
      artifact: "review_plan",
    });
    await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.review_plan_status === "ready",
    );
    expect((await api.getReviewPlan(session.created.interview_id)).plan.study_plan[0].actions.length).toBeGreaterThanOrEqual(2);
    await session.subscription;
  });

  it("recovers a failed report without losing answered history", async () => {
    const api = new MockInterviewApi({ delayScale: 0, failReportOnce: true });
    const session = await createStartedSession(api);
    await api.submitAnswer(session.created.interview_id, "prompt_1_main", strongAnswer);
    await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.awaiting_answer?.prompt_id === "prompt_2_main",
    );
    await api.quitInterview(session.created.interview_id);
    const failedReport = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.report_status === "failed",
    );

    expect(failedReport.qa_history).toHaveLength(1);
    await expect(api.retryReport(session.created.interview_id)).resolves.toMatchObject({
      accepted: true,
      artifact: "report",
    });
    const recovered = await waitForSnapshot(
      api,
      session.created.interview_id,
      (snapshot) => snapshot.report_status === "ready" && snapshot.review_plan_status === "ready",
    );
    expect(recovered.qa_history).toHaveLength(1);
    expect((await api.getReport(session.created.interview_id)).report.detailed_review[0].user_answer).toBe(strongAnswer);
    await session.subscription;
  });
});
