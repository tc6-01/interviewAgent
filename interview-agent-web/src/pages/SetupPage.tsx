import { useMemo, useState } from "react";
import { api } from "../api/client";
import { navigate } from "../app/router";
import { AppShell } from "../components/AppShell";
import { FocusReview } from "../components/FocusReview";
import { ArrowLeftIcon, ArrowRightIcon, SparkIcon } from "../components/Icons";
import { ProgressSteps } from "../components/ProgressSteps";
import { SourceInput } from "../components/SourceInput";
import { deriveInterviewFocus } from "../lib/focus";
import { useAppStore } from "../store/app-store";
import { INTERVIEW_QUESTION_COUNT } from "../config/interview";

export function SetupPage() {
  const { state, dispatch } = useAppStore();
  const { setup } = state;
  const [busyKind, setBusyKind] = useState<"jd" | "resume" | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ready = Boolean(setup.jdText.trim() && setup.resumeText.trim());

  const completionHint = useMemo(() => {
    if (!setup.jdText.trim() && !setup.resumeText.trim()) return "先准备岗位描述与简历，通常只需 2 分钟。";
    if (!setup.jdText.trim()) return "还差岗位描述。";
    if (!setup.resumeText.trim()) return "还差简历内容。";
    return "资料齐全，可以生成面试方向。";
  }, [setup.jdText, setup.resumeText]);

  const parse = async (
    kind: "jd" | "resume",
    input: { text?: string; url?: string; file?: File },
  ) => {
    setBusyKind(kind);
    setError(null);
    try {
      const parsed = await api.parseDocument({ kind, ...input });
      dispatch({
        type: "patch_setup",
        patch:
          kind === "jd"
            ? { jdText: parsed.text, jdSourceName: parsed.source.name ?? parsed.source.url ?? "已粘贴文本", focus: null }
            : { resumeText: parsed.text, resumeSourceName: parsed.source.name ?? "已粘贴文本", focus: null },
      });
    } catch (parseError) {
      setError(parseError instanceof Error ? parseError.message : "解析失败，请重试");
    } finally {
      setBusyKind(null);
    }
  };

  const review = async () => {
    if (!ready) return;
    setError(null);
    try {
      if (!setup.jdSourceName) await parse("jd", { text: setup.jdText });
      if (!setup.resumeSourceName) await parse("resume", { text: setup.resumeText });
      dispatch({ type: "patch_setup", patch: { focus: deriveInterviewFocus(setup.jdText, setup.resumeText) } });
      window.scrollTo({ top: 0, behavior: "smooth" });
    } catch {
      // parse() already provides a user-facing error.
    }
  };

  const startInterview = async () => {
    if (!setup.focus) return;
    setStarting(true);
    setError(null);
    try {
      const response = await api.createInterview({
        jd_text: setup.jdText,
        resume_text: setup.resumeText,
        options: { question_count: INTERVIEW_QUESTION_COUNT },
      });
      dispatch({
        type: "create_session",
        response,
        focus: setup.focus,
        total: INTERVIEW_QUESTION_COUNT,
      });
      navigate(`/interview/${encodeURIComponent(response.interview_id)}`);
    } catch (startError) {
      setError(startError instanceof Error ? startError.message : "创建面试失败，请重试");
    } finally {
      setStarting(false);
    }
  };

  return (
    <AppShell>
      <main className="setup-page">
        <ProgressSteps current={setup.focus ? 2 : 1} />

        {!setup.focus ? (
          <>
            <section className="setup-hero">
              <span className="eyebrow"><SparkIcon /> PERSONALIZED INTERVIEW</span>
              <h1>让每一道问题，<br /><em>都与你有关。</em></h1>
              <p>提供岗位描述和简历。InterviewAgent 会梳理匹配方向，生成问题，并陪你完成一场可复盘的模拟面试。</p>
              <span className="hero-note">{completionHint}</span>
            </section>

            {error && <div className="alert alert--error" role="alert">{error}</div>}

            <div className="source-grid">
              <SourceInput
                kind="jd"
                eyebrow="01 · ROLE"
                title="目标岗位"
                description="告诉面试官，这个岗位真正关心什么。"
                text={setup.jdText}
                sourceName={setup.jdSourceName}
                onTextChange={(jdText) => dispatch({ type: "patch_setup", patch: { jdText, jdSourceName: "", focus: null } })}
                onParse={(input) => parse("jd", input)}
                busy={busyKind === "jd"}
              />
              <SourceInput
                kind="resume"
                eyebrow="02 · YOU"
                title="你的简历"
                description="让问题贴近你的经历，而不是泛泛而谈。"
                text={setup.resumeText}
                sourceName={setup.resumeSourceName}
                onTextChange={(resumeText) => dispatch({ type: "patch_setup", patch: { resumeText, resumeSourceName: "", focus: null } })}
                onParse={(input) => parse("resume", input)}
                busy={busyKind === "resume"}
              />
            </div>

            <div className="setup-cta">
              <div><span>下一步</span><strong>确认岗位、级别与面试重点</strong></div>
              <button className="button button--primary button--large" type="button" onClick={() => void review()} disabled={!ready || busyKind !== null}>
                生成面试方向 <ArrowRightIcon />
              </button>
            </div>
          </>
        ) : (
          <>
            <section className="review-hero">
              <button className="back-link" type="button" onClick={() => dispatch({ type: "patch_setup", patch: { focus: null } })}><ArrowLeftIcon /> 返回修改资料</button>
              <span className="eyebrow">方向确认</span>
              <h1>准备好了，<em>就开始。</em></h1>
              <p>本轮固定 15 题。最终分析会在开始后通过实时事件逐步展示。</p>
            </section>
            {error && <div className="alert alert--error" role="alert">{error}</div>}
            <FocusReview focus={setup.focus} />
            <div className="setup-cta setup-cta--review">
              <div><span>预计用时</span><strong>45–75 分钟</strong></div>
              <button className="button button--primary button--large" type="button" onClick={() => void startInterview()} disabled={starting}>
                {starting ? "正在创建面试…" : "开始模拟面试"} {!starting && <ArrowRightIcon />}
              </button>
            </div>
          </>
        )}
      </main>
      <footer className="site-footer"><span>InterviewAgent · Open source mock interview</span><span>你的资料仅用于本次面试流程</span></footer>
    </AppShell>
  );
}
