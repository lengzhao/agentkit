# AgentKit `plugin_*.go` 与 DeepSeek Harness 插件定义对比

本文对比 `agentkit` 根包中的 `plugin_*.go` 接口契约与 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)（下称 **DSH**）在 Cordis 微内核上的等价能力定义，提炼可互相借鉴的设计点。

相关文档：[reference-analysis.zh.md](reference-analysis.zh.md)、[plugin-catalog.zh.md](plugin-catalog.zh.md)、[go-agent-harness-architecture.zh.md](go-agent-harness-architecture.zh.md)。

> **2026-08 接口重整**：`MessageEvent`/`OutboundEvent` 必填 `SessionID`；Loop 在 `Dispatch` 时写入 `ctx.Value(agentkit.KeySessionID)` / `ctx.Value(agentkit.KeyAgentID)`；`TurnInput` 不再重复携带路由字段。`SessionStore` 由 Agent 依赖并在 `RunTurn` 内用 context key 取出的 SessionID 执行 `Get`。SessionID 格式对齐 cc-connect（`platform:segment:...`），由 `platform/*` 生成。

## 1. 对比范围说明

| 维度 | AgentKit | DSH |
|---|---|---|
| 语言 | Go | TypeScript |
| 接口文件 | `plugin_agent.go` … `plugin_tool.go`（9 个） | 分散在 `packages/core/*`、`packages/*/src/index.ts` |
| 装配方式 | `pluginkit.Register(kind, New)` + YAML 实例图 | Cordis `Plugin`（`name` / `inject` / `Config` / `apply`）+ `cordis.yml` patch |
| 运行时访问 | 构造期 `Deps` 注入，请求路径不查容器 | `ctx.<service>` 服务容器 + 事件总线 |

DSH 没有与 `plugin_*.go` 一一对应的单文件；其「插件定义」是 **Cordis 插件形状** + **核心 Service 接口** + **事件扩展点** 三者的组合。下表按 AgentKit 文件组织对比。

```mermaid
flowchart LR
  subgraph ak ["AgentKit plugin_*.go"]
    PR["plugin_runner"]
    PL["plugin_loop"]
    PA["plugin_agent"]
    PS["plugin_session"]
    PP["plugin_prompt"]
    PT["plugin_tool"]
    PO["plugin_policy"]
    PH["plugin_hook"]
    PLL["plugin_llm"]
  end

  subgraph dsh ["DSH 等价层"]
    Boot["app-boot / headless / web"]
    AL["dsh-agent-loop"]
    AG["dsh-agent"]
    SE["dsh-session"]
    SP["dsh-system-prompt"]
    TO["dsh-tools"]
    AP["dsh-user-approval + tools/pre-execute"]
    EV["Cordis Events waterfall"]
    LM["dsh-llm"]
  end

  PR --> Boot
  PL --> AL
  PA --> AG
  PS --> SE
  PP --> SP
  PT --> TO
  PO --> AP
  PH --> EV
  PLL --> LM
```

## 2. 扩展模型：根本差异

### 2.1 DSH Cordis 插件形状

每个 npm 包导出 namespace plugin：

```typescript
// 典型形状（见 packages/sdk/server/tests/plugin-shape.spec.ts）
export const name = 'sdk-jsonrpc-server'
export const inject = ['agents']
export const Config = z.object({ ... })
export function apply(ctx: Context, config: Config) { ... }
```

特点：

- **运行时注册**：`apply(ctx)` 向 `ctx` 挂载服务、注册事件监听、注册工具。
- **依赖声明**：`inject` 数组保证拓扑排序；支持 intercept config。
- **配置校验**：`Config` 使用 Standard Schema / zod，启动前校验。
- **可逆副作用**：Fiber dispose 时撤销注册。

### 2.2 AgentKit pluginkit 插件形状

```go
func init() {
    pluginkit.Register("tool/read-file", New)
}
func New(cfg Config, deps Deps) (agentkit.Tool, error) { ... }
```

特点：

- **类型即角色**：构造函数返回值类型（`Tool`、`Loop`、`Policy`…）决定运行时角色，kind 字符串仅用于配置引用。
- **静态依赖**：`Deps` struct + `json` tag，构建期类型检查，无全局 `ctx`。
- **接口极简**：`plugin_*.go` 只定义语义契约，不含注册/事件框架代码。

