import type {
  CreateInterviewInput,
  CreateInterviewResponse,
  InterviewEventMap,
  InterviewSnapshot,
  ParsedDocument,
  Question,
  ReportResponse,
  RetryArtifactResponse,
  ReviewPlanResponse,
  SseMessage,
} from "../types/api";
import type { InterviewApi, ParseDocumentInput, SubscribeOptions } from "./client";
import { InterviewApiError } from "./errors";
import { INTERVIEW_QUESTION_COUNT } from "../config/interview";

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
  reportFailuresRemaining: number;
  reviewPlanFailuresRemaining: number;
}

interface MockInterviewApiOptions {
  delayScale?: number;
  failReportOnce?: boolean;
  failReviewPlanOnce?: boolean;
}

const sampleQuestionContent = [
  "请先选一个你最有代表性的项目，说明你负责的部分、遇到的关键挑战，以及最终产生的结果。",
  "如果线上接口的 P99 延迟突然升高，你会怎样定位？请按排查顺序说明你关注的指标和判断依据。",
  "现在需要把一个单体服务拆成可扩展的架构，你会如何划分边界，并处理数据一致性与故障恢复？",
  "请说明你做过的一次关键技术选型。当时有哪些备选方案，你如何验证并承担最终结果？",
  "一个依赖服务开始间歇性超时，你会怎样设计重试、熔断、降级与监控，避免故障继续放大？",
  "请设计一个支持突发流量的任务处理系统，说明队列、幂等、背压和失败补偿策略。",
  "线上出现数据不一致但没有明显报错时，你会如何缩小范围、保护现场并推进修复？",
  "请结合实际经历说明你如何推动一次跨团队技术改造，以及遇到分歧时怎样达成共识。",
  "如果核心数据库容量将在三个月内触顶，你会如何完成容量评估、拆分方案与迁移演练？",
  "请解释缓存穿透、击穿和雪崩的差异，并给出你会在生产环境采用的组合治理方案。",
  "面对一个缺少测试、频繁回归的老系统，你会怎样分阶段补齐质量保障而不阻塞业务迭代？",
  "请设计关键链路的可观测性方案，说明日志、指标、追踪和告警分别解决什么问题。",
  "当需求目标明确但实现周期明显不足时，你会如何拆范围、识别风险并与产品负责人沟通？",
  "请复盘一次你判断失误或方案失败的经历。你后来如何修正，并沉淀了什么机制？",
  "最后，请总结你与这个岗位最匹配的三项能力，以及入职后最希望优先补齐的一项能力。",
] as const;

const sampleQuestions: Question[] = sampleQuestionContent.map((content, index) => ({
  prompt_id: `prompt_${index + 1}_main`,
  question_no: index + 1,
  kind: "primary",
  content,
  source: `demo:question-${index + 1}`,
}));

export class MockInterviewApi implements InterviewApi {
  readonly mode = "mock" as const;
  private sessions = new Map<string, MockSession>();
  private readonly delayScale: number;
  private readonly failReportOnce: boolean;
  private readonly failReviewPlanOnce: boolean;

  constructor(options: MockInterviewApiOptions = {}) {
    this.delayScale = options.delayScale ?? 1;
    this.failReportOnce = options.failReportOnce ?? false;
    this.failReviewPlanOnce = options.failReviewPlanOnce ?? false;
  }

  private wait(milliseconds: number): Promise<void> {
    return new Promise((resolve) => globalThis.setTimeout(resolve, milliseconds * this.delayScale));
  }

