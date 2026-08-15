import { type CSSProperties, useEffect, useState } from "react";
import { api } from "../api/client";
import { navigate } from "../app/router";
import { AppShell } from "../components/AppShell";
import { BookIcon, CheckIcon, DownloadIcon, RefreshIcon, SparkIcon, TargetIcon } from "../components/Icons";
import { MarkdownText } from "../components/MarkdownText";
import { ProgressSteps } from "../components/ProgressSteps";
import { useAppStore } from "../store/app-store";

export function ResultsPage({ interviewId }: { interviewId: string }) {
  const { state, dispatch } = useAppStore();
  const session = state.sessions[interviewId];
  const [loading, setLoading] = useState(!session?.report);
  const [error, setError] = useState<string | null>(null);
  const [planError, setPlanError] = useState<string | null>(null);
  const [retryingPlan, setRetryingPlan] = useState(false);
  const [tab, setTab] = useState<"report" | "plan">("report");

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        if (!session) {
          const snapshot = await api.getInterview(interviewId);
          if (cancelled) return;
          dispatch({ type: "hydrate_session", snapshot });
        }

        if (!session?.report) {
          const report = await api.getReport(interviewId);
          if (cancelled) return;
          dispatch({
            type: "patch_session",
            id: interviewId,
            patch: { report: report.report, reportMarkdown: report.report_markdown },
          });
        }
      } catch (loadError) {
        if (!cancelled) setError(loadError instanceof Error ? loadError.message : "加载评估报告失败");
      } finally {
        if (!cancelled) setLoading(false);
      }

      if (!session?.reviewPlan) {
        try {
          const plan = await api.getReviewPlan(interviewId);
          if (cancelled) return;
          dispatch({
            type: "patch_session",
            id: interviewId,
            patch: {
              reviewPlan: plan.plan,
              planMarkdown: plan.plan_markdown,
              status: "completed",
            },
          });
        } catch (loadError) {
          if (!cancelled) {
            setPlanError(loadError instanceof Error ? loadError.message : "加载复习计划失败");
          }
        }
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
    // Load once per result route; store updates are applied by the dispatched actions above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dispatch, interviewId]);

  const retryPlan = async () => {
    setRetryingPlan(true);
    setPlanError(null);
    try {
      const plan = await api.retryReviewPlan(interviewId);
      dispatch({
        type: "patch_session",
        id: interviewId,
        patch: {
          reviewPlan: plan.plan,
          planMarkdown: plan.plan_markdown,
          status: "completed",
        },
      });
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
    return <AppShell><main className="center-state"><h1>报告暂时不可用</h1><p>{error ?? "结果仍在生成，请稍后再试。"}</p><button className="button button--primary" type="button" onClick={() => window.location.reload()}>重新加载</button></main></AppShell>;
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
                {report.weaknesses.map((item, index) => {
                  const citation = report.detailed_review.find((review) => review.topic === item);
                  return (
                    <li key={item}>
                      <span>{String(index + 1).padStart(2, "0")}</span>
                      <p className="weakness-copy"><strong>{item}</strong>{citation?.summary && <small>{citation.summary}</small>}</p>
                    </li>
                  );
                })}
              </ul>
            </section>

            {session.reportMarkdown && <section className="result-card result-card--markdown"><MarkdownText content={session.reportMarkdown} /></section>}
          </div>
        ) : reviewPlan ? (
          <div className="plan-layout">
            <section className="result-card plan-overview">
              <div className="section-heading"><span className="eyebrow">YOUR FOCUS</span><h2>优先补齐这三个方向</h2></div>
              <div className="weak-area-list">{reviewPlan.weak_areas.map((area, index) => <article key={area}><span>{index + 1}</span><strong>{area}</strong></article>)}</div>
            </section>

            <section className="result-card plan-schedule">
              <div className="section-heading"><span className="eyebrow">7-DAY PLAN</span><h2>把复习拆成可以完成的动作</h2></div>
              <ol className="study-plan">
                {reviewPlan.study_plan.map((item, index) => (
                  <li key={`${item.day}-${item.title}`}>
                    <span className="study-day">DAY {item.day ?? index + 1}</span>
                    <div><h3>{item.title ?? "专项练习"}</h3><div className="tag-list">{item.topics?.map((topic) => <span className="tag" key={topic}>{topic}</span>)}</div><p>{item.outcome}</p></div>
                  </li>
                ))}
              </ol>
            </section>

            <section className="result-card resources-card">
              <div className="section-heading"><span className="eyebrow">RESOURCES</span><h2>推荐材料</h2></div>
              <div className="resource-list">
                {reviewPlan.resources.map((resource) => (
                  <a href={resource.url} target="_blank" rel="noreferrer" key={resource.title}><BookIcon /><span><strong>{resource.title}</strong><small>{resource.type ?? "学习资源"}</small></span><span aria-hidden="true">↗</span></a>
                ))}
              </div>
            </section>

            {session.planMarkdown && <section className="result-card result-card--markdown"><MarkdownText content={session.planMarkdown} /></section>}
          </div>
        ) : (
          <section className="result-card plan-retry-card" role="status">
            <div className="section-heading"><span className="eyebrow">REVIEW PLAN</span><h2>评估已完成，复习计划需要重试</h2></div>
            <p>{planError ?? "复习计划暂时不可用，可以只重试这一阶段，不会重复面试或评估。"}</p>
            <button className="button button--primary" type="button" onClick={() => void retryPlan()} disabled={retryingPlan}>
              <RefreshIcon /> {retryingPlan ? "正在重新生成…" : "重试生成复习计划"}
            </button>
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
