import type { InterviewFocus } from "../types/ui";
import { CheckIcon, SparkIcon, TargetIcon } from "./Icons";
import { INTERVIEW_QUESTION_COUNT } from "../config/interview";

interface FocusReviewProps {
  focus: InterviewFocus;
}

export function FocusReview({ focus }: FocusReviewProps) {
  return (
    <section className="focus-review">
      <div className="focus-review__intro">
        <span className="focus-icon"><SparkIcon /></span>
        <div>
          <span className="eyebrow">INTERVIEW BLUEPRINT</span>
          <h2>这场面试将重点验证什么</h2>
          <p>以下为前端预览；开始后，服务端会结合完整资料生成最终方向与题目结构。</p>
        </div>
      </div>

      <div className="focus-summary-grid">
        <article>
          <span>目标岗位</span>
          <strong>{focus.position}</strong>
        </article>
        <article>
          <span>预估级别</span>
          <strong>{focus.level}</strong>
        </article>
        <article>
          <span>问题数量</span>
          <strong>{INTERVIEW_QUESTION_COUNT} 题</strong>
        </article>
      </div>

      <div className="focus-details">
        <div>
          <h3><TargetIcon /> 面试重点</h3>
          <div className="tag-list">
            {focus.focusAreas.map((area) => <span className="tag" key={area}>{area}</span>)}
          </div>
        </div>
        <div>
          <h3><CheckIcon /> 简历匹配项</h3>
          {focus.matchedSkills.length > 0 ? (
            <ul className="check-list">
              {focus.matchedSkills.slice(0, 4).map((skill) => <li key={skill}><CheckIcon /> {skill}</li>)}
            </ul>
          ) : (
            <p className="muted-copy">开始后由服务端给出详细匹配结果。</p>
          )}
        </div>
      </div>

      <div className="range-field">
        <span><strong>标准 Alpha 面试</strong><small>固定 15 题，建议预留 45–75 分钟</small></span>
        <output>{INTERVIEW_QUESTION_COUNT} 题</output>
      </div>
    </section>
  );
}
