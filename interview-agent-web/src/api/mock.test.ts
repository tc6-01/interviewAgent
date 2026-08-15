import { describe, expect, it } from "vitest";
import { MockInterviewApi } from "./mock";
import type { SseMessage } from "../types/api";

describe("MockInterviewApi", () => {
  it("runs from preparation through multiple interview questions", async () => {
    const api = new MockInterviewApi();
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

    await new Promise((resolve) => setTimeout(resolve, 2800));
    const first = await api.getInterview(created.interview_id);
    expect(first.awaiting_answer?.prompt_id).toBe("prompt_1_main");

    await api.submitAnswer(created.interview_id, "prompt_1_main", "完整的项目回答，包含背景、行动和量化结果。".repeat(3));
    await new Promise((resolve) => setTimeout(resolve, 750));
    const second = await api.getInterview(created.interview_id);
    expect(second.awaiting_answer?.prompt_id).toBe("prompt_2_main");
    expect(events.some((event) => event.event === "score")).toBe(true);
    expect(events.some((event) => event.event === "question" && (event.data as { prompt_id?: string }).prompt_id === "prompt_2_main")).toBe(true);

    controller.abort();
    await subscription;
  }, 10_000);
});
