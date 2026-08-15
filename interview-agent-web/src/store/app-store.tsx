import {
  createContext,
  type Dispatch,
  type PropsWithChildren,
  useContext,
  useEffect,
  useMemo,
  useReducer,
} from "react";
import type {
  CreateInterviewResponse,
  InterviewSnapshot,
  SseMessage,
} from "../types/api";
import type {
  ChatMessage,
  InterviewFocus,
  InterviewSessionState,
} from "../types/ui";

interface SetupDraft {
  jdText: string;
  resumeText: string;
  jdSourceName: string;
  resumeSourceName: string;
  questionCount: number;
  focus: InterviewFocus | null;
}

interface AppState {
  setup: SetupDraft;
  sessions: Record<string, InterviewSessionState>;
}

type Action =
  | { type: "patch_setup"; patch: Partial<SetupDraft> }
  | { type: "reset_setup" }
  | {
      type: "create_session";
      response: CreateInterviewResponse;
      focus: InterviewFocus;
      total: number;
    }
  | { type: "hydrate_session"; snapshot: InterviewSnapshot }
  | { type: "patch_session"; id: string; patch: Partial<InterviewSessionState> }
  | { type: "candidate_message"; id: string; promptId: string; content: string }
  | { type: "event"; id: string; message: SseMessage };

const STORAGE_KEY = "interview-agent-web-state-v1";

const emptySetup: SetupDraft = {
  jdText: "",
  resumeText: "",
  jdSourceName: "",
  resumeSourceName: "",
  questionCount: 8,
  focus: null,
};

const initialState: AppState = { setup: emptySetup, sessions: {} };

function readPersistedState(): AppState {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return initialState;
    const parsed = JSON.parse(raw) as AppState;
    return { setup: { ...emptySetup, ...parsed.setup }, sessions: parsed.sessions ?? {} };
  } catch {
    return initialState;
  }
}

function now(): string {
  return new Date().toISOString();
}

function createSession(
  response: CreateInterviewResponse,
  focus: InterviewFocus,
  total: number,
): InterviewSessionState {
  return {
    id: response.interview_id,
    status: response.status,
    stage: "jd_analysis",
    stageMessage: "正在建立面试会话",
    messages: [
      {
        id: `system-${response.interview_id}`,
        role: "system",
        content: "资料已提交。面试官正在理解岗位、匹配经历并规划本轮问题。",
        createdAt: response.created_at,
      },
    ],
    currentQuestion: null,
    awaitingAnswer: null,
    streamingQuestion: "",
    answered: 0,
    total,
    lastEventId: "0",
    focus,
    report: null,
    reviewPlan: null,
    reportMarkdown: "",
    planMarkdown: "",
    warning: null,
    error: null,
    connected: false,
    createdAt: response.created_at,
  };
}

function messagesFromSnapshot(snapshot: InterviewSnapshot): ChatMessage[] {
  const messages: ChatMessage[] = [];
  snapshot.qa_history.forEach((item) => {
    messages.push({
      id: `q-${item.prompt_id}`,
      role: "interviewer",
      content: item.question,
      promptId: item.prompt_id,
      questionNo: item.question_no,
      kind: item.kind,
      createdAt: snapshot.updated_at,
    });
    messages.push({
      id: `a-${item.prompt_id}`,
      role: "candidate",
      content: item.answer,
      promptId: item.prompt_id,
      createdAt: snapshot.updated_at,
    });
    if (item.score !== undefined) {
      messages.push({
        id: `score-${item.prompt_id}`,
        role: "feedback",
        content: item.feedback ?? "本题已完成评估。",
        promptId: item.prompt_id,
        score: {
          prompt_id: item.prompt_id,
          score: item.score,
          feedback: item.feedback ?? "",
        },
        createdAt: snapshot.updated_at,
      });
    }
  });
  if (
    snapshot.current_question &&
    !snapshot.qa_history.some((item) => item.prompt_id === snapshot.current_question?.prompt_id)
  ) {
    messages.push({
      id: `q-${snapshot.current_question.prompt_id}`,
      role: "interviewer",
      content: snapshot.current_question.content,
      promptId: snapshot.current_question.prompt_id,
      questionNo: snapshot.current_question.question_no,
      kind: snapshot.current_question.kind,
      createdAt: snapshot.updated_at,
    });
  }
  return messages;
}