### 2.3 取舍小结

| 方面 | DSH 优势 | AgentKit 优势 |
|---|---|---|
| 扩展粒度 | 事件点极细（20+ waterfall），scope 过滤 | 接口少、Policy/Hook 职责清晰 |
| 类型安全 | TS 模块增强（`SessionEventMap` 可扩展） | Go 接口 + 构建期 deps 检查 |
| 动态组合 | patch.yml 运行时替换任意插件 | 编译期链接，部署简单 |
| 测试 | `ctx.plugin()` 轻量挂载 | 直接构造接口实现，无框架 mock |
| 学习曲线 | 需理解 Cordis 事件模式 | 标准 Go interface 即可 |

## 3. 逐文件接口对比

### 3.1 `plugin_runner.go` — 进程入口

**AgentKit**

```go
type Runner interface {
    Run(context.Context) error
    Stop(context.Context) error
}
type Platform interface {
    Receive(context.Context) (MessageEvent, error)
    Send(context.Context, OutboundEvent) error
}
```

**DSH 等价**

- 无单一 `Runner` 接口；由 **profile + bundle**（`dsh-base`、`dsh-web-app`、`dsh-headless`）组装 Cordis 树。
- 消息入口分散：`host-apiproxy`（Web）、`headless`（一次性任务）、`acp`（ACP 协议）等，均实现各自的 transport，最终写入 `ctx.agents` / inbox。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 生命周期 | `Run` / `Stop` 显式 | Cordis Fiber `dispose` |
| 入口抽象 | `Platform` 统一 Receive/Send | 各 Host 适配器独立 |
| 多入口 | `platform/multiplex`（规划中） | Web + CLI + ACP 原生共存 |

**取长补短**

- AgentKit 可取：Runner/Platform 二分清晰，适合 Go 单二进制部署。
- DSH 可取：多 Host 并存是一等公民；AgentKit 应确保 `PlatformID` 在 `OutboundEvent` 中贯穿（已做），multiplex 尽快落地。
- AgentKit 缺口：架构文档提到 `StartStop`，但根包尚未定义该接口；Runner 应负责收集并有序启停子组件（对齐 DSH Fiber 生命周期）。

---

### 3.2 `plugin_loop.go` — 调度器

**AgentKit**

```go
type Loop interface {
    Dispatch(context.Context, LoopRequest) error
    Steer(context.Context, ModelMessage) error
    FollowUp(context.Context, ModelMessage) error
}
```

**DSH 等价**：`ctx.agentLoop`（`dsh-agent-loop`）

- 实现 `AgentFactory`：`createAgent` / `resume`，含 setup 事务、session 发布、loop 启动。
- 内部 `ReactLoopAgent` 驱动 turn/step 状态机，而非外部 `Dispatch` 单次调用。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 调度模型 | Loop 被动 `Dispatch`，Agent 跑完一次 turn | Agent 常驻 goroutine，inbox 驱动 |
| Session 路由 | Loop 按 SessionID 串行；Agent 持 `SessionStore` 并 `Get` | Agent.id == SessionId，一一对应 |
| Follow-up | `FollowUpMode`（one-at-a-time / all） | inbox `next-turn` 队列，FIFO |
| Steer | Loop.Steer → SessionControl | `agent.steer()` → inbox `next-step` |
| 创建/恢复 | 不在 Loop 接口内 | `AgentFactory.create/resume` + `AgentSetup` |

**取长补短**

- AgentKit 可取：`Loop` 接口极简，易于测试单次 Dispatch；`FollowUpMode` 显式可配置。
- DSH 可取：
  - **Agent 与 Session 同 ID**，路由无歧义。
  - **inbox 持久化**（`agent/inbox/spliced` 会话事件），重启可恢复队列。
  - **AgentSetup** 在发布前完成 scoped 注册，观察者不见半成品。
- AgentKit 建议：
  - 长期考虑将 turn 驱动内聚到 Agent（类似 DSH），Loop 只做路由与多 Agent 仲裁。
  - Loop 按 SessionID 串行 + Agent 依赖 `SessionStore` 已对齐 cc-connect 不透明 SessionKey；可补充 inbox 持久事件。

