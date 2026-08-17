function renderInline(text: string) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`)/g);
  return parts.map((part, index) => {
    if (part.startsWith("**") && part.endsWith("**")) return <strong key={index}>{part.slice(2, -2)}</strong>;
    if (part.startsWith("`") && part.endsWith("`")) return <code key={index}>{part.slice(1, -1)}</code>;
    return part;
  });
}

export function MarkdownText({ content }: { content: string }) {
  const blocks = content.split(/\n{2,}/).filter(Boolean);
  return (
    <div className="markdown-text">
      {blocks.map((block, index) => {
        if (block.startsWith("### ")) return <h3 key={index}>{renderInline(block.slice(4))}</h3>;
        if (block.startsWith("## ")) return <h2 key={index}>{renderInline(block.slice(3))}</h2>;
        if (block.startsWith("# ")) return <h1 key={index}>{renderInline(block.slice(2))}</h1>;
        const lines = block.split("\n");
        if (lines.every((line) => /^[-*] /.test(line))) {
          return <ul key={index}>{lines.map((line) => <li key={line}>{renderInline(line.slice(2))}</li>)}</ul>;
        }
        return <p key={index}>{lines.map((line, lineIndex) => <span key={lineIndex}>{renderInline(line)}{lineIndex < lines.length - 1 && <br />}</span>)}</p>;
      })}
    </div>
  );
}
