import type {
  AwaitingAnswer,
  InterviewReport,
  InterviewStage,
  InterviewStatus,
  Question,
  ReviewPlan,
  ScoreEvent,
} from "./api";

export interface InterviewFocus {
  position: string;
  level: string;
  focusAreas: string[];
  matchedSkills: string[];
}

export interface ChatMessage {
  id: string;
  role: "interviewer" | "candidate" | "system" | "feedback";
  content: string;
  promptId?: string;
  questionNo?: number;
  kind?: string;
  score?: ScoreEvent;
  createdAt: string;
}

export interface InterviewSessionState {
  id: string;
  status: InterviewStatus;
  stage: InterviewStage;
  stageMessage: string;
  messages: ChatMessage[];
  currentQuestion: Question | null;
  awaitingAnswer: AwaitingAnswer | null;
  streamingQuestion: string;
  answered: number;
  total: number;
  lastEventId: string;
  focus: InterviewFocus | null;
  report: InterviewReport | null;
  reviewPlan: ReviewPlan | null;
  reportMarkdown: string;
  planMarkdown: string;
  warning: string | null;
  error: string | null;
  connected: boolean;
  createdAt: string;
}