---

### 3.3 `plugin_agent.go` — 执行体

**AgentKit**

```go
type Agent interface {
    ID() AgentID
    RunTurn(context.Context, TurnInput) error
}
type SessionControl interface {
    Steer(...) / FollowUp(...) / Cancel(...) / DrainFollowUps(...)
}
type TurnInput struct {
    Message ModelMessage
    Emit    OutboundEmit
    Control SessionControl
}
```

Loop 在调用 Agent 前把 `MessageEvent.SessionID`、`AgentID`、`PlatformID` 写入 context key；Agent 插件通过 `ctx.Value(agentkit.KeySessionID)` 取 ID，并用 `deps.sessionStore` 在 `RunTurn` 内加载 Session。Loop 不注入 `Session` 对象。

**DSH 等价**：`Agent`（`dsh-agent/runtime-types.ts`）

```typescript
interface Agent {
  id: SessionId
  session: Session
  inbox: Inbox
  status: 'idle' | 'running'
  ctx: Context          // agent-scoped Cordis 子树
  cancel(cause, options?)
  whenIdle(): Promise<void>
  runMaintenance(task)
  send(message, target, wakeup)
  followup(message)
  steer(message)
  inject(message)      // 注入上下文，不唤醒
}
```

| 对比项 | AgentKit | DSH |
|---|---|---|
| 控制面 | `SessionControl` 由 Loop 注入 TurnInput | Agent 自身方法 + inbox |
| Session 加载 | Agent `SessionStore.Get(ctx.Value(KeySessionID))` | Agent 内嵌 `session` 字段 |
| inject | **无** | `inject()` 排队 next-step 上下文，不唤醒 |
| cancel | `SessionControl.Cancel` | `cancel(cause, { keepInbox? })` + AbortSignal 贯穿 turn |
| 状态 | 无 status 概念 | `idle` / `running` + `agent/status` 事件 |
| scoped 世界 | 无 agent-scoped ctx | `agent.ctx` 隔离工具/prompt/监听器 |
| 维护任务 | 无 | `runMaintenance` 在 idle 相运行非 turn 任务 |

**取长补短**

- AgentKit 可取：Agent 只做 `RunTurn`，控制面与执行面分离，适合 pluginkit 静态图。
- DSH 可取：
  - **`inject()`** 是独立能力（审批结果、子任务完成通知），AgentKit 仅有 steer/follow-up，缺少「静默注入」。
  - **`AgentCancelCause`** 结构化（user/parent/hook/disposed），比单纯 reason 字符串更可观测。
  - **`whenIdle()`** 便于测试与编排（子 agent 等待父 agent 空闲）。
- AgentKit 建议：在 `SessionControl` 增加 `Inject(ctx, ModelMessage) error`；`Cancel` 支持 `keepQueue bool`。

---

### 3.4 `plugin_session.go` — 会话存储

**AgentKit**

```go
type Session interface {
    ID() SessionID
    Append(ctx, SessionEvent) (EventSeq, error)
    Read(ctx, EventSeq) ([]SessionEvent, error)
    DeriveMessages(ctx) ([]ModelMessage, error)
}
// SessionStore resolves durable sessions by opaque SessionID.
// Agent plugins depend on SessionStore; Loop only routes SessionID.
type SessionStore interface {
    Get(ctx, SessionID) (Session, error)
}
```

**DSH 等价**：`ctx.sessions`（`dsh-session`）

- `Session` 含 `deriveMessages()`、fork、epoch header、`request/header` 事件。
- `SessionEventMap` 通过 TS module augmentation 扩展事件类型（插件可追加事件种类）。
- `SessionHeader` 持久化 cwd、parentSession、delegationDepth、agentPreset 等元数据。
- 独立 `ctx.sessionPersistence` seam（jsonl / sqlite）。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 事件类型 | `EventType` 字符串常量 | 强类型 `SessionEventMap` + 可扩展 |
| 元数据 | `SessionEvent` 含 AgentID | `SessionHeader` 丰富持久字段 |
| Fork | 未在接口层暴露 | `sessions.fork(source, boundary?)` |
| LLM 配置 | 不在 Session | `request/header` 记录 `LlmCallConfig` |
| Store | `SessionStore.Get` | 内存 store + persistence 双层 |

