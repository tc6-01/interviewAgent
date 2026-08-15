export type InterviewStatus =
  | "preparing"
  | "interviewing"
  | "evaluating"
  | "planning_review"
  | "completed"
  | "terminated"
  | "failed";

export type ArtifactStatus = "not_started" | "generating" | "ready" | "failed";

export type InterviewStage =
  | "jd_analysis"
  | "resume_match"
  | "question_plan"
  | "rag_retrieval"
  | "question_assemble"
  | "interview"
  | "weak_review"
  | "evaluation"
  | "review_plan"
  | string;

export interface ApiErrorShape {
  error: {
    code: string;
    message: string;
    request_id?: string;
    details?: Record<string, unknown>;
  };
}

export interface ParsedDocument {
  kind: "jd" | "resume";
  text: string;
  chars: number;
  source: {
    type: "file" | "text" | "url";
    name?: string;
    url?: string;
  };
  warnings: string[];
}

export interface CreateInterviewInput {
  jd_text: string;
  resume_text: string;
  options?: {
    question_count?: number;
  };
}

export interface CreateInterviewResponse {
  interview_id: string;
  status: "preparing";
  events_url: string;
  created_at: string;
}

export interface Question {
  prompt_id: string;
  question_no: number;
  kind: "primary" | "followup" | string;
  content: string;
  source?: string;
}

export interface AwaitingAnswer {
  prompt_id: string;
  question_no: number;
  kind: string;
}

export interface QaHistoryItem {
  prompt_id: string;
  question_no: number;
  kind: string;
  question: string;
  answer: string;
  score?: number;
  feedback?: string;
}

export interface InterviewSnapshot {
  interview_id: string;
  status: InterviewStatus;
  stage: InterviewStage;
  awaiting_answer: AwaitingAnswer | null;
  current_question: Question | null;
  progress: {
    answered: number;
    total: number;
  };
  jd_analysis?: {
    position?: string;
    experience_level?: string;
    focus_areas?: string[];
  };
  match_result?: {
    overall_score?: number;
    matched_skills?: string[];
    gaps?: string[];
  };
  qa_history: QaHistoryItem[];
  ended_reason: string | null;
  report_status: ArtifactStatus;
  review_plan_status: ArtifactStatus;
  report_ready: boolean;
  review_plan_ready: boolean;
  last_event_id: string;
  created_at: string;
  updated_at: string;
}

export interface ScoreEvent {
  prompt_id: string;
  score: number;
  feedback: string;
  key_points_hit?: string[];
  key_points_missed?: string[];
  is_follow_up?: boolean;
}

export interface InterviewReport {
  interview_id: string;
  position: string;
  overall_score: number;
  overall_level: string;
  dimension_scores: Record<string, number>;
  strengths: string[];
  weaknesses: string[];
  detailed_review: Array<{
    question_content: string;
    user_answer: string;
    score: number;
    comment: string;
    key_points_hit: string[];
    key_points_missed: string[];
  }>;
  summary: string;
  created_at: string;
}

export interface ReportResponse {
  report_markdown: string;
  report: InterviewReport;
}

export interface ReviewPlan {
  interview_id: string;
  weak_areas: Array<{
    topic: string;
    score: number;
    priority: "high" | "medium" | "low";
  }>;
  study_plan: Array<{
    topic: string;
    objective: string;
    actions: string[];
    time_estimate: string;
  }>;
  resources: Array<{
    title: string;
    url?: string;
    type: "article" | "video" | "repo" | "book";
    desc: string;
  }>;
  created_at: string;
}

export interface ReviewPlanResponse {
  plan_markdown: string;
  plan: ReviewPlan;
}

export interface RetryArtifactResponse {
  accepted: true;
  interview_id: string;
  artifact: "report" | "review_plan";
}

export interface SseMessage<T = unknown> {
  id?: string;
  event: string;
  data: T;
}

export interface InterviewEventMap {
  stage: { stage: InterviewStage; message: string };
  jd_analysis: NonNullable<InterviewSnapshot["jd_analysis"]>;
  resume_match: NonNullable<InterviewSnapshot["match_result"]>;
  question_plan: { total_questions: number; distribution?: Record<string, number> };
  question_delta: { prompt_id: string; delta: string };
  question: Question;
  awaiting_answer: AwaitingAnswer;
  score: ScoreEvent;
  warning: { code?: string; message: string };
  completed: { interview_id?: string };
  terminated: { interview_id?: string; reason?: string };
  failed: { code?: string; message?: string; reason?: string };
  report: ReportResponse;
  review_plan: ReviewPlanResponse;
}
