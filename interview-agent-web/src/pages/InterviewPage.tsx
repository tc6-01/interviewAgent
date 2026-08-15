import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { navigate } from "../app/router";
import { AppShell } from "../components/AppShell";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ClockIcon, SendIcon, SignalIcon, SparkIcon } from "../components/Icons";
import { StageTimeline } from "../components/StageTimeline";
import { useAppStore } from "../store/app-store";

export function InterviewPage({ interviewId }: { interviewId: string }) {
  const { state, dispatch } = useAppStore();
  const session = state.sessions[interviewId];
  const [answer, setAnswer] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [loading, setLoading] = useState(!session);
  const [pageError, setPageError] = useState<string | null>(null);
  const [confirmQuit, setConfirmQuit] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    let completionTimer: number | undefined;

    const connect = async () => {
      try {
        let lastEventId = session?.lastEventId ?? "0";
        if (!session || api.mode === "real") {
          const snapshot = await api.getInterview(interviewId);
          lastEventId = snapshot.last_event_id;
          dispatch({ type: "hydrate_session", snapshot });
          if (snapshot.status === "completed") {
            navigate(`/results/${encodeURIComponent(interviewId)}`);
            return;
          }
        }
        setLoading(false);
        await api.subscribe(interviewId, {
          lastEventId,
          signal: controller.signal,
          onConnectionChange: (connected) =>
            dispatch({ type: "patch_session", id: interviewId, patch: { connected } }),
          onMessage: (message) => {
            dispatch({ type: "event", id: interviewId, message });
            if (message.event === "completed") {
              completionTimer = window.setTimeout(
                () => navigate(`/results/${encodeURIComponent(interviewId)}`),
                900,
              );
            }
          },
        });
      } catch (error) {
        if (!controller.signal.aborted) {
          setLoading(false);
          setPageError(error instanceof Error ? error.message : "无法恢复面试会话");
        }
      }
    };
    void connect();
    return () => {
      controller.abort();
      if (completionTimer) window.clearTimeout(completionTimer);
    };
    // The connection is intentionally scoped to the route id, not store updates.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [interviewId, dispatch]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [session?.messages.length, session?.streamingQuestion]);

  const submit = async () => {
    if (!session?.awaitingAnswer || !answer.trim()) return;
    const promptId = session.awaitingAnswer.prompt_id;
    const content = answer.trim();
    setSubmitting(true);
    setPageError(null);
    try {
      await api.submitAnswer(interviewId, promptId, content);
      dispatch({ type: "candidate_message", id: interviewId, promptId, content });
      setAnswer("");
    } catch (error) {
      setPageError(error instanceof Error ? error.message : "提交回答失败");
    } finally {
      setSubmitting(false);
    }
  };

  const quit = async () => {
    setConfirmQuit(false);
    try {
      await api.quitInterview(interviewId);
    } catch (error) {
      setPageError(error instanceof Error ? error.message : "结束面试失败");
    }
  };

  if (loading) {
    return <AppShell compact><main className="center-state"><span className="loader-orbit"><SparkIcon /></span><h1>正在恢复面试</h1><p>同步会话快照与实时事件…</p></main></AppShell>;
  }

  if (!session) {
    return <AppShell compact><main className="center-state"><h1>没有找到这场面试</h1><p>{pageError ?? "演示会话刷新后不会保留，请重新开始。"}</p><a className="button button--primary" href="#/setup">返回首页</a></main></AppShell>;
  }

  const waitingForAnswer = Boolean(session.awaitingAnswer);
  const progress = session.total > 0 ? Math.min(100, (session.answered / session.total) * 100) : 0;

  return (
    <AppShell
      compact
      action={<button className="button button--ghost button--small" type="button" onClick={() => setConfirmQuit(true)}>结束面试</button>}
    >
      <main className="interview-layout">
        <aside className="interview-sidebar">
          <div className="session-kicker"><span>LIVE SESSION</span><span className={session.connected ? "connection is-online" : "connection"}><SignalIcon /> {session.connected ? "实时连接" : "正在重连"}</span></div>
          <h1>{session.focus?.position ?? "模拟面试"}</h1>
          <p>{session.focus?.level} · {session.total} 个核心问题</p>

          <div className="interview-progress">
            <div><span>完成进度</span><strong>{session.answered} / {session.total}</strong></div>
            <div className="progress-track"><span style={{ width: `${progress}%` }} /></div>
          </div>

          <StageTimeline stage={session.stage} />

          <div className="sidebar-note">
            <ClockIcon />
            <p><strong>像真实面试一样思考</strong><span>不必追求标准答案，清楚说明你的判断和取舍。</span></p>
          </div>
        </aside>

        <section className="conversation-panel" aria-label="面试对话">
          <header className="conversation-header">
            <div>
              <span className="eyebrow">AI INTERVIEWER</span>
              <h2>{session.stageMessage}</h2>
            </div>
            <span className={`status-pill status-pill--${session.status}`}>{statusLabel(session.status)}</span>
          </header>

          <div className="message-list" aria-live="polite">
            {(pageError || session.error) && <div className="alert alert--error" role="alert">{pageError ?? session.error}</div>}
            {session.warning && <div className="alert alert--warning" role="status">{session.warning}</div>}
            {session.messages.map((message) => (
              <article className={`message message--${message.role}`} key={message.id}>
                <div className="message-avatar">{message.role === "candidate" ? "你" : message.role === "feedback" ? "✓" : <SparkIcon />}</div>
                <div className="message-body">
                  <span className="message-label">{message.role === "candidate" ? "你的回答" : message.role === "feedback" ? `单题反馈 · ${message.score?.score ?? "—"} 分` : message.role === "system" ? "准备中" : `面试官${message.questionNo ? ` · 第 ${message.questionNo} 题` : ""}`}</span>
                  <p>{message.content}</p>
                  {message.score && (
                    <div className="feedback-points">
                      {message.score.key_points_hit?.map((point) => <span className="point point--hit" key={point}>✓ {point}</span>)}
                      {message.score.key_points_missed?.map((point) => <span className="point point--miss" key={point}>补充：{point}</span>)}
                    </div>
                  )}
                </div>
              </article>
            ))}

            {session.streamingQuestion && (
              <article className="message message--interviewer message--streaming">
                <div className="message-avatar"><SparkIcon /></div>
                <div className="message-body"><span className="message-label">面试官正在提问</span><p>{session.streamingQuestion}<span className="typing-caret" /></p></div>
              </article>
            )}

            {!waitingForAnswer && !session.streamingQuestion && !["completed", "terminated", "failed"].includes(session.status) && (
              <div className="thinking-line"><span /><span /><span /> {session.stageMessage}</div>
            )}
            <div ref={bottomRef} />
          </div>

          <div className="answer-composer">
            <label htmlFor="answer">你的回答</label>
            <textarea
              id="answer"
              value={answer}
              onChange={(event) => setAnswer(event.target.value)}
              onKeyDown={(event) => {
                if ((event.metaKey || event.ctrlKey) && event.key === "Enter") void submit();
              }}
              placeholder={waitingForAnswer ? "写下你的思路。可以先讲结论，再说明依据、行动和结果……" : "等待面试官完成当前步骤…"}
              disabled={!waitingForAnswer || submitting}
              rows={4}
            />
            <div className="composer-footer">
              <span>{waitingForAnswer ? "⌘ / Ctrl + Enter 发送" : "收到完整问题后即可作答"}</span>
              <button className="button button--primary" type="button" onClick={() => void submit()} disabled={!waitingForAnswer || !answer.trim() || submitting}>
                {submitting ? "提交中…" : "提交回答"} {!submitting && <SendIcon />}
              </button>
            </div>
          </div>
        </section>
      </main>

      <ConfirmDialog
        open={confirmQuit}
        title="确定提前结束吗？"
        description={session.answered > 0 ? "系统会基于已经完成的回答生成评估与复习计划。" : "你还没有完成任何问题，本次会话将直接结束且不会生成报告。"}
        confirmLabel={session.answered > 0 ? "结束并生成报告" : "结束面试"}
        onConfirm={() => void quit()}
        onCancel={() => setConfirmQuit(false)}
      />
    </AppShell>
  );
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    preparing: "准备中",
    interviewing: "面试中",
    evaluating: "评估中",
    planning_review: "生成计划",
    completed: "已完成",
    terminated: "已结束",
    failed: "已中断",
  };
  return labels[status] ?? status;
}