**取长补短**

- AgentKit 可取：四方法接口极简，易于实现 memory/jsonl/sqlite。
- DSH 可取：
  - **Session 即真相源**贯彻更彻底：inbox splice、tool code-dispatch 均记入日志。
  - **Fork / resume** 是一等 API。
  - **LlmCallConfig 按会话记录**，换模型不丢历史语义。
- AgentKit 建议：评估 `Session` 是否需 `Fork`；`DeriveMessages` 保持纯函数，但事件类型考虑用注册表扩展（类似 DSH 的 module augmentation 思路可用 Go 的 typed event data + `json.RawMessage` 分发）。

---

### 3.5 `plugin_prompt.go` — 提示词组装

**AgentKit**

```go
type PromptAssembler interface {
    Assemble(ctx, PromptRequest) (Prompt, error)
}
type SectionProvider interface {
    Sections() []Section  // Name, Order, Build func
}
```

**DSH 等价**：`ctx.systemPrompt`（`dsh-system-prompt`）

- **Section**：静态或动态 `text`，支持 `{{variable}}` 插值。
- **Context**：动态用户角色快照（与 section 分开）。
- **Variables**：键值对，渲染时替换。
- **Tools**：组装时收集 scoped tool schemas。
- **`system-prompt/assemble` waterfall**：监听器可改写 assembly，但有 `complete` section 保护。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 组装入口 | 单一 `Assemble` | registry + waterfall |
| 动态上下文 | 合并在 Section.Build | 独立 `PromptContext` |
| 工具 schema | `PromptRequest.Tools` 传入 | 从 `ctx.tools` scoped 收集 |
| 变量插值 | 无 | `{{var}}` + `variables` 表 |
| Scope | 无 | global / agent scope 分层 |

**取长补短**

- AgentKit 可取：`SectionProvider` + `Order` 与 DSH section 一致，简单直接。
- DSH 可取：
  - **Context vs Section 分离**：section 是 system，context 是 user-role 快照，职责更清晰。
  - **assemble waterfall** 允许运行时调整，但 `complete` 防覆盖。
  - **scoped 组装**：不同 agent preset 看到不同 tools/sections。
- AgentKit 建议：`Prompt` 增加 `Contexts []PromptSection`；`PromptAssembler` 实现层参考 DSH 做 scope 过滤（与 context key 对齐）。

---

### 3.6 `plugin_tool.go` — 工具

**AgentKit**

```go
type Tool interface {
    Name() / Description() / InputSchema() / Call(ctx, ToolCall) (ToolResult, error)
}
type ToolRuntime interface {
    Visible(context.Context) ([]ToolSpec, error)
    Execute(ctx, ToolCall) (ToolResult, error)
}
```

**DSH 等价**：`ctx.tools`（`dsh-tools`）

- 注册：`tools.register(name, handler, schema, options)`。
- 执行流水线（waterfall）：
  - `tools/pre-execute` → allow / deny / ask
  - `tools/execute` → around（超时/重试）
  - `tools/post-execute` → 改写 result
  - `tools/result` → 观察（emit）
- **Code Mode**：`run_code` 折叠工具为 SDK 程序调用。
- **Presentation**：`ToolCallView` / `ToolResultView` 供 UI 渲染。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 消费者接口 | `Tool` 四方法 | `defineTool` + schema 推导 |
| 运行时 | `ToolRuntime` 统一 Visible/Execute | `ToolRuntime` service + 事件链 |
| 策略 | 外置 `Policy` | `pre-execute` 内嵌 allow/deny/ask |
| 改写结果 | `Hook AfterTool` | `post-execute` waterfall |
| UI 呈现 | 无 | `presentation.ts` 丰富视图类型 |
| Scope | context key（`KeySessionID` / `KeyAgentID` / `KeyTurnID`） | `dsh-scope` 多层 restrict |

**取长补短**

