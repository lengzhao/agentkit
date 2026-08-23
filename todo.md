# AgentKit TODO

基于代码、`docs/` 设计文档，以及与 [Pi](../../pi) 的功能对比整理。

**优先级**：P0 核心契约（已完成）→ P1 对齐 Pi 最小可用 → P2 长会话与集成 → P3 产品化 → P4 工程与清理

**对照基准**：[docs/reference-analysis.zh.md](docs/reference-analysis.zh.md) · [docs/plugin-catalog.zh.md](docs/plugin-catalog.zh.md)

---

## 状态总览

| 维度 | AgentKit 现状 | Pi 参考 |
|---|---|---|
| Spine（Runner/Loop/Agent/Session/Tools/LLM） | ✅ MVP 可运行 | ✅ |
| 核心工具 read/write/edit/bash/grep/find/ls/skill | ✅ | ✅ |
| Compaction + Skills + AGENTS.md | ✅（含 overflow 恢复） | ✅ |
| Tool 执行管线（Policy/Approval/Hook/超时） | ✅ | ✅ |
| Steering / Follow-up 双队列 | ✅ Loop API + 测试 | ✅ + TUI 快捷键 |
| Slash 命令 | ⚠️ 框架 + `/compact` `/new` `/session` | ✅ 20+ 内置 |
| LLM 双层重试 | ✅ Provider + Agent `retry/*` | ✅ `settings.retry` + `auto_retry_*` |
| LLM Provider | ⚠️ `go-openai` chat/responses | ✅ `pi-ai` 30+ Provider |
| Session 树 / fork | ❌ 线性 JSONL | ✅ JSONL v3 树 |
| 运行时换模型 / Thinking level | ❌ | ✅ `/model` `/settings` |
| 并行 tool 执行 | ❌ 串行 | ✅ preflight 串行 + body 并发 |
| Extension / Hook 面 | ⚠️ 3 个 hook 点 | ✅ 40+ Extension 事件 |
| TUI / RPC / SDK | ❌ 简易 REPL | ✅ 四模式 |
| 运行时扩展 | ❌ Go 编译期插件 | ✅ TS Extension `/reload` |
| pluginkit Manager + multiplex | ✅ Pi 无 | — |

### AgentKit 独有（不必追 Pi）

- `pluginkit` 静态装配 + 类型安全 Deps 注入
- `cmd/agent -manager` Web 工作台（装配树 / 试装配 / 写回）
- `platform/multiplex` 多 Platform 复用 Runner

### 建议补齐顺序（对齐 Pi 最小可用）

1. ~~Overflow compact-and-retry~~（已完成）
2. 并行 tool + 文件变更串行队列
3. `/model` + settings 热更新（不必一次上 30 Provider）
4. CLI steer/follow-up 交互（Loop 已有 API，补 Platform）
5. Session 树 + `/fork` `/tree`
6. `hook/llm-request` + compaction summary 重试链

---

## 已完成（Phase 1 + P0 核心契约）

<details>
<summary>展开查看已完成项</summary>

### 工具执行管道

```
可见性 → Policy → Approval → OnBeforeTool → 超时/取消 → body → OnAfterTool → Session
```

- [x] `HookRuntime` 注入 `tools.Runtime`
- [x] `OnBeforeTool` / `OnAfterTool` hook 调用
- [x] `DefaultTimeoutSeconds` 与 per-tool timeout
- [x] `OnAfterTool` 执行后截断大结果（prune 在 derive 阶段）

### Session 生命周期事件

- [x] `turn/start`、`turn/end`、`step/start`、`step/end`
- [x] 与 `user/message`、`assistant/message`、`tool/call`、`tool/result` 顺序一致

### 凭据与配置安全

- [x] LLM 经 `credentials.Store` 解析 `apiKeyRef`
- [x] `presets/coding.yaml` + `config.example.yaml`，`config.yaml` 在 `.gitignore`

### FS 边界

- [x] `fs/readonly` 包装 + `presets/coding.yaml` 统一 workspace 策略（见 `docs/coding-workspace.zh.md`）

### 配置与装配

- [x] `manager.FromYAML` + `Document.ToGraph()` + `build.Build`
- [x] `cmd/agent -manager` 工作台（装配树 / 试装配 / 写回）
- [x] `go generate` → `plugins/all.go` 自动 import

### Platform 拆分

- [x] `platform/cli`、`platform/multiplex`
- [x] `session/store`（多 SessionID 目录，IM 场景）

### Phase 2 部分提前落地

- [x] `compaction/summary` + `compaction/prune-tool-results` + `hook/before-step`
- [x] `skill/filesystem` + `tool/skill` + `prompt/section/skills`
- [x] `settings/file` 插件（未接入主流程）

