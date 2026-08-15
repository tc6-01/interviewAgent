import { type ChangeEvent, useRef, useState } from "react";
import { FileIcon, LinkIcon, UploadIcon } from "./Icons";

type InputMode = "text" | "file" | "url";

interface SourceInputProps {
  kind: "jd" | "resume";
  title: string;
  eyebrow: string;
  description: string;
  text: string;
  sourceName: string;
  onTextChange: (text: string) => void;
  onParse: (input: { text?: string; url?: string; file?: File }) => Promise<void>;
  busy: boolean;
}

export function SourceInput({
  kind,
  title,
  eyebrow,
  description,
  text,
  sourceName,
  onTextChange,
  onParse,
  busy,
}: SourceInputProps) {
  const [mode, setMode] = useState<InputMode>("text");
  const [url, setUrl] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const inputId = `${kind}-content`;

  const pickFile = (event: ChangeEvent<HTMLInputElement>) => {
    const selected = event.target.files?.[0] ?? null;
    setFile(selected);
  };

  return (
    <section className="source-card" aria-labelledby={`${kind}-title`}>
      <div className="source-card__heading">
        <div>
          <span className="eyebrow">{eyebrow}</span>
          <h2 id={`${kind}-title`}>{title}</h2>
          <p>{description}</p>
        </div>
        {sourceName && <span className="parsed-badge">已解析</span>}
      </div>

      <div className="input-tabs" role="tablist" aria-label={`${title}输入方式`}>
        <button className={mode === "text" ? "is-active" : ""} onClick={() => setMode("text")} role="tab" aria-selected={mode === "text"} type="button">
          <FileIcon /> 粘贴文本
        </button>
        <button className={mode === "file" ? "is-active" : ""} onClick={() => setMode("file")} role="tab" aria-selected={mode === "file"} type="button">
          <UploadIcon /> 上传文件
        </button>
        {kind === "jd" && (
          <button className={mode === "url" ? "is-active" : ""} onClick={() => setMode("url")} role="tab" aria-selected={mode === "url"} type="button">
            <LinkIcon /> 岗位链接
          </button>
        )}
      </div>

      {mode === "text" && (
        <div className="field-stack">
          <label htmlFor={inputId}>{kind === "jd" ? "岗位描述" : "简历内容"}</label>
          <textarea
            id={inputId}
            value={text}
            onChange={(event) => onTextChange(event.target.value)}
            placeholder={kind === "jd" ? "粘贴岗位职责、任职要求、技术栈……" : "粘贴你的经历、项目、技能和教育背景……"}
            rows={10}
          />
          <div className="field-meta">
            <span>{text.length.toLocaleString()} / 50,000 字</span>
            <button className="inline-action" type="button" onClick={() => void onParse({ text })} disabled={busy || !text.trim()}>
              {busy ? "解析中…" : sourceName ? "重新解析" : "解析内容"}
            </button>
          </div>
        </div>
      )}

      {mode === "file" && (
        <div className="upload-zone" onClick={() => inputRef.current?.click()}>
          <input ref={inputRef} type="file" accept=".pdf,.docx,.txt,.md" onChange={pickFile} aria-label={`选择${title}文件`} />
          <span className="upload-zone__icon"><UploadIcon /></span>
          <strong>{file ? file.name : "拖入文件，或点击选择"}</strong>
          <span>PDF、DOCX、TXT、MD · 最大 10 MiB</span>
          <button
            className="button button--secondary button--small"
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              if (file) void onParse({ file });
              else inputRef.current?.click();
            }}
            disabled={busy}
          >
            {busy ? "解析中…" : file ? "解析这个文件" : "选择文件"}
          </button>
        </div>
      )}

      {mode === "url" && kind === "jd" && (
        <div className="field-stack field-stack--url">
          <label htmlFor={`${kind}-url`}>公开岗位链接</label>
          <div className="url-field">
            <LinkIcon />
            <input id={`${kind}-url`} type="url" value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://example.com/jobs/backend-engineer" />
          </div>
          <p className="field-hint">部分招聘网站需要登录或限制抓取，失败时请改用粘贴文本。</p>
          <button className="button button--secondary" type="button" onClick={() => void onParse({ url })} disabled={busy || !url.trim()}>
            {busy ? "抓取中…" : "抓取并解析"}
          </button>
        </div>
      )}
    </section>
  );
}
