# InterviewAgent Web

InterviewAgent 的 React + TypeScript 前端，覆盖完整模拟面试流程：

1. 输入或上传 JD / 简历
2. 确认面试岗位、级别、重点与题量
3. 通过 HTTP 提交回答，通过 SSE 接收阶段、流式问题、评分与终态
4. 查看评估报告、复习计划并导出 Markdown

## 本地启动

需要 Node.js 20.19+ 或 22.12+。

```bash
npm install
npm run dev
```

开发服务器默认监听 <http://localhost:5173>，并把 `/api`、`/healthz`、`/readyz` 代理到 <http://localhost:9090>。

## 数据模式

默认使用内置 Mock，可在没有后端和密钥的情况下体验完整流程：

```bash
cp .env.example .env
npm run dev
```

连接符合 INT-4 v2.1 契约的真实后端：

```bash
VITE_API_MODE=real npm run dev
```

- `VITE_API_BASE_URL` 留空：同源部署或使用 Vite 开发代理（推荐）。
- `VITE_API_BASE_URL=https://api.example.com`：跨源 JWT 部署；匿名 Cookie 模式不支持跨源。
- JWT 如需使用，写入浏览器的 `interview-agent-token` localStorage key；令牌不会放进 SSE URL。

## 验证与构建

```bash
npm run typecheck
npm run test:run
npm run build
```

构建产物位于 `dist/`。Vite 使用相对资源路径和 Hash 路由，因此 `dist/` 可直接发布到 GitHub Pages 的仓库子路径：

```bash
VITE_API_MODE=mock npm run build
```

生产同源部署时使用 `VITE_API_MODE=real` 构建，再由 Go 服务嵌入并提供 `dist/` 静态文件。

## 目录

```text
src/
├── api/          HTTP 客户端、SSE 解析器与 Mock 服务
├── app/          Hash 路由和应用入口
├── components/   可复用交互与展示组件
├── lib/          面试方向预览等纯函数
├── pages/        资料、面试、结果页面
├── store/        会话状态、SSE 事件归并和本地恢复
└── types/        API v2.1 与 UI 类型
```

## SSE 恢复策略

- 使用 `fetch` + `ReadableStream`，允许设置 `Authorization` 和 `Last-Event-ID`。
- 页面恢复时先请求 `GET /interviews/{id}`，再从快照的 `last_event_id` 订阅事件。
- `question_delta` 只用于即时渲染；完整 `question` 事件是最终权威文本。
- 未识别的事件和字段会被忽略，避免服务端扩展破坏旧前端。
