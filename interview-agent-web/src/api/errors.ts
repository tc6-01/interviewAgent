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