### LLM 层（近期）

- [x] 迁移至 `github.com/sashabaranov/go-openai`
- [x] 双 API 模式：`chat`（默认）/ `responses`
- [x] `ContentPart` 多模态字段（`url` / `mime` / `detail`）；LLM convert 已支持 image
- [x] Chat `reasoning_content` → `thinking_*` 事件
- [x] Provider 层重试（`llm.config.retry.provider`，默认 `maxRetries: 0`）
- [x] Agent 层自动重试（`agent.config.retry` + `retry/start` `retry/end` 事件）

</details>

---

## P1 — 对齐 Pi 最小可用

> 目标：不追求 TUI，但 Coding Agent 日常可用，工具集与队列语义与 Pi 默认集对齐。

### 1.1 补全内置工具（Pi 默认 7 工具）

- [x] `tool/grep` — 内容搜索
- [x] `tool/find` — 文件查找
- [x] `tool/list-dir` — 目录列表（Pi `ls`）
- [x] 更新 `presets/coding.yaml` 默认启用上述工具

### 1.2 Steering / Follow-up 双队列

Pi 核心 UX：`steer()` 中断当前 turn、`followUp()` turn 结束后排队。

- [x] `RunTurn` turn 结束后消费 `followUps` 并继续调度（对齐 Pi `followUpMode`）
- [x] Loop 暴露 `Steer` / `FollowUp` 入口（为 RPC 预留）
- [x] 支持 `all` / `one-at-a-time` 模式
- [ ] CLI 交互：agent 工作时排队消息（Pi：Enter=steer、Alt+Enter=follow-up）
- [ ] CLI 取消与队列回退（Pi：Escape 中止并恢复队列到编辑器）

### 1.3 Slash 命令（`command/*` 插件层）

Pi 高频命令优先；无 TUI 时走 stderr 文本交互。

- [x] `command/registry` — 注册与分发框架
- [x] `/compact` — 手动触发 compaction（`hook/before-step` 贡献）
- [x] `/new` — 新 Session
- [x] `/session` — 显示 session id、路径、消息数
- [x] `platform/cli` 接入 command registry（替代硬编码 switch）
- [ ] `/model` — 运行时切换模型
- [ ] `/settings` — thinking level、重试开关等（对齐 `.pi/settings.json` 子集）
- [ ] `/resume` — 浏览并恢复历史 Session
- [ ] `/export` `/import` — Session JSONL 导入导出（HTML 可后置）
- [ ] `/name` — Session 显示名
- [ ] Prompt Templates（`{{变量}}` + `/templatename`）
- [ ] `/login` `/logout` — OAuth / API Key（订阅类 Provider）
- [ ] `/reload` — 重载 skills / prompt sections（Go 无 jiti，可用配置热加载替代）
- [ ] `/copy` — 复制上条 assistant 消息
- [ ] `/trust` — 项目信任决策（对齐 Pi Project Trust）

### 1.4 Agent 执行增强

- [ ] 并行 tool 执行（Pi 默认 `parallel`：preflight 串行，body 并发）
- [ ] `shouldStopAfterTurn` / `OnTurnStopping` hook（`hook/turn-stopping`）
  - 注：落地时定义 `TurnStoppingHook` / `TurnState` / `StopDecision`
- [ ] 文件变更串行队列（避免并发 write/edit 竞态，参考 Pi `file-mutation-queue`）

### 1.5 模型与 Provider

- [ ] 第二 LLM Provider（`llm/anthropic` 或 openai-compatible 多 `baseUrl` 实例）
- [ ] 运行时切换 model（`/model` 命令或 settings 热更新，不必一次上 30 Provider）
- [ ] Thinking level 配置入口（LLM 已可发 `thinking_*`，缺 settings / 命令）
- [ ] Usage / cost 统计与展示（Pi `/session` 显示 token；类型已有 `Usage`，未接线）

### 1.6 LLM 稳定性（Pi 双层 retry 对齐）

- [x] 错误分类器（`llm.IsRetryableError`，排除 quota / context overflow）
- [x] Agent turn 重试 + 指数退避 + `retry/start` `retry/end`
- [x] Provider HTTP 重试包装（`streamWithProviderRetry`）
- [x] Overflow compact-and-retry（Pi：删失败 assistant → compact → 重试 turn **最多 1 次**）
- [x] Compaction / branch summary 独立重试链（`summarization/retry/*` + `compaction.RetryCall`）
- [ ] CLI 重试状态展示（Pi TUI：「Retrying 2/3…」；事件已有，CLI 未渲染）
- [ ] `hook/llm-request` — 请求改写（Pi `before_provider_request`）
- [ ] Provider headers hook（Pi `before_provider_headers` / `after_provider_response`）

