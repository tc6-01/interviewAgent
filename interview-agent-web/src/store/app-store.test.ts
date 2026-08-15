import { describe, expect, it } from "vitest";
import { applyEvent, hydrateSession } from "./app-store";
import type { InterviewSnapshot } from "../types/api";

describe("snapshot recovery", () => {
  it("rebuilds answered history and the current question without duplicates", () => {
    const snapshot: InterviewSnapshot = {
      interview_id: "interview_1",
      status: "interviewing",
      stage: "interview",
      awaiting_answer: { prompt_id: "prompt_2_main", question_no: 2, kind: "primary" },
      current_question: {
        prompt_id: "prompt_2_main",
        question_no: 2,
        kind: "primary",
        content: "第二题",
      },
      progress: { answered: 1, total: 15 },
      qa_history: [
        {
          prompt_id: "prompt_1_main",
          question_no: 1,
          kind: "primary",
          question: "第一题",
          answer: "第一题回答",
          score: 82,
          feedback: "结构清楚",
        },
      ],
      ended_reason: null,
      report_ready: false,
      review_plan_ready: false,
      last_event_id: "18",
      created_at: "2026-08-15T00:00:00.000Z",
      updated_at: "2026-08-15T00:05:00.000Z",
    };

    const session = hydrateSession(snapshot);

    expect(session.messages.map((message) => message.id)).toEqual([
      "q-prompt_1_main",
      "a-prompt_1_main",
      "score-prompt_1_main",
      "q-prompt_2_main",
    ]);
    expect(session.awaitingAnswer?.prompt_id).toBe("prompt_2_main");
    expect(session.answered).toBe(1);
    expect(session.total).toBe(15);
    expect(session.lastEventId).toBe("18");

    const failed = applyEvent(session, {
      id: "19",
      event: "failed",
      data: { code: "upstream_failed", message: "模型服务暂时不可用" },
    });
    expect(failed.status).toBe("failed");
    expect(failed.awaitingAnswer).toBeNull();
    expect(failed.error).toBe("模型服务暂时不可用");
    expect(failed.lastEventId).toBe("19");
  });
});
