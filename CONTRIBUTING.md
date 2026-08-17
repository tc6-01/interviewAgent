# 贡献指南 / Contributing Guide

[中文](#中文) | [English](#english)

## 中文

感谢你对 InterviewAgent 的关注！欢迎任何形式的贡献：报告 Bug、提出新特性建议、完善文档、提交代码。

### 提交 Issue

- **Bug 报告**：请附上复现步骤、期望行为、实际行为、环境信息（OS / Go 版本 / Docker 版本）。
- **特性建议**：说明使用场景和期望的行为即可。

### 开发流程

1. Fork 本仓库并克隆到本地。
2. 创建特性分支：`git checkout -b feat/your-feature`。
3. 开发并确保本地检查通过：

   ```bash
   make check           # build + vet + test
   ```

4. 提交并推送，然后发起 Pull Request，描述清楚改动内容和动机。

### 提交规范

Commit message 使用 [Conventional Commits](https://www.conventionalcommits.org/) 风格：

- `feat(agent): 新增评估维度`
- `fix(rag): 修复 BM25 分词问题`
- `docs: 更新部署文档`

### 代码风格

- Go 代码使用 `gofmt` 格式化，遵循标准 Go 项目惯例。
- 默认服务必须保持 `httpapi → session → workflow → core/agent → domain interfaces` 的单向业务依赖；`core/graph` 只负责内向装配检查，基础设施实现只在 `internal/bootstrap` 装配。
- 默认启动路径不得依赖 Redis、MySQL、Milvus 或 Embedding 服务。
- 前端代码（`interview-agent-web/`）遵循目录内既有的组件与命名风格。
- 新增功能请尽量附带测试。

### 分支与 CI

- 主分支为 `master`，所有改动通过 PR 合入。
- CI（GitHub Actions）会自动运行编译、`go vet`、测试与 Docker 构建，需全部通过。

## English

Thanks for your interest in InterviewAgent! Contributions of all kinds are welcome: bug reports, feature requests, documentation improvements, and code.

### Filing Issues

- **Bug reports**: include reproduction steps, expected vs. actual behavior, and environment info (OS / Go version / Docker version).
- **Feature requests**: describe the use case and desired behavior.

### Development Workflow

1. Fork the repository and clone it locally.
2. Create a feature branch: `git checkout -b feat/your-feature`.
3. Make your changes and ensure local checks pass:

   ```bash
   make check           # build + vet + test
   ```

4. Commit, push, and open a Pull Request describing what changed and why.

### Commit Convention

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat(agent): add new evaluation dimension`
- `fix(rag): fix BM25 tokenization`
- `docs: update deployment guide`

### Code Style

- Go code must be `gofmt`-formatted and follow standard Go project conventions.
- Keep the default business dependency direction `httpapi → session → workflow → core/agent → domain interfaces`; `core/graph` remains an inward-facing assembly check, and infrastructure is wired only in `internal/bootstrap`.
- Do not add Redis, MySQL, Milvus, or embedding services to the default startup path.
- Frontend code (`interview-agent-web/`) should follow the existing component and naming style.
- Please add tests for new functionality where practical.

### Branches & CI

- The main branch is `master`; all changes land via Pull Requests.
- CI (GitHub Actions) runs build, `go vet`, tests, and the Docker build — all must pass.