- AgentKit 可取：**Policy 与 Hook 分离**——DSH 的 `pre-execute` 同时承担策略与观察，AgentKit 用 `Policy.Evaluate` + `BeforeToolHook` 拆开，边界更清晰（与 reference-analysis 一致）。
- DSH 可取：
  - **`tools/execute` around** 适合超时/重试/指标，AgentKit 尚无等价 Hook。
  - **Presentation 层**与工具解耦，Web UI 可渲染 diff/terminal/web 等专用视图。
  - **Code Mode** 降低 tool schema token 消耗。
- AgentKit 建议：增加 `AroundToolHook` 或让 Tool Runtime 内置 execute 包装链；长期考虑 presentation 类型（可在 `cap/` 或 `runtime/tools` 定义，不进 `plugin_tool.go`）。

---

### 3.7 `plugin_policy.go` — 策略与审批

**AgentKit**

```go
type Policy interface {
    Evaluate(ctx, PolicyInput) (Decision, error)
}
// DecisionKind: allow | deny | ask
type Approval interface {
    Ask(ctx, ApprovalRequest) (ApprovalDecision, error)
}
```

**DSH 等价**

- 策略：`tools/pre-execute` waterfall → `PreToolDecision { allow | deny | ask }`
- 审批：`ctx.approval`（`dsh-user-approval`）→ `approval/request` waterfall → `ApprovalOutcome`
- Permission presets：`ctx.permissionPresets` 按 agent 预设组合策略

| 对比项 | AgentKit | DSH |
|---|---|---|
| 裁决入口 | 独立 `Policy` 接口 | 事件 `tools/pre-execute` |
| ask 路径 | `DecisionAsk` + `Approval.Ask` | pre-execute `ask` → approval service |
| 审计 | `Decision.Audit` map | 分散在各 listener |
| 预设 | 靠多个 Policy 插件组合 | `permission-presets` 一等概念 |

**取长补短**

- AgentKit 可取：Policy Plane 单点裁决，不会与 Hook 改写混淆。
- DSH 可取：
  - **permission presets** 把 allow/deny/ask 策略打包为 agent 级配置，适合「只读审查 agent」等场景。
  - **approval/request waterfall** 允许多 UX 提供方链式尝试。
- AgentKit 建议：在 `cap/approval` 或 preset 层增加「按 AgentID 绑定 Policy 链」；保持 `Evaluate` 同步、轻量。

---

### 3.8 `plugin_hook.go` — 钩子

**AgentKit（当前实现）**

```go
type HookProvider interface { Hooks() []Hook }
type BeforeStepHook / BeforeToolHook / AfterToolHook / TurnStoppingHook
// HookProvider 贡献的 hook 按 deps.providers 列表顺序执行；同一 provider 内 Hooks() 返回顺序保留。
```

**DSH 等价（节选）**

| DSH 事件 | 模式 | AgentKit 映射 |
|---|---|---|
| `agent/pre-step` | waterfall | `BeforeStepHook`（但无 reject/enter 决策类型） |
| `agent/request` | waterfall | 文档规划 `hook/llm-request`，**未实现** |
| `agent/request-error` | waterfall | **未实现** |
| `agent/turn-stopping` | serial | `TurnStoppingHook`（已实现，`hook/turn-continue` 为默认驱动） |
| `tools/pre-execute` | waterfall | `Policy`（裁决，非 Hook） |
| `tools/post-execute` | waterfall | `AfterToolHook`（但无 block/replace 决策） |
| `tools/execute` | waterfall | **未实现** |
| `system-prompt/assemble` | waterfall | 可在 `PromptAssembler` 内部扩展 |

| 对比项 | AgentKit | DSH |
|---|---|---|
| 钩子类型 | 4 种 typed interface | 20+ 事件点 |
| 链式语义 | deps 列表顺序，error 中断 | waterfall `next()` 委托 |
| 改写 vs 否决 | Hook 返回 error 中断 | `PreStepDecision.reject` / `PreToolDecision.deny` 显式 |
| Scope | 无 | agent-scoped 监听器 |

**取长补短**

