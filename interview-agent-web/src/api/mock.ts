import type {
  CreateInterviewInput,
  CreateInterviewResponse,
  InterviewEventMap,
  InterviewSnapshot,
  ParsedDocument,
  Question,
  ReportResponse,
  ReviewPlanResponse,
  SseMessage,
} from "../types/api";
import type { InterviewApi, ParseDocumentInput, SubscribeOptions } from "./client";

interface MockSession {
  snapshot: InterviewSnapshot;
  input: CreateInterviewInput;
  started: boolean;
  sequence: number;
  currentIndex: number;
  questions: Question[];
  events: SseMessage[];
  subscribers: Set<(message: SseMessage) => void>;
  report?: ReportResponse;
  reviewPlan?: ReviewPlanResponse;
}

const wait = (milliseconds: number) =>
  new Promise<void>((resolve) => globalThis.setTimeout(resolve, milliseconds));

const sampleQuestions: Question[] = [
  {
    prompt_id: "prompt_1_main",
    question_no: 1,
    kind: "primary",
    content: "请先选一个你最有代表性的项目，说明你负责的部分、遇到的关键挑战，以及最终产生的结果。",
    source: "demo:project-impact",
  },
  {
    prompt_id: "prompt_2_main",
    question_no: 2,
    kind: "primary",
    content: "如果线上接口的 P99 延迟突然升高，你会怎样定位？请按排查顺序说明你关注的指标和判断依据。",
    source: "demo:system-debugging",
  },
  {
    prompt_id: "prompt_3_main",
    question_no: 3,
    kind: "primary",
    content: "现在需要把一个单体服务拆成可扩展的架构，你会如何划分边界，并处理数据一致性与故障恢复？",
    source: "demo:system-design",
  },
];

export class MockInterviewApi implements InterviewApi {
  readonly mode = "mock" as const;
  private sessions = new Map<string, MockSession>();

  async parseDocument(input: ParseDocumentInput): Promise<ParsedDocument> {
    await wait(450);
    let text = input.text?.trim() ?? "";
    if (input.file) {
      const canReadAsText = /\.(txt|md)$/i.test(input.file.name);
      text = canReadAsText
        ? (await input.file.text()).trim()
        : input.kind === "jd"
          ? `高级研发工程师\n负责核心业务系统的架构设计与研发，要求具备 5 年以上经验，熟悉高并发、数据库、缓存与微服务治理。\n\n（演示模式已接收 ${input.file.name}；真实模式由服务端解析原文件。）`
          : `拥有 6 年软件研发经验，主导过核心交易系统重构，负责服务治理、性能优化与稳定性建设。熟悉 Go、MySQL、Redis 与微服务。\n\n（演示模式已接收 ${input.file.name}；真实模式由服务端解析原文件。）`;
    }
    if (!text && input.url) text = `来自 ${input.url} 的演示岗位描述`;
    if (!text) throw new Error("请输入文本、URL 或选择文件");

    return {
      kind: input.kind,
      text,
      chars: [...text].length,
      source: input.file
        ? { type: "file", name: input.file.name }
        : input.url
          ? { type: "url", url: input.url }
          : { type: "text" },
      warnings: [],
    };
  }

  async createInterview(input: CreateInterviewInput): Promise<CreateInterviewResponse> {
    await wait(350);
    const id = `demo_${crypto.randomUUID().slice(0, 8)}`;
    const createdAt = new Date().toISOString();
    const total = Math.min(input.options?.question_count ?? 3, sampleQuestions.length);
    const session: MockSession = {
      input,
      started: false,
      sequence: 0,
      currentIndex: 0,
      questions: sampleQuestions.slice(0, total),
      events: [],
      subscribers: new Set(),
      snapshot: {
        interview_id: id,
        status: "preparing",
        stage: "jd_analysis",
        awaiting_answer: null,
        current_question: null,
        progress: { answered: 0, total },
        qa_history: [],
        ended_reason: null,
        report_ready: false,
        review_plan_ready: false,
        last_event_id: "0",
        created_at: createdAt,
        updated_at: createdAt,
      },
    };
    this.sessions.set(id, session);
    return {
      interview_id: id,
      status: "preparing",
      events_url: `/api/v1/interviews/${id}/events`,
      created_at: createdAt,
    };
  }

  async getInterview(id: string): Promise<InterviewSnapshot> {
    await wait(100);
    return structuredClone(this.requireSession(id).snapshot);
  }

