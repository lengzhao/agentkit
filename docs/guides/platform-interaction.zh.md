# Platform 人机交互（Permission 协议）

`ask_user`、工具审批、表单/多选统一走 **Permission 协议**（`cap/permission` + Loop `PermissionBroker`），对齐 cc-connect 的 pending + 入站分流模型。

> **Permission** 指「一次需要人类输入才能继续的裁决」，不限于权限检查——`ask_user` 的开放提问同样走这条通道。

## 架构

```mermaid
flowchart TB
    subgraph inbound["入站（独立 goroutine）"]
        Rcv["Platform.Receive"]
        Runner["Runner"]
        TD{"event.Reply != nil ?"}
        Rcv --> Runner --> TD
    end

    subgraph turn["Turn 内（Loop 持 session 锁）"]
        Agent["Agent.RunTurn"]
        TR["tools/runtime.Execute"]
        Broker["PermissionBroker.Await"]
        Agent --> TR --> Broker
        Broker -->|"permission/request"| Emit
        Broker --> Wait["等待 resolve"]
    end

    subgraph platform["Platform"]
        Send["Send(permission/request)"]
        User["用户"]
        Send --> User --> Rcv
    end

    Emit --> Send
    TD -->|"yes"| Resolve["DeliverPermissionReply"]
    TD -->|"no + pending"| Cancel["SupersedePending"]
    TD -->|"no"| Queue["新 turn"]
    Resolve --> Wait
    Cancel --> Wait
    Wait --> TR
```

核心原则：

1. **入站与 turn 解耦**：turn 等待回复时 Runner 仍能 `Receive`。
2. **`Reply != nil` 不开新 turn**：Platform 回传 `MessageEvent.Reply`，Runner 投递给 Broker。
3. **同一 session 至多一个 pending**；`KindQuestion` 一次一题。
4. **无交互 platform 立即降级**：`Interactive=false` → `OutcomeNoHuman`，不挂 pending。

## 核心类型

```go
type Kind string
const (
    KindAllowDeny Kind = "allow_deny" // 工具是否允许执行
    KindQuestion  Kind = "question"   // ask_user / 单选 / 多选
)

type Request struct {
    ID       string
    Kind     Kind
    ToolCall *agentkit.ToolCall // allow_deny
    Question *Question          // question，仅一题
    Timeout  time.Duration      // 0 → EffectiveTimeout（交互平台默认 10 分钟）
    AskedBy  string
}

type Reply struct {
    RequestID    string
    UserID       string
    Decision     string         // allow_deny: allow/deny/y/n
    Selected     []int
    Text         string
    UpdatedInput map[string]any
    Cancelled    bool
}

type Outcome string
const (
    OutcomeResolved   Outcome = "resolved"
    OutcomeTimeout    Outcome = "timeout"
    OutcomeNoHuman    Outcome = "no_human"
    OutcomeCancelled  Outcome = "cancelled"
    OutcomeSuperseded Outcome = "superseded"
)

type Capability struct {
    Interactive    bool
    MultiSelect    bool
    DefaultTimeout time.Duration
    AnswerScope    AnswerScope // asker（默认）| anyone
}
```

`Capable` 由 **leaf** platform 实现；`multiplex` 经 `CapabilityRouter` 按 `PlatformID` 转发。

Broker 经 `KeySessionControl`（`*loop.Control`）注入；`tools/runtime` 与 `ask_user` 通过 `permission.BrokerFrom(ctx)` 获取。

## 事件

| Type | 含义 |
|---|---|
| `permission/request` | 开始等待人类输入 |
| `permission/resolved` | 结束等待（含 `Outcome`） |

`MessageEvent.Reply` 为 `json.RawMessage`，与 `Message` 互斥。

## Platform 接入