function hydrateSession(snapshot: InterviewSnapshot, existing?: InterviewSessionState): InterviewSessionState {
  return {
    id: snapshot.interview_id,
    status: snapshot.status,
    stage: snapshot.stage,
    stageMessage: existing?.stageMessage ?? "已恢复面试进度",
    messages: messagesFromSnapshot(snapshot),
    currentQuestion: snapshot.current_question,
    awaitingAnswer: snapshot.awaiting_answer,
    streamingQuestion: "",
    answered: snapshot.progress.answered,
    total: snapshot.progress.total,
    lastEventId: snapshot.last_event_id,
    focus: {
      position: snapshot.jd_analysis?.position ?? existing?.focus?.position ?? "模拟面试",
      level: snapshot.jd_analysis?.experience_level ?? existing?.focus?.level ?? "自适应",
      focusAreas: snapshot.jd_analysis?.focus_areas ?? existing?.focus?.focusAreas ?? [],
      matchedSkills: snapshot.match_result?.matched_skills ?? existing?.focus?.matchedSkills ?? [],
    },
    report: existing?.report ?? null,
    reviewPlan: existing?.reviewPlan ?? null,
    reportMarkdown: existing?.reportMarkdown ?? "",
    planMarkdown: existing?.planMarkdown ?? "",
    warning: null,
    error: snapshot.status === "failed" ? `面试已中断：${snapshot.ended_reason ?? "未知原因"}` : null,
    connected: false,
    createdAt: snapshot.created_at,
  };
}

function appendUnique(messages: ChatMessage[], message: ChatMessage): ChatMessage[] {
  return messages.some((item) => item.id === message.id) ? messages : [...messages, message];
}

function applyEvent(session: InterviewSessionState, message: SseMessage): InterviewSessionState {
  const next = { ...session, lastEventId: message.id ?? session.lastEventId };
  const data = message.data as Record<string, unknown>;

  switch (message.event) {
    case "stage":
      return {
        ...next,
        stage: String(data.stage ?? session.stage),
        stageMessage: String(data.message ?? "处理中"),
        status:
          data.stage === "evaluation"
            ? "evaluating"
            : data.stage === "review_plan"
              ? "planning_review"
              : session.status,
      };
    case "jd_analysis":
      return {
        ...next,
        focus: {
          position: String(data.position ?? session.focus?.position ?? "模拟面试"),
          level: String(data.experience_level ?? session.focus?.level ?? "自适应"),
          focusAreas: Array.isArray(data.focus_areas)
            ? data.focus_areas.map(String)
            : session.focus?.focusAreas ?? [],
          matchedSkills: session.focus?.matchedSkills ?? [],
        },
      };
    case "resume_match":
      return {
        ...next,
        focus: {
          position: session.focus?.position ?? "模拟面试",
          level: session.focus?.level ?? "自适应",
          focusAreas: session.focus?.focusAreas ?? [],
          matchedSkills: Array.isArray(data.matched_skills)
            ? data.matched_skills.map(String)
            : session.focus?.matchedSkills ?? [],
        },
      };
    case "question_plan":
      return { ...next, total: Number(data.total_questions ?? session.total) };
    case "question_delta":
      return {
        ...next,
        streamingQuestion: `${session.streamingQuestion}${String(data.delta ?? "")}`,
      };
    case "question": {
      const question = data as unknown as InterviewSessionState["currentQuestion"];
      if (!question) return next;
      return {
        ...next,
        status: "interviewing",
        currentQuestion: question,
        streamingQuestion: "",
        messages: appendUnique(session.messages, {
          id: `q-${question.prompt_id}`,
          role: "interviewer",
          content: question.content,
          promptId: question.prompt_id,
          questionNo: question.question_no,
          kind: question.kind,
          createdAt: now(),
        }),
      };
    }
    case "awaiting_answer":
      return {
        ...next,
        awaitingAnswer: {
          prompt_id: String(data.prompt_id),
          question_no: Number(data.question_no),
          kind: String(data.kind ?? "primary"),
        },
      };
    case "score": {
      const promptId = String(data.prompt_id);
      return {
        ...next,
        answered: Math.max(session.answered, session.messages.filter((item) => item.role === "candidate").length),
        messages: appendUnique(session.messages, {
          id: `score-${promptId}`,
          role: "feedback",
          content: String(data.feedback ?? "本题已完成评估。"),
          promptId,
          score: {
            prompt_id: promptId,
            score: Number(data.score ?? 0),
            feedback: String(data.feedback ?? ""),
            key_points_hit: Array.isArray(data.key_points_hit) ? data.key_points_hit.map(String) : [],
            key_points_missed: Array.isArray(data.key_points_missed)
              ? data.key_points_missed.map(String)
              : [],
          },
          createdAt: now(),
        }),
      };
    }
    case "report": {
      const payload = message.data as { report?: InterviewSessionState["report"]; report_markdown?: string };
      return {
        ...next,
        report: payload.report ?? null,
        reportMarkdown: payload.report_markdown ?? "",
      };
    }
    case "review_plan": {
      const payload = message.data as {
        plan?: InterviewSessionState["reviewPlan"];
        plan_markdown?: string;
      };
      return {
        ...next,
        reviewPlan: payload.plan ?? null,
        planMarkdown: payload.plan_markdown ?? "",
      };
    }
    case "warning":
      return { ...next, warning: String(data.message ?? "部分能力暂时降级") };
    case "completed":
      return { ...next, status: "completed", awaitingAnswer: null, connected: false };
    case "terminated":
      return { ...next, status: "terminated", awaitingAnswer: null, connected: false };
    case "failed":
      return {
        ...next,
        status: "failed",
        awaitingAnswer: null,
        connected: false,
        error: String(data.message ?? data.reason ?? "面试发生不可恢复错误"),
      };
    default:
      return next;
  }
}