- AgentKit 可取：typed hook 易读、易测；Policy 不混入 Hook。
- DSH 可取：
  - **`PreStepDecision`**：`reject` vs `enter(messages)` 比单纯返回 error 语义更精确。
  - **`agent/turn-stopping`**：已补齐为 `TurnStoppingHook`（`Continue` 延长 turn / `Stop` 强制收尾，硬预算优先），见 [autonomous-run.zh.md](autonomous-run.zh.md)。
  - **waterfall `next()`**：链式组合比纯 Order 更灵活（可短路或委托）。
- AgentKit 建议：
  - `BeforeStep` 增加 `StepDecision`（Allow / Reject / EnterMessages）。
  - `AfterTool` 增加可选「替换结果」返回值，对齐 `post-execute`。
  - 按 plugin-catalog 补齐 `BeforeLLMRequest` 接口（`TurnStopping` 已补齐）。

---

### 3.9 `plugin_llm.go` — 模型提供方

**AgentKit**

```go
type LLMProvider interface {
    Name() string
    Stream(ctx, LLMRequest) (LLMStream, error)
}
type LLMRequest struct { Model, Messages, Tools }
type LLMEvent struct { Type, Message, Delta, ToolCall, Usage, Raw }
```

**DSH 等价**：`ctx.llm`（`dsh-llm`）

- Adapter 注册：`registerAdapter(provider, model, streamFn, options)`。
- **`LlmCallConfig`** 按会话记录在 `request/header` 事件。
- **`llm/stream` waterfall**：可包装流（日志、限速）。
- **`agent/request` waterfall**：提议替换 call config。
- 消息类型：`Message` 含 `ContentBlock`（text/thinking/toolCall/toolResult）。
- `deepFreeze` 保证请求不可变。

| 对比项 | AgentKit | DSH |
|---|---|---|
| 配置状态 | 每次 `LLMRequest` 传入 | 会话级 `LlmCallConfig` + header 事件 |
| 流事件 | 复用 Pi RPC `AssistantMessageEventType` | adapter 自有 chunk → `assistant/chunk` 会话事件 |
| 请求拦截 | 无接口 | `agent/request` waterfall |
| 错误恢复 | 无接口 | `agent/request-error` + retry policy |
| Thinking | 通过 `thinking_*` 事件 | adapter 层统一 reasoningEffort |

**取长补短**

- AgentKit 可取：Provider 只管 `Stream`，适配层薄；与 Pi RPC 事件词汇对齐便于互通。
- DSH 可取：
  - **会话级 call config** 避免静默改模型；变更有日志。
  - **request-error 恢复链** 是生产必备。
- AgentKit 建议：`LLMRequest` 增加 `CallConfig` 字段并由 Session 记录；在 LLM Runtime（非 `plugin_llm.go`）实现 request 拦截与 retry。

---

## 4. AgentKit 有规划但 DSH 已成熟的能力

以下在 `plugin-catalog.zh.md` 或架构文档中出现，但根包 `plugin_*.go` 尚未定义或 `plugin_command.go` 已删除：

| 能力 | plugin-catalog | 当前代码 | DSH |
|---|---|---|---|
| Slash 命令 | `command/*` | `plugin_command.go` 已删，CLI 硬编码 | `ctx.commands` |
| 生命周期 | `StartStop` | 仅文档，无类型 | Fiber dispose |
| Compaction | `compaction/*` + hook | 仅 `EventCompaction` | `ctx.compaction` seam |
| Scope 隔离 | 隐含于 context key | 仅 Turn 级字段 | `dsh-scope` 多层 |
| Settings | `settings/*` | 无 | `ctx.settings` |
| Telemetry | `telemetry/*` | 无 | `ctx.sessionTelemetry` |

## 5. 综合取长补短

### 5.1 AgentKit 应保留的优势

1. **静态依赖注入**：`Deps` struct 优于运行时 `ctx.get()`，利于测试与重构。
2. **Policy / Hook 分离**：避免 Pi/DSH 式「一个扩展点既 block 又改写」的模糊边界。
3. **返回值定角色**：插件 kind 是配置别名，接口类型是真相，降低「字符串驱动」错误。
4. **薄 Provider 接口**：`LLMProvider` 只流式输出，横切逻辑放 Runtime。

### 5.2 建议从 DSH 吸收的设计

按优先级排序：

