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

export function answerConflictMessage(code: string): string {
  const messages: Record<string, string> = {
    answer_already_submitted: "这份回答已被服务端接收，页面已同步到最新进度，请勿重复提交。",
    prompt_mismatch: "当前题目已经变化，页面已同步到最新问题，请重新作答。",
    interview_not_awaiting_answer: "面试官正在处理上一轮回答，页面已同步，请等待下一道题。",
    interview_finished: "本场面试已经结束，页面已同步到最终状态。",
  };
  return messages[code] ?? "当前会话状态已变化，页面已同步，请按最新状态继续。";
}
