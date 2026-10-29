# 将宿主工具运行时提供给 ACP Agent

`acp/session-mcp-tools` 支持两个互斥后端：

- `deps.provider`：兼容原有 ToolProvider，直接调用 Tool.Call。
- `deps.runtime`：通过 ToolRuntime.Visible / Execute 保留渐进披露、策略、审批、超时、hooks 和执行错误语义。

两者都未设置或同时设置时，构造失败。Runtime 模式推荐与本地 Agent 引用同一工具运行时实例。

以下为合并到现有配置的片段（宿主需已有 `agent.cursor.default` 和 `tools.default`）：

```yaml
acp.session-tools.default:
  use: acp/session-mcp-tools
  config:
    serverName: host-tools
  deps:
    runtime: tools.default

agent.cursor.default:
  deps:
    sessionMcp: acp.session-tools.default
```

Runtime 模式自动通过不透明连接凭证找到宿主上下文，不需要配置 `bind` 来传递审批对象或 turn state。`bind` 继续用于 Provider 模式；`headers` 和可选的 `apiToken` 两种模式都支持。内部作用域 header 保留，静态配置不能覆盖。

## 作用域和生命周期

MCP 连接随 ACP 会话复用；宿主在每次 RunTurn 开始时绑定当前上下文，在退出时取消并释放该轮绑定。目录、description、schema、deferred 揭示状态和执行使用同一作用域。每轮有独立 turn state，结束后不能继续执行工具。

连接按宿主会话、Agent 和 ACP bridge 实例隔离；当前工作区从每轮上下文读取，工作区变化不会让复用连接的凭证失效。连接淘汰和关闭时撤销对应凭证，旧连接的清理不能影响新的 bridge。同一作用域的工具调用串行处理；不同作用域可以并行执行。

兼容性边界：现有客户端没有逐请求的 turn 标识，因此连接路由采用会话级回退。工具请求在进入 HTTP 服务时捕获当前 turn，执行时不会切换到下一轮；若旧请求直到下一轮开始后才到达，服务端无法判断其原始轮次。执行上下文、审批、取消和揭示状态仍逐轮更新，不承诺严格的客户端跨轮次身份隔离。

自定义宿主接入时：

1. 为每个连接生命周期使用唯一的 `capacp.WithSessionMCPBinding` 标识。
2. 每轮调用可选接口 `SessionMCPTurnProvider.BeginMCPTurn`，使用返回 ctx 建立会话和发送 prompt；复用 ACP 会话也不能省略 Begin。
3. 无论成功、失败或取消都调用返回的 end，检查其返回的 turn-fatal 错误。end 会取消工具上下文；应在清理前记录真实的 turn 结束状态。
4. bridge 退役时通过 `SessionMCPReleaser.ReleaseMCPSession` 释放资源，传入相同连接标识及工作区、会话、Agent 身份；进程退出调用 Close。

内置 `agent/acp-remote` 已负责这些调用。配置命令可以提前建立 MCP 连接，首轮开始前目录为空；没有活动 turn 时不能执行工具。

## 动态目录与结果

每轮绑定、工具执行结束和显式 `Refresh(ctx)` 时重读目录。search/describe 揭示的工具会加入 MCP 目录，可以按名称直接调用。仅当名称、description 或 schema 实际变化时发送 `notifications/tools/list_changed`，通知只影响所属作用域。无请求、无刷新信号时不主动轮询外部目录；外部工具热更新后应调用 Refresh。

Runtime 模式下 Refresh 的 ctx 必须携带同一作用域身份。刷新失败返回错误，保留已发布目录；调用后目录刷新失败也会返回 MCP 错误诊断。

策略拒绝、审批拒绝、超时和普通执行错误返回 MCP `isError=true`，保留 Runtime 的文本诊断。审批取消及其他 turn-fatal 错误还会取消宿主 turn，并通过 end 返回原始错误。完整 Audit 留在宿主运行时中，MCP 文本结果不承载全部审计结构。

ToolProvider 模式继续支持显式 Refresh，并对无变化的刷新去重。它不经过 ToolRuntime，不提供上述运行时策略保证。