| 优先级 | 项 | 建议落点 |
|---|---|---|
| P0 | `Inject` 静默上下文 | `SessionControl.Inject` |
| P0 | `PreStepDecision` 显式 reject/enter | 扩展 `BeforeStepHook` 或 `BeforeStep` 返回值 |
| P0 | inbox 持久化事件 | `events.go` + `session/*` 实现 |
| P1 | `agent/request` / `request-error` 拦截 | `plugin_hook.go` 或 LLM Runtime |
| P1 | `tools/execute` around（超时/重试） | Tool Runtime 包装链 |
| P1 | `StartStop` 生命周期 | 新 `plugin_lifecycle.go` 或 `agentkit.go` |
| P1 | Session `Fork` + `SessionHeader` 元数据 | `plugin_session.go` |
| P2 | Prompt Context 与 Section 分离 | `plugin_prompt.go` |
| P2 | Tool presentation 视图 | `cap/` 或 `runtime/tools/presentation` |
| P2 | Permission preset 组合 | `presets/` + Policy 链 |
| P3 | Code Mode 工具折叠 | `tool/code-mode` 插件 |
| P3 | `command` 插件接口 | 恢复 `plugin_command.go` |

### 5.3 DSH 可借鉴 AgentKit 的设计

1. **显式 FollowUpMode**：DSH inbox 默认 FIFO one-at-a-time，AgentKit 将其提升为配置项。
2. **ToolRuntime 与 Tool 分离**：DSH 的 `ctx.tools` 混合注册与执行；AgentKit 的 `Tool`（consumer）+ `ToolRuntime`（编排）更清晰。
3. **Loop 无状态 Dispatch**：更易于水平扩展 Worker 模式（`platform/worker`）。

## 6. 接口演进路线图（建议）

```mermaid
flowchart TB
  subgraph phase1 ["Phase 1 — 对齐 DSH 控制面"]
    A1["SessionControl.Inject"]
    A2["BeforeStep → StepDecision"]
    A3["StartStop + Runner 收集启停"]
    A4["inbox 持久事件 agent/inbox/spliced"]
  end

  subgraph phase2 ["Phase 2 — 对齐 DSH 执行面"]
    B1["LLM request / request-error hooks"]
    B2["AroundTool / PostTool 可替换结果"]
    B3["Session.Fork + Header 元数据"]
    B4["plugin_command.go 恢复"]
  end

  subgraph phase3 ["Phase 3 — 产品化"]
    C1["Prompt Context 分离 + scope 组装"]
    C2["Tool presentation 类型"]
    C3["Permission preset"]
    C4["Code Mode 可选"]
  end

  phase1 --> phase2 --> phase3
```

## 7. 文件级对照索引

| AgentKit 文件 | 核心类型 | DSH 包 / ctx 键 |
|---|---|---|
| `plugin_runner.go` | `Runner`, `Platform` | app-boot, headless, host-apiproxy |
| `plugin_loop.go` | `Loop` | `dsh-agent-loop` → `ctx.agentLoop` |
| `plugin_agent.go` | `Agent`, `SessionControl` | `dsh-agent` → `ctx.agents` |
| `plugin_session.go` | `Session`, `SessionStore` | `dsh-session` → `ctx.sessions` |
| `plugin_prompt.go` | `PromptAssembler`, `SectionProvider` | `dsh-system-prompt` → `ctx.systemPrompt` |
| `plugin_tool.go` | `Tool`, `ToolRuntime` | `dsh-tools` → `ctx.tools` |
| `plugin_policy.go` | `Policy`, `Approval` | `tools/pre-execute` + `dsh-user-approval` |
| `plugin_hook.go` | `HookProvider`, `*Hook` | Cordis `agent/*`, `tools/*` events |
| `plugin_llm.go` | `LLMProvider`, `LLMStream` | `dsh-llm` → `ctx.llm` |
| （缺失）`plugin_command.go` | — | `dsh-commands` → `ctx.commands` |
| （缺失）lifecycle | `StartStop`（文档） | Cordis Fiber lifecycle |

---

*文档版本：2026-08-22，基于 agentkit 工作区 `plugin_*.go` 与 deepseek-harness `master` 分支 `packages/core/*` 只读对比。*