  async parseDocument(input: ParseDocumentInput): Promise<ParsedDocument> {
    await this.wait(450);
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
    await this.wait(350);
    const id = `demo_${crypto.randomUUID().slice(0, 8)}`;
    const createdAt = new Date().toISOString();
    const total = INTERVIEW_QUESTION_COUNT;
    const session: MockSession = {
      input,
      started: false,
      sequence: 0,
      currentIndex: 0,
      questions: sampleQuestions.slice(0, total),
      events: [],
      subscribers: new Set(),
      reportFailuresRemaining: this.failReportOnce ? 1 : 0,
      reviewPlanFailuresRemaining: this.failReviewPlanOnce ? 1 : 0,
      snapshot: {
        interview_id: id,
        status: "preparing",
        stage: "jd_analysis",
        awaiting_answer: null,
        current_question: null,
        progress: { answered: 0, total },
        qa_history: [],
        ended_reason: null,
        report_status: "not_started",
        review_plan_status: "not_started",
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
    await this.wait(100);
    return structuredClone(this.requireSession(id).snapshot);
  }

  async submitAnswer(id: string, promptId: string, text: string): Promise<void> {
    const session = this.requireSession(id);
    const awaiting = session.snapshot.awaiting_answer;
    if (["completed", "terminated", "failed"].includes(session.snapshot.status)) {
      throw new InterviewApiError("本场面试已经结束。", "interview_finished", 409);
    }
    if (session.snapshot.qa_history.some((item) => item.prompt_id === promptId)) {
      throw new InterviewApiError("这道题的回答已经提交，已为你同步最新进度。", "answer_already_submitted", 409);
    }
    if (!awaiting) {
      throw new InterviewApiError("面试官正在处理上一轮回答。", "interview_not_awaiting_answer", 409);
    }
    if (awaiting.prompt_id !== promptId) {
      throw new InterviewApiError("当前问题已变化，请同步最新进度后重试。", "prompt_mismatch", 409, {
        current_prompt_id: awaiting.prompt_id,
      });
    }
    if (!text.trim()) throw new Error("回答不能为空");

    session.snapshot.awaiting_answer = null;
    session.snapshot.qa_history.push({
      prompt_id: awaiting.prompt_id,
      question_no: awaiting.question_no,
      kind: awaiting.kind,
      question: session.snapshot.current_question?.content ?? "",
      answer: text.trim(),
    });
    if (awaiting.kind === "primary") session.snapshot.progress.answered += 1;
    session.snapshot.updated_at = new Date().toISOString();
    void this.evaluateAnswerAndContinue(session, promptId, text.trim());
    await this.wait(100);
  }

  private async evaluateAnswerAndContinue(
    session: MockSession,
    promptId: string,
    answer: string,
  ): Promise<void> {
    await this.wait(500);

    const isFollowUp = promptId.endsWith("_followup");
    const score = Math.min(92, 38 + answer.length / 2);
    const scoreEvent: InterviewEventMap["score"] = {
      prompt_id: promptId,
      score: Math.round(score),
      feedback:
        answer.length > 80
          ? "回答结构清楚，能够结合实际场景说明判断依据。进一步补充量化指标会更有说服力。"
          : "核心思路正确。建议使用“背景—行动—结果”的结构，并补充关键指标与取舍依据。",
      key_points_hit: ["思路完整", "场景意识"],
      key_points_missed: ["量化结果", "边界条件"],
      is_follow_up: isFollowUp,
    };
    const historyItem = session.snapshot.qa_history.at(-1);
    if (historyItem) {
      historyItem.score = scoreEvent.score;
      historyItem.feedback = scoreEvent.feedback;
    }
    this.emit(session, "score", scoreEvent);

    if (!isFollowUp && scoreEvent.score < 80 && scoreEvent.score >= 30) {
      const primary = session.questions[session.currentIndex];
      await this.streamQuestion(session, {
        prompt_id: `prompt_${primary.question_no}_followup`,
        question_no: primary.question_no,
        kind: "followup",
        content: "追问：请补充一个具体指标，并说明方案失效时你会如何回滚或降级。",
        source: primary.source,
      });
      return;
    }

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
    await this.wait(100);
  }

  async getReport(id: string): Promise<ReportResponse> {
    const session = this.requireSession(id);
    if (!session.report) {
      throw new InterviewApiError("评估报告尚未就绪。", "report_not_ready", 409);
    }
    return structuredClone(session.report);
  }

  async getReviewPlan(id: string): Promise<ReviewPlanResponse> {
    const session = this.requireSession(id);
    if (!session.reviewPlan) {
      throw new InterviewApiError("复习计划生成失败，可以单独重试。", "review_plan_not_ready", 409);
    }
    return structuredClone(session.reviewPlan);
  }

  async retryReport(id: string): Promise<RetryArtifactResponse> {
    const session = this.requireSession(id);
    if (session.snapshot.report_status !== "failed") {
      const code = session.snapshot.report_status === "generating"
        ? "report_retry_in_progress"
        : "report_retry_not_allowed";
      throw new InterviewApiError("当前状态不能重试评估报告。", code, 409);
    }
    session.snapshot.report_status = "generating";
    void this.generateArtifacts(session, false, true);
    return { accepted: true, interview_id: id, artifact: "report" };
  }

  async retryReviewPlan(id: string): Promise<RetryArtifactResponse> {
    const session = this.requireSession(id);
    if (session.snapshot.review_plan_status !== "failed") {
      const code = session.snapshot.review_plan_status === "generating"
        ? "review_plan_retry_in_progress"
        : "review_plan_retry_not_allowed";
      throw new InterviewApiError("当前状态不能重试复习计划。", code, 409);
    }
    if (session.snapshot.report_status !== "ready") {
      throw new InterviewApiError("评估报告尚未就绪。", "review_plan_prerequisites_not_ready", 409);
    }
    session.snapshot.review_plan_status = "generating";
    void this.generateReviewPlan(session, false, true);
    return { accepted: true, interview_id: id, artifact: "review_plan" };
  }

  async subscribe(id: string, options: SubscribeOptions): Promise<void> {
    const session = this.requireSession(id);
    options.onSnapshot?.(structuredClone(session.snapshot));
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
      await this.wait(550);
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
      distribution: { basic: 8, experience: 5, design: 2 },
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
    session.snapshot.report_status = "generating";
    this.emit(session, "stage", { stage: "evaluation", message: "正在综合你的回答生成评估" });
    await this.generateArtifacts(session, true);
  }

  private async generateArtifacts(
    session: MockSession,
    allowFailure: boolean,
    preserveInterviewStatus = false,
  ): Promise<void> {
    await this.wait(800);

    const id = session.snapshot.interview_id;
    if (allowFailure && session.reportFailuresRemaining > 0) {
      session.reportFailuresRemaining -= 1;
      session.snapshot.report_status = "failed";
      session.snapshot.status = "completed";
      this.emit(session, "warning", {
        code: "report_generation_failed",
        message: "逐题评分已保留，但评估报告生成失败，可在结果页单独重试。",
      });
      this.emit(session, "completed", { interview_id: id });
      return;
    }

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
        detailed_review: session.snapshot.qa_history.map((item) => ({
          question_content: item.question,
          user_answer: item.answer,
          score: item.score ?? 0,
          comment: item.feedback ?? "本题已完成评分，建议继续补充具体数据与边界条件。",
          key_points_hit: ["问题拆解", "场景意识"],
          key_points_missed: ["量化结果", "异常边界"],
        })),
        summary: "技术基础与工程实践达到高级研发岗位预期，建议强化架构题中的约束澄清、容量估算与容灾设计。",
        created_at: createdAt,
      },
    };
    session.report = report;
    session.snapshot.report_status = "ready";
    session.snapshot.report_ready = true;
    this.emit(session, "report", report);

    await this.generateReviewPlan(session, true, preserveInterviewStatus);
  }