function reducer(state: AppState, action: Action): AppState {
  switch (action.type) {
    case "patch_setup":
      return { ...state, setup: { ...state.setup, ...action.patch } };
    case "reset_setup":
      return { ...state, setup: emptySetup };
    case "create_session":
      return {
        ...state,
        sessions: {
          ...state.sessions,
          [action.response.interview_id]: createSession(
            action.response,
            action.focus,
            action.total,
          ),
        },
      };
    case "hydrate_session":
      return {
        ...state,
        sessions: {
          ...state.sessions,
          [action.snapshot.interview_id]: hydrateSession(
            action.snapshot,
            state.sessions[action.snapshot.interview_id],
          ),
        },
      };
    case "patch_session": {
      const session = state.sessions[action.id];
      if (!session) return state;
      return {
        ...state,
        sessions: { ...state.sessions, [action.id]: { ...session, ...action.patch } },
      };
    }
    case "candidate_message": {
      const session = state.sessions[action.id];
      if (!session) return state;
      return {
        ...state,
        sessions: {
          ...state.sessions,
          [action.id]: {
            ...session,
            awaitingAnswer: null,
            messages: appendUnique(session.messages, {
              id: `a-${action.promptId}`,
              role: "candidate",
              content: action.content,
              promptId: action.promptId,
              createdAt: now(),
            }),
          },
        },
      };
    }
    case "event": {
      const session = state.sessions[action.id];
      if (!session) return state;
      return {
        ...state,
        sessions: { ...state.sessions, [action.id]: applyEvent(session, action.message) },
      };
    }
  }
}

interface StoreValue {
  state: AppState;
  dispatch: Dispatch<Action>;
}

const StoreContext = createContext<StoreValue | null>(null);

export function AppStoreProvider({ children }: PropsWithChildren) {
  const [state, dispatch] = useReducer(reducer, undefined, readPersistedState);
  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  }, [state]);
  const value = useMemo(() => ({ state, dispatch }), [state]);
  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>;
}

export function useAppStore(): StoreValue {
  const store = useContext(StoreContext);
  if (!store) throw new Error("useAppStore must be used within AppStoreProvider");
  return store;
}
