import { describe, expect, it } from "vitest";
import { answerConflictMessage } from "../api/errors";

describe("answer conflict feedback", () => {
  it.each([
    ["answer_already_submitted", "已被服务端接收"],
    ["prompt_mismatch", "题目已经变化"],
    ["interview_not_awaiting_answer", "正在处理上一轮回答"],
    ["interview_finished", "面试已经结束"],
  ])("maps %s without claiming every conflict was accepted", (code, expected) => {
    expect(answerConflictMessage(code)).toContain(expected);
  });
});