  private async generateReviewPlan(
    session: MockSession,
    allowFailure: boolean,
    preserveInterviewStatus = false,
  ): Promise<void> {
    const id = session.snapshot.interview_id;
    if (!preserveInterviewStatus) session.snapshot.status = "planning_review";
    session.snapshot.stage = "review_plan";
    session.snapshot.review_plan_status = "generating";
    this.emit(session, "stage", { stage: "review_plan", message: "正在整理针对性的复习计划" });
    await this.wait(650);
    if (allowFailure && session.reviewPlanFailuresRemaining > 0) {
      session.reviewPlanFailuresRemaining -= 1;
      session.snapshot.review_plan_status = "failed";
      session.snapshot.status = "completed";
      session.snapshot.ended_reason = null;
      this.emit(session, "warning", {
        code: "review_plan_generation_failed",
        message: "评估报告已完成，但复习计划生成失败，可在结果页单独重试。",
      });
      this.emit(session, "completed", { interview_id: id });
      return;
    }

    const reviewPlan = this.createReviewPlan(session);
    session.reviewPlan = reviewPlan;
    session.snapshot.review_plan_status = "ready";
    session.snapshot.review_plan_ready = true;
    this.emit(session, "review_plan", reviewPlan);
    session.snapshot.status = "completed";
    session.snapshot.ended_reason = null;
    this.emit(session, "completed", { interview_id: id });
  }

  private createReviewPlan(session: MockSession): ReviewPlanResponse {
    const createdAt = new Date().toISOString();
    return {
      plan_markdown: "# 7 天复习计划\n\n围绕量化表达、性能诊断和高可用设计完成三轮训练。",
      plan: {
        interview_id: session.snapshot.interview_id,
        weak_areas: [
          { topic: "量化表达", score: 58, priority: "high" },
          { topic: "故障恢复", score: 66, priority: "high" },
          { topic: "容量估算", score: 72, priority: "medium" },
        ],
        study_plan: [
          {
            topic: "量化表达",
            objective: "把项目成果转化为可验证的数据结论",
            actions: ["用 STAR 重写两个项目案例", "为每个案例补齐基线、结果与验证口径"],
            time_estimate: "2 小时",
          },
          {
            topic: "故障恢复",
            objective: "能完整说明降级、回滚与数据修复路径",
            actions: ["整理一次线上故障时间线", "画出限流、熔断、回滚和补偿决策树"],
            time_estimate: "3 小时",
          },
          {
            topic: "容量估算",
            objective: "在系统设计题中快速建立容量与成本模型",
            actions: ["完成一组 QPS/存储估算练习", "用 30 分钟复述一次扩容与迁移方案"],
            time_estimate: "2.5 小时",
          },
        ],
        resources: [
          { title: "Google SRE Workbook", type: "book", url: "https://sre.google/workbook/table-of-contents/", desc: "从告警、应急响应到可靠性实践的官方资料。" },
          { title: "System Design Primer", type: "repo", url: "https://github.com/donnemartin/system-design-primer", desc: "系统设计基础、容量估算与常见架构模式练习。" },
          { title: "Google Cloud Architecture Framework", type: "article", url: "https://cloud.google.com/architecture/framework", desc: "用于补充高可用、运维与性能设计检查项。" },
        ],
        created_at: createdAt,
      },
    };
  }
}