### 1.7 CLI / Platform 体验

- [x] 流式 `text_delta` / `thinking_delta` / `toolcall_*` stderr 输出
- [ ] 渲染 `retry/start` `retry/end` outbound 事件
- [ ] 多行输入 / 图片粘贴（多模态 Platform → LLM 端到端）
- [ ] `--once` 之外的非交互 flag 对齐（`--model`、`--session`、`--fork` 等）

---

## P2 — 长会话与集成

> 目标：长会话可管理、可嵌入、可过滤工具，对齐 Pi Phase 2 能力。

### 2.1 Session 树与分支

Pi JSONL v3：`parentId` 树、`/tree` `/fork` `/clone`。

- [ ] Session 事件增加 `parentId` / 树形导航 API
- [ ] `command/tree` — 分支切换（CLI 文本版即可）
- [ ] `command/fork` — 从历史 user 消息 fork 新 Session
- [ ] `command/clone` — 复制当前分支到新 Session 文件
- [ ] 分支摘要（Pi `/tree` 放弃分支时可 summarization）
- [ ] Session 导出 HTML（JSONL 优先）

### 2.2 Tool Scope / 可见性

文档 §8；当前 `Visible()` 返回全部工具。

- [ ] 按 `ToolScope`（global / preset / agent / turn）过滤
- [ ] restriction 只减不增；更小 scope 可 shadow 同名工具
- [ ] CLI `--tools` 或配置级工具白名单

### 2.3 Loop 增强

- [ ] Turn 级 Session 事件与 Agent 职责文档化（或上移到 Loop）
- [ ] 多 Agent 路由（AgentID、slash command、未来 AgentSet）
- [ ] `LoopResult` 有意义地返回（当前被丢弃）

### 2.4 Platform 扩展

- [ ] `platform/rpc` — JSONL stdin/stdout（对齐 Pi RPC 模式）
- [ ] `platform/http` — HTTP/WebSocket API
- [ ] `platform/slack` / `platform/feishu` — IM 接入（配合 `multiplex`）

### 2.5 Session 持久化进阶

- [ ] `session/sqlite` — 索引与检索
- [ ] `tool/session-query` + `SessionQuery` 接口落地
- [ ] `settings/file` 接入主配置（模型默认、工具开关，对齐 `.pi/settings.json`）

### 2.6 可观测性

- [ ] `telemetry/otel` — 用量与 trace
- [ ] 统一 slog 字段：`session_id`、`agent_id`、`tool_call_id`、`plugin_kind`

### 2.7 Extension / Hook 面（Pi 有、AgentKit 缺）

Pi `pi.on()` 事件；AgentKit 仅 `before-step` / `before-tool` / `after-tool`。

| Pi Extension 事件 | AgentKit 映射 | 优先级 |
|---|---|---|
| `before_provider_request` | `hook/llm-request` | P1 |
| `before_provider_headers` | LLM Runtime | P2 |
| `after_provider_response` | LLM Runtime | P2 |
| `input` | Platform 拦截用户输入 | P2 |
| `tool_call` | 已有 Policy；缺 Extension 式 block/改写 | P2 |
| `message_end` | 可替换最终 assistant 消息 | P3 |
| `session_before_fork` / `session_tree` | Session 树插件 | P2 |
| `session_before_compact` | compaction hook（部分由 `hook/before-step` 覆盖） | P1 |
| `model_select` / `thinking_level_select` | `/model` + settings | P1 |
| `agent_settled` | turn 完全结束通知 | P2 |
| `user_bash` | 用户手动 bash 记录到 Session | P3 |

---

## P3 — 高级编排与产品化

> 目标：Subagent、沙箱、Harness 多 Lane；按需推进。

### 3.1 AgentHarness（Pi `packages/agent`）

- [ ] `loop/harness` — 多 Lane + operation state machine
- [ ] crash recovery / checkpoint
- [ ] usage ledger

### 3.2 能力层（Phase 3 plugin-catalog）

- [ ] `subagent/inprocess` + `tool/subagent`
- [ ] `web/http-fetch` + `tool/web-fetch` + `tool/web-search`
- [ ] `sandbox/landlock` + `fs/sandbox` + `process/sandbox`
- [ ] `policy/plan-mode`、`policy/path-denylist`
- [ ] `tool/ask-user` — 向用户提问（DSH `tool-ask-user`）

### 3.3 交互产品化（Pi 最大差距）

