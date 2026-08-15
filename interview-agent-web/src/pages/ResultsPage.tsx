import { type CSSProperties, useEffect, useState } from "react";
import { api } from "../api/client";
import { navigate } from "../app/router";
import { AppShell } from "../components/AppShell";
import { BookIcon, CheckIcon, DownloadIcon, RefreshIcon, SparkIcon, TargetIcon } from "../components/Icons";
import { MarkdownText } from "../components/MarkdownText";
import { ProgressSteps } from "../components/ProgressSteps";
import { useAppStore } from "../store/app-store";
import type { ArtifactStatus } from "../types/api";

type Artifact = "report" | "review_plan";

export function ResultsPage({ interviewId }: { interviewId: string }) {
  const { state, dispatch } = useAppStore();
  const session = state.sessions[interviewId];
  const [loading, setLoading] = useState(!session?.report);
  const [error, setError] = useState<string | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [retryingReport, setRetryingReport] = useState(false);
  const [retryingPlan, setRetryingPlan] = useState(false);
  const [tab, setTab] = useState<"report" | "plan">("report");

  const loadReadyArtifact = async (artifact: Artifact) => {
    if (artifact === "report") {
      const response = await api.getReport(interviewId);
      dispatch({
        type: "patch_session",
        id: interviewId,
        patch: {
          report: response.report,
          reportMarkdown: response.report_markdown,
          reportStatus: "ready",
        },
      });
      return;
    }
    const response = await api.getReviewPlan(interviewId);
    dispatch({
      type: "patch_session",
      id: interviewId,
      patch: {
        reviewPlan: response.plan,
        planMarkdown: response.plan_markdown,
        reviewPlanStatus: "ready",
      },
    });
  };

  const waitForArtifact = async (artifact: Artifact, cancelled: () => boolean = () => false) => {
    for (let attempt = 0; attempt < 60 && !cancelled(); attempt += 1) {
      const snapshot = await api.getInterview(interviewId);
      if (cancelled()) return;
      dispatch({ type: "hydrate_session", snapshot });
      const status: ArtifactStatus = artifact === "report"
        ? snapshot.report_status
        : snapshot.review_plan_status;
      if (status === "ready") {
        await loadReadyArtifact(artifact);
        return;
      }
      if (status === "failed") {
        throw new Error(artifact === "report" ? "评估报告生成失败，可以单独重试。" : "复习计划生成失败，可以单独重试。");
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1_000));
    }
    if (!cancelled()) throw new Error("生成时间较长，请稍后重新加载查看结果。");
  };

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const snapshot = await api.getInterview(interviewId);
        if (cancelled) return;
        dispatch({ type: "hydrate_session", snapshot });

        if (snapshot.report_status === "ready") {
          if (!session?.report) await loadReadyArtifact("report");
        } else if (snapshot.report_status === "generating") {
          await waitForArtifact("report", () => cancelled);
        } else if (snapshot.report_status === "failed") {
          setError("逐题评分已保留，但评估报告生成失败，可以只重试报告生成。");
        } else {
          setError("本场面试没有可用的评估报告。完成至少一道主问题后提前结束，才会生成报告。");
        }
      } catch (loadError) {
        if (!cancelled) setError(loadError instanceof Error ? loadError.message : "加载评估报告失败");
      } finally {
        if (!cancelled) setLoading(false);
      }

      try {
        const snapshot = await api.getInterview(interviewId);
        if (cancelled) return;
        dispatch({ type: "hydrate_session", snapshot });
        if (snapshot.review_plan_status === "ready") {
          if (!session?.reviewPlan) await loadReadyArtifact("review_plan");
        } else if (snapshot.review_plan_status === "generating") {
          await waitForArtifact("review_plan", () => cancelled);
        } else if (snapshot.review_plan_status === "failed") {
          setPlanError("评估报告仍可使用，但复习计划生成失败，可以只重试这一阶段。");
        }
      } catch (loadError) {
        if (!cancelled) setPlanError(loadError instanceof Error ? loadError.message : "加载复习计划失败");
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
    // Route-level artifact recovery intentionally starts once for this interview id.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dispatch, interviewId]);

  const retryReport = async () => {
    setRetryingReport(true);
    setError(null);
    try {
      await api.retryReport(interviewId);
      dispatch({ type: "patch_session", id: interviewId, patch: { reportStatus: "generating" } });
      await waitForArtifact("report");
      const snapshot = await api.getInterview(interviewId);
      dispatch({ type: "hydrate_session", snapshot });
      if (snapshot.review_plan_status === "ready") await loadReadyArtifact("review_plan");
      if (snapshot.review_plan_status === "generating") {
        await waitForArtifact("review_plan");
      }
    } catch (retryError) {
      setError(retryError instanceof Error ? retryError.message : "评估报告重试失败");
    } finally {
      setRetryingReport(false);
    }
  };

  const retryPlan = async () => {
    setRetryingPlan(true);
    setPlanError(null);
    try {
      await api.retryReviewPlan(interviewId);
      dispatch({ type: "patch_session", id: interviewId, patch: { reviewPlanStatus: "generating" } });
      await waitForArtifact("review_plan");
    } catch (retryError) {
      setPlanError(retryError instanceof Error ? retryError.message : "复习计划重试失败");
    } finally {
      setRetryingPlan(false);
    }
  };

  if (loading) {
    return <AppShell><main className="center-state"><span className="loader-orbit"><SparkIcon /></span><h1>正在整理你的复盘</h1><p>汇总评分、优势、薄弱点与学习路径…</p></main></AppShell>;
  }
  if (!session?.report) {
    const canRetry = session?.reportStatus === "failed";
    return (
      <AppShell>
        <main className="center-state">
          <h1>{canRetry ? "评估报告需要重试" : "本次未生成报告"}</h1>
          <p>{error ?? "结果仍在生成，请稍后再试。"}</p>
          {canRetry ? (
            <button className="button button--primary" type="button" onClick={() => void retryReport()} disabled={retryingReport}>
              <RefreshIcon /> {retryingReport ? "正在重新生成…" : "重试生成评估报告"}
            </button>
          ) : (
            <button className="button button--primary" type="button" onClick={() => navigate("/setup")}>返回首页</button>
          )}
        </main>
      </AppShell>
    );
  }

  const { report, reviewPlan } = session;
  const scoreStyle = { "--score": `${report.overall_score * 3.6}deg` } as CSSProperties;

  const download = () => {
    const content = `${session.reportMarkdown}\n\n---\n\n${session.planMarkdown}`;
    const url = URL.createObjectURL(new Blob([content], { type: "text/markdown;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `interview-review-${interviewId}.md`;
    anchor.click();
    URL.revokeObjectURL(url);
  };

  const restart = () => {
    dispatch({ type: "reset_setup" });
    navigate("/setup");
  };

  return (
    <AppShell action={<button className="button button--secondary button--small" type="button" onClick={download}><DownloadIcon /> 导出 Markdown</button>}>
      <main className="results-page">
        <ProgressSteps current={4} />
        <section className="results-hero">
          <div>
            <span className="eyebrow">INTERVIEW COMPLETE</span>
            <h1>这不是终点，<br /><em>是下一次更好的起点。</em></h1>
            <p>{report.summary}</p>
          </div>
          <div className="score-ring" style={scoreStyle} aria-label={`综合得分 ${report.overall_score} 分`}>
            <div><strong>{report.overall_score}</strong><span>/ 100</span><small>{report.overall_level}</small></div>
          </div>
        </section>
        <p className="score-disclaimer" role="note">分数仅供自评参考，不代表真实面试结果。</p>

        <div className="result-tabs" role="tablist">
          <button type="button" role="tab" aria-selected={tab === "report"} className={tab === "report" ? "is-active" : ""} onClick={() => setTab("report")}><TargetIcon /> 评估报告</button>
          <button type="button" role="tab" aria-selected={tab === "plan"} className={tab === "plan" ? "is-active" : ""} onClick={() => setTab("plan")}><BookIcon /> 复习计划</button>
        </div>

        {tab === "report" ? (
          <div className="report-layout">
            <section className="result-card result-card--dimensions">
              <div className="section-heading"><span className="eyebrow">能力画像</span><h2>你在这场面试中的表现</h2></div>
              <div className="dimension-list">
                {Object.entries(report.dimension_scores).map(([name, score]) => (
                  <div className="dimension" key={name}>
                    <div><span>{name}</span><strong>{score}</strong></div>
                    <div className="dimension-track"><span style={{ width: `${score}%` }} /></div>
                  </div>
                ))}
              </div>
            </section>

            <section className="result-card result-card--strengths">
              <div className="section-heading"><span className="eyebrow">STRENGTHS</span><h2>值得保留的优势</h2></div>
              <ul className="insight-list insight-list--positive">
                {report.strengths.map((item) => <li key={item}><span><CheckIcon /></span><p>{item}</p></li>)}
              </ul>
            </section>

            <section className="result-card result-card--weaknesses">
              <div className="section-heading"><span className="eyebrow">GROWTH AREAS</span><h2>下一步突破点</h2></div>
              <ul className="insight-list">
                {report.weaknesses.map((item, index) => (
                  <li key={item}><span>{String(index + 1).padStart(2, "0")}</span><p className="weakness-copy"><strong>{item}</strong></p></li>
                ))}
              </ul>
            </section>

            <section className="result-card result-card--reviews">
              <div className="section-heading"><span className="eyebrow">QUESTION EVIDENCE</span><h2>逐题回顾与证据</h2></div>
              <div className="question-review-list">
                {report.detailed_review.map((review, index) => (
                  <article key={`${index}-${review.question_content}`}>
                    <div className="question-review__heading"><strong>第 {index + 1} 轮</strong><span>{review.score} 分</span></div>
                    <h3>{review.question_content}</h3>
                    <p><b>你的回答：</b>{review.user_answer}</p>
                    <p><b>点评：</b>{review.comment}</p>
                    <div className="feedback-points">
                      {review.key_points_hit.map((point) => <span className="point point--hit" key={point}>✓ {point}</span>)}
                      {review.key_points_missed.map((point) => <span className="point point--miss" key={point}>补充：{point}</span>)}
                    </div>
                  </article>
                ))}
              </div>
            </section>

            {session.reportMarkdown && <section className="result-card result-card--markdown"><MarkdownText content={session.reportMarkdown} /></section>}
          </div>
        ) : reviewPlan ? (
          <div className="plan-layout">
            <section className="result-card plan-overview">
              <div className="section-heading"><span className="eyebrow">YOUR FOCUS</span><h2>优先补齐这些方向</h2></div>
              <div className="weak-area-list">
                {reviewPlan.weak_areas.map((area, index) => (
                  <article key={area.topic}><span>{index + 1}</span><div><strong>{area.topic}</strong><small>{priorityLabel(area.priority)} · 当前 {area.score} 分</small></div></article>
                ))}
              </div>
            </section>

            <section className="result-card plan-schedule">
              <div className="section-heading"><span className="eyebrow">ACTION PLAN</span><h2>把复习拆成可以完成的动作</h2></div>
              <ol className="study-plan">
                {reviewPlan.study_plan.map((item, index) => (
                  <li key={item.topic}>
                    <span className="study-day">STEP {index + 1}</span>
                    <div><h3>{item.topic}</h3><p>{item.objective}</p><ul className="study-actions">{item.actions.map((action) => <li key={action}>{action}</li>)}</ul><small>预计 {item.time_estimate}</small></div>
                  </li>
                ))}
              </ol>
            </section>

            <section className="result-card resources-card">
              <div className="section-heading"><span className="eyebrow">RESOURCES</span><h2>推荐材料</h2></div>
              <div className="resource-list">
                {reviewPlan.resources.map((resource) => {
                  const content = <><BookIcon /><span><strong>{resource.title}</strong><small>{resource.type} · {resource.desc}</small></span>{resource.url && <span aria-hidden="true">↗</span>}</>;
                  return resource.url ? <a href={resource.url} target="_blank" rel="noreferrer" key={resource.title}>{content}</a> : <article key={resource.title}>{content}</article>;
                })}
              </div>
            </section>

            {session.planMarkdown && <section className="result-card result-card--markdown"><MarkdownText content={session.planMarkdown} /></section>}
          </div>
        ) : (
          <section className="result-card plan-retry-card" role="status">
            <div className="section-heading"><span className="eyebrow">REVIEW PLAN</span><h2>{session.reviewPlanStatus === "generating" ? "复习计划正在生成" : "评估已完成，复习计划需要重试"}</h2></div>
            <p>{planError ?? "复习计划正在生成；只处理这一阶段，不会重复面试或评估。"}</p>
            {session.reviewPlanStatus === "failed" && (
              <button className="button button--primary" type="button" onClick={() => void retryPlan()} disabled={retryingPlan}>
                <RefreshIcon /> {retryingPlan ? "正在重新生成…" : "重试生成复习计划"}
              </button>
            )}
          </section>
        )}

        <section className="results-next">
          <div><span className="eyebrow">KEEP PRACTICING</span><h2>准备下一次模拟？</h2><p>换一个岗位，或带着复习成果完成新一轮 15 题面试。</p></div>
          <button className="button button--primary button--large" type="button" onClick={restart}><RefreshIcon /> 开始新的面试</button>
        </section>
      </main>
      <footer className="site-footer"><span>InterviewAgent · Open source mock interview</span><span>报告可导出为 Markdown 长期保存</span></footer>
    </AppShell>
  );
}

function priorityLabel(priority: "high" | "medium" | "low"): string {
  return { high: "高优先级", medium: "中优先级", low: "低优先级" }[priority];
}