  async submitAnswer(id: string, promptId: string, text: string): Promise<void> {
    const session = this.requireSession(id);
    const awaiting = session.snapshot.awaiting_answer;
    if (!awaiting || awaiting.prompt_id !== promptId) throw new Error("当前问题已变化，请刷新后重试");
    if (!text.trim()) throw new Error("回答不能为空");

    session.snapshot.awaiting_answer = null;
    session.snapshot.qa_history.push({
      prompt_id: awaiting.prompt_id,
      question_no: awaiting.question_no,
      kind: awaiting.kind,
      question: session.snapshot.current_question?.content ?? "",
      answer: text.trim(),
    });
    session.snapshot.progress.answered += 1;
    session.snapshot.updated_at = new Date().toISOString();
    void this.evaluateAnswerAndContinue(session, promptId, text.trim());
    await wait(100);
  }

  private async evaluateAnswerAndContinue(
    session: MockSession,
    promptId: string,
    answer: string,
  ): Promise<void> {
    await wait(500);

    const score = Math.min(92, 68 + answer.length / 8);
    const scoreEvent: InterviewEventMap["score"] = {
      prompt_id: promptId,
      score: Math.round(score),
      feedback:
        answer.length > 80
          ? "回答结构清楚，能够结合实际场景说明判断依据。进一步补充量化指标会更有说服力。"
          : "核心思路正确。建议使用“背景—行动—结果”的结构，并补充关键指标与取舍依据。",
      key_points_hit: ["思路完整", "场景意识"],
      key_points_missed: ["量化结果", "边界条件"],
    };
    const historyItem = session.snapshot.qa_history.at(-1);
    if (historyItem) {
      historyItem.score = scoreEvent.score;
      historyItem.feedback = scoreEvent.feedback;
    }
    this.emit(session, "score", scoreEvent);
    session.currentIndex += 1;

    if (session.currentIndex < session.questions.length) {
      await this.streamQuestion(session, session.questions[session.currentIndex]);
      return;
    }
    await this.finishInterview(session);
  }

  async quitInterview(id: string): Promise<void> {
    const session = this.requireSession(id);
    session.snapshot.awaiting_answer = null;
    if (session.snapshot.progress.answered === 0) {
      session.snapshot.status = "terminated";
      session.snapshot.ended_reason = "user_requested";
      this.emit(session, "terminated", { interview_id: id, reason: "user_requested" });
      return;
    }
    void this.finishInterview(session);
    await wait(100);
  }

  async getReport(id: string): Promise<ReportResponse> {
    const session = this.requireSession(id);
    if (!session.report) throw new Error("评估报告仍在生成中");
    return structuredClone(session.report);
  }

  async getReviewPlan(id: string): Promise<ReviewPlanResponse> {
    const session = this.requireSession(id);
    if (!session.reviewPlan) throw new Error("复习计划仍在生成中");
    return structuredClone(session.reviewPlan);
  }

  async subscribe(id: string, options: SubscribeOptions): Promise<void> {
    const session = this.requireSession(id);
    const last = Number(options.lastEventId ?? 0);
    session.events
      .filter((event) => Number(event.id ?? 0) > last)
      .forEach((event) => options.onMessage(structuredClone(event)));

    if (["completed", "terminated", "failed"].includes(session.snapshot.status)) return;
    options.onConnectionChange?.(true);
    const listener = (message: SseMessage) => options.onMessage(structuredClone(message));
    session.subscribers.add(listener);

    if (!session.started) {
      session.started = true;
      void this.runPreparation(session);
    }

    await new Promise<void>((resolve) => {
      const finish = () => {
        session.subscribers.delete(listener);
        options.onConnectionChange?.(false);
        resolve();
      };
      options.signal?.addEventListener("abort", finish, { once: true });
      const terminalListener = (message: SseMessage) => {
        if (["completed", "terminated", "failed"].includes(message.event)) {
          session.subscribers.delete(terminalListener);
          finish();
        }
      };
      session.subscribers.add(terminalListener);
    });
  }

  private requireSession(id: string): MockSession {
    const session = this.sessions.get(id);
    if (!session) throw new Error("未找到该演示面试，请从首页重新开始");
    return session;
  }

  private emit<K extends keyof InterviewEventMap>(
    session: MockSession,
    event: K,
    data: InterviewEventMap[K],
    authoritative = true,
  ): void {
    const id = authoritative ? String(++session.sequence) : undefined;
    const message: SseMessage = { id, event, data };
    if (id) {
      session.snapshot.last_event_id = id;
      session.events.push(message);
    }
    session.snapshot.updated_at = new Date().toISOString();
    session.subscribers.forEach((subscriber) => subscriber(message));
  }