- [ ] TUI 层（自建或接入现有库）：流式渲染、diff、Markdown、主题
- [ ] Prompt Templates（`{{变量}}` + `/templatename`）
- [ ] 图片输入端到端（`ContentPart.URL` 已有；Platform 剪贴板/附件未接）
- [ ] OAuth `/login` 流程（订阅类 Provider：Copilot、ChatGPT 等）
- [ ] Session 分享（Pi `/share` → GitHub gist + HTML）
- [ ] 模型目录与能力表（Pi `models.json` + thinking capabilities）

### 3.4 配置高层语法

- [ ] Preset / Feature / AgentSet → 展开为 root graph（架构文档 §5.7）
- [ ] 消除 flat YAML 重复（model 单一来源、YAML anchor、共用 `llm` 实例）
- [ ] `config resolve` CLI（引入 Feature 合并时需要）

---

## P4 — 工程、测试与清理

### 4.1 诊断 CLI

- [ ] `plugins list` — `pluginkit.Lookup`
- [ ] `config graph` — 实例 id、kind、deps
- [ ] `build dry-run` — 类型检查不启动 Runner
- [ ] `session replay <id>` — 重放模型上下文
- [ ] `hooks list` — 已装配 hook 顺序

### 4.2 测试金字塔

- [ ] Build graph test — unknown kind、重复 tool name、deps 类型错误
- [ ] Tool pipeline test — deny / ask / allow + approval/auto-deny
- [ ] Session golden test — `DeriveMessages` 快照
- [ ] Preset smoke test — `presets/coding-smoke.yaml`
- [ ] Import coverage test — 配置引用的 kind 已注册
- [ ] LLM retry 集成测试（Provider mock 429 + Agent turn 重试）
- [ ] Overflow compact-and-retry 回归测试

### 4.3 StartStop 生命周期

- [ ] `build.Build` 后收集 `StartStop` 组件（落地时定义接口）
- [ ] 按依赖顺序 `Start`；失败反向 `Stop`（带超时）

### 4.4 JSON Schema 生成

- [x] 反射 `json` + `jsonschema` tag，或 `invopop/jsonschema`
- [x] 移除 `tool_builder.go` type switch 硬编码

### 4.5 文档与结构

- [ ] 架构文档 §5.2：Policy 挂在 `tools/runtime`（示例同步）
- [ ] plugin-catalog / README 标注「已实现 / 规划中」
- [ ] `cap/*` 空壳：删除或标注 Phase，避免误以为已实现
- [ ] （可选）根目录 `plugin_*.go` 合并为 `interfaces.go` / `doc.go`
- [ ] 同步本文与代码：Follow-up、LLM retry、go-openai 等已完成项

---

## 路线图

```mermaid
flowchart LR
  subgraph done ["已完成"]
    A1["Phase 1 Spine + 核心工具"]
    A2["P0 管线 / Session 事件 / 凭据"]
    A3["Compaction + Skills + Manager"]
    A4["LLM go-openai + 双层 retry"]
  end

  subgraph p1 ["P1 对齐 Pi 最小可用"]
    B1["overflow compact-retry"]
    B2["并行 tool + 文件队列"]
    B3["/model + settings"]
    B4["CLI steer/follow-up"]
    B5["hook/llm-request"]
  end

  subgraph p2 ["P2 长会话与集成"]
    C1["Session 树 / fork"]
    C2["Tool scope"]
    C3["platform/rpc"]
    C4["Extension hooks"]
  end

  subgraph p3 ["P3 产品化"]
    D1["Harness 多 Lane"]
    D2["Subagent / Sandbox / Web"]
    D3["TUI / OAuth"]
  end

  done --> p1 --> p2 --> p3
```

---

## 不必做

- 自研 plugin kernel（已用 pluginkit）
- 服务容器 / 请求路径 `Use(Key)` 定位
- 运行时扫描 `.go` 热加载（Go 无 jiti；用 Preset + import 生成器替代 Pi Extension）
- 一次实现全部 `cap/*` 空接口
- 复刻 Pi 完整 TUI 再推进 Harness 核心（可并行，但不阻塞 P1）
- 一次实现 30+ LLM Provider（openai-compatible 多 `baseUrl` 可覆盖大部分场景）
- 复刻 Pi npm/git 包生态（`pi install npm:...`）

---

## 参考

- [docs/go-agent-harness-architecture.zh.md](docs/go-agent-harness-architecture.zh.md)
- [docs/plugin-catalog.zh.md](docs/plugin-catalog.zh.md)
- [docs/reference-analysis.zh.md](docs/reference-analysis.zh.md)
- [docs/coding-workspace.zh.md](docs/coding-workspace.zh.md)
- Pi：`packages/coding-agent/docs/extensions.md`、`packages/coding-agent/docs/usage.md`、`packages/agent/docs/harness.md`
- Pi LLM 重试：`packages/ai/src/utils/retry.ts`、`packages/coding-agent/src/core/agent-session.ts`
