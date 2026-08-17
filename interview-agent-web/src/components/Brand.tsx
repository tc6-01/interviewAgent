import { SparkIcon } from "./Icons";

export function Brand() {
  return (
    <a className="brand" href="#/setup" aria-label="InterviewAgent 首页">
      <span className="brand-mark"><SparkIcon /></span>
      <span className="brand-copy">
        <strong>InterviewAgent</strong>
        <small>AI MOCK INTERVIEW</small>
      </span>
    </a>
  );
}
