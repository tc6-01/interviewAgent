import type { InterviewStage } from "../types/api";
import { CheckIcon } from "./Icons";

const stages = [
  { id: "jd_analysis", label: "理解岗位" },
  { id: "resume_match", label: "匹配经历" },
  { id: "question_plan", label: "规划问题" },
  { id: "interview", label: "模拟面试" },
  { id: "evaluation", label: "综合评估" },
  { id: "review_plan", label: "复习计划" },
];

export function StageTimeline({ stage }: { stage: InterviewStage }) {
  const activeIndex = Math.max(0, stages.findIndex((item) => item.id === stage));
  return (
    <ol className="stage-timeline" aria-label="面试生成进度">
      {stages.map((item, index) => (
        <li
          className={`${index === activeIndex ? "is-active" : ""}${index < activeIndex ? " is-complete" : ""}`}
          key={item.id}
        >
          <span className="stage-dot">{index < activeIndex && <CheckIcon />}</span>
          <span>{item.label}</span>
        </li>
      ))}
    </ol>
  );
}