  private async runPreparation(session: MockSession): Promise<void> {
    const stages: Array<[string, string]> = [
      ["jd_analysis", "正在理解岗位职责与能力要求"],
      ["resume_match", "正在匹配你的经历与岗位重点"],
      ["question_plan", "正在生成个性化面试结构"],
    ];
    for (const [stage, message] of stages) {
      session.snapshot.stage = stage;
      this.emit(session, "stage", { stage, message });
      await wait(550);
      if (stage === "jd_analysis") {
        session.snapshot.jd_analysis = {
          position: "高级研发工程师",
          experience_level: "senior",
          focus_areas: ["项目深挖", "问题定位", "系统设计"],
        };
        this.emit(session, "jd_analysis", session.snapshot.jd_analysis);
      }
      if (stage === "resume_match") {
        session.snapshot.match_result = {
          overall_score: 82,
          matched_skills: ["工程实践", "服务治理", "性能优化"],
          gaps: ["量化表达", "容灾设计"],
        };
        this.emit(session, "resume_match", session.snapshot.match_result);
      }
    }
    this.emit(session, "question_plan", {
      total_questions: session.questions.length,
      distribution: { project: 1, debugging: 1, system_design: 1 },
    });
    await this.streamQuestion(session, session.questions[0]);
  }

  private async streamQuestion(session: MockSession, question: Question): Promise<void> {
    session.snapshot.status = "interviewing";
    session.snapshot.stage = "interview";
    this.emit(session, "stage", { stage: "interview", message: "面试进行中" });
    session.snapshot.current_question = question;

    const chunks = question.content.match(/.{1,10}/gu) ?? [question.content];
    for (const delta of chunks) {
      this.emit(session, "question_delta", { prompt_id: question.prompt_id, delta }, false);
      await Promise.resolve();
    }
    this.emit(session, "question", question);
    session.snapshot.awaiting_answer = {
      prompt_id: question.prompt_id,
      question_no: question.question_no,
      kind: question.kind,
    };
    this.emit(session, "awaiting_answer", session.snapshot.awaiting_answer);
  }

  private async finishInterview(session: MockSession): Promise<void> {
    session.snapshot.awaiting_answer = null;
    session.snapshot.status = "evaluating";
    session.snapshot.stage = "evaluation";
    this.emit(session, "stage", { stage: "evaluation", message: "正在综合你的回答生成评估" });
    await wait(800);

    const id = session.snapshot.interview_id;
    const createdAt = new Date().toISOString();
    const report: ReportResponse = {
      report_markdown: "# 面试评估报告\n\n你展现了扎实的工程经验与问题拆解能力。下一步重点提升量化表达和系统边界分析。",
      report: {
        interview_id: id,
        position: "高级研发工程师",
        overall_score: 82,
        overall_level: "B+",
        dimension_scores: { 技术深度: 84, 问题分析: 88, 系统设计: 78, 表达沟通: 79 },
        strengths: ["能够从真实场景出发拆解问题", "排查路径有层次，具备工程判断力", "技术选型能说明基本取舍"],
        weaknesses: ["结果缺少量化数据支撑", "故障恢复与边界条件覆盖不足"],
        detailed_review: [],
        summary: "技术基础与工程实践达到高级研发岗位预期，建议强化架构题中的约束澄清、容量估算与容灾设计。",
        created_at: createdAt,
      },
    };
    session.report = report;
    session.snapshot.report_ready = true;
    this.emit(session, "report", report);

    session.snapshot.status = "planning_review";
    session.snapshot.stage = "review_plan";
    this.emit(session, "stage", { stage: "review_plan", message: "正在整理针对性的复习计划" });
    await wait(650);
    const reviewPlan: ReviewPlanResponse = {
      plan_markdown: "# 7 天复习计划\n\n围绕量化表达、性能诊断和高可用设计完成三轮训练。",
      plan: {
        interview_id: id,
        weak_areas: ["量化表达", "容量估算", "故障恢复"],
        study_plan: [
          { day: 1, title: "重写项目案例", topics: ["STAR", "技术指标"], outcome: "完成 2 个可量化项目故事" },
          { day: 2, title: "性能诊断", topics: ["RED 指标", "Tracing", "P99"], outcome: "输出一份排障决策树" },
          { day: 3, title: "系统边界", topics: ["领域拆分", "数据一致性"], outcome: "完成一次 30 分钟架构演练" },
          { day: 5, title: "高可用设计", topics: ["限流", "降级", "容灾"], outcome: "补齐故障场景清单" },
          { day: 7, title: "模拟复盘", topics: ["限时表达", "追问"], outcome: "完成一轮二次模拟面试" },
        ],
        resources: [
          { title: "Google SRE Workbook", type: "book", url: "https://sre.google/workbook/table-of-contents/" },
          { title: "System Design Primer", type: "repository", url: "https://github.com/donnemartin/system-design-primer" },
        ],
        created_at: createdAt,
      },
    };
    session.reviewPlan = reviewPlan;
    session.snapshot.review_plan_ready = true;
    this.emit(session, "review_plan", reviewPlan);
    session.snapshot.status = "completed";
    session.snapshot.ended_reason = null;
    this.emit(session, "completed", { interview_id: id });
  }
}
