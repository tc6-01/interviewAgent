import type { PropsWithChildren, ReactNode } from "react";
import { api } from "../api/client";
import { Brand } from "./Brand";

interface AppShellProps extends PropsWithChildren {
  action?: ReactNode;
  compact?: boolean;
}

export function AppShell({ children, action, compact = false }: AppShellProps) {
  return (
    <div className={compact ? "app-shell app-shell--compact" : "app-shell"}>
      <header className="topbar">
        <Brand />
        <div className="topbar-actions">
          {api.mode === "mock" && <span className="mode-pill">演示模式</span>}
          {action}
          <a className="text-link text-link--quiet" href="https://github.com" target="_blank" rel="noreferrer">
            开源项目 <span aria-hidden="true">↗</span>
          </a>
        </div>
      </header>
      {children}
    </div>
  );
}