| 平台 | Capability | 展示 | 回传 |
|---|---|---|---|
| `platform/cli` | `Interactive`, `DefaultTimeout=10m`, `ScopeAnyone` | stderr prompt | `Receive` → `Reply` |
| `platform/feishu` | `Interactive`, `MultiSelect`, `DefaultTimeout≥10m` | 卡片 + 按钮 | callback 或 reply-to |
| `platform/slack` | 同上 | Block Kit 卡片 + 按钮 | 交互 payload |
| `platform/chat-api` | 默认 `Interactive=true`；`config.interactive: false` 降级为 headless | SSE `question_request` / debug 弹窗 | `POST /runs/.../respond` |
| `platform/acp` | `Interactive`, `DefaultTimeout=10m`, `ScopeAnyone` | ACP `session/update` 流式 chunk | ACP `request_permission`（Send 内同步） |
| `platform/headless` | `Interactive=false` | 无 | 直接 `NoHuman` |
| `platform/multiplex` | **转发 leaf Capability** | 按 `PlatformID` 路由 | 子平台 `Receive` 原样上送 |

`multiplex` 必须转发 leaf 的 `PermissionCapability()`，不能自己在 root 实现。

## 飞书流式进度卡片

`platform/feishu` / `platform/lark` 在 `enableFeishuCard: true`（默认）时，通过 Interactive Card 的 `Patch` 原地更新展示 agent 输出。整轮 turn 复用**一张卡片**，thinking / tool 进度与最终正文都在同一条消息里更新，不再为中间状态单独发消息。

| 配置 | 默认 | 说明 |
|---|---|---|
| `progressStyle` | `legacy` | `card`：Card 2.0 可折叠进度面板 + 正文；`compact`：结构化进度列表；`legacy`：仅流式正文 |
| `showThinking` | `false` | `card`/`compact` 下是否在进度区展示 thinking |
| `showToolProgress` | `card`/`compact` 时为 `true` | 是否展示 tool 调用名与参数摘要 |
| `enableFeishuCard` | `true` | `false` 时回退纯文本出站 |

`card` / `compact` 模式下，进度区将 **tool 调用**（参数）与 **tool 结果**（输出）分两行展示；平台监听 `tool/result` 生命周期事件写入结果行。

`card` / `compact` 模式下，turn 未完成且已发出进度卡片后，平台每 **5 秒** 原地 `Patch` 一次卡片，刷新页脚「⏱ 运行中 …」耗时；tool 长时间无新事件时用户仍能看到状态在更新。

```yaml
platform.default:
  use: platform/feishu
  config:
    progressStyle: card
    showThinking: false
    showToolProgress: true
```

与 hermes-agent `display.*` 的对应关系：`tool_progress` → `showToolProgress` + `progressStyle: card`；`thinking_progress` → `showThinking`；进度气泡原地编辑 → 单卡 `Patch` 更新。

## 无人值守与 Policy 分工

| 场景 | 行为 |
|---|---|
| `approval/auto-allow` | runtime 短路 allow，不创建 pending |
| `approval/auto-deny` | runtime 短路 deny |
| `policy` deny | 不进入 Permission 平面 |
| headless / `Interactive=false` | allow_deny → deny；question → guidance |
| schedule fire turn | 出站仍走 delivery platform（如 chat-api `send`）；permission 强制 `Interactive=false`，`ask_user` 降级为 `NoHuman` |
| 超时 | `OutcomeTimeout`，按 kind 降级并**继续** turn |

`auto-allow` / `auto-deny` 只作用于 allow_deny；`KindQuestion` 始终走 Broker。

## 超时与持久化

- 超时链：`Request.Timeout` → `Capability.DefaultTimeout` → 10 分钟。
- 等待 ≥ `permissionPersistAfterSeconds`（默认 60s）时 `permission/request` 落入 session 日志（审计用，模型不可见）。
- **不做 durable resume**：重启后 `session/recovery` 收尾悬挂 pending，补 orphan `tool/result`。

## 配置

```yaml
tool.ask-user.default:
  use: tool/ask-user

platform.chat-api:
  use: platform/chat-api
  config:
  # interactive: false   # 无人值守 BFF：ask_user 降级为 NoHuman，不挂 SSE 提问

loop.default:
  config:
    permissionPersistAfterSeconds: 60
```

## 实现入口

`cap/permission/`、`runtime/loop/permission.go`、`runtime/runner/dispatch.go`、`runtime/tools/runtime.go`、`runtime/platform/cli/permission.go`

外部 ACP Agent 的权限请求经 `agent/acp-remote` 桥接到同一 Broker，见 [plugin-catalog.zh.md](../plugin-catalog.zh.md) §3.2 `agent/acp-remote`。
