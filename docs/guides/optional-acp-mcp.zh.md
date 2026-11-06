# 可选 ACP 子代理组合与 MCP 工具桥接

ACP 委派复用已有的 `subagent/loop-agent`，通过 `deps.agents` 注入
`agentkit.Agent`。MCP 协议转换复用公共库 `runtime/mcpbridge`。
两种能力独立选用，不依赖 ACPD、Buddy、NEX 或 TaskHub，也不改变默认 preset。

## ACP 委派：通过 deps 组合已有插件

```yaml
subagent.remote:
  use: subagent/loop-agent
  config:
    freshSession: true
    timeoutSeconds: 900
    agents:
      - name: reviewer
        description: 检查代码并返回结论
        agent: remote
        async: false
  deps:
    sessionStore: session.store
    agents: [agent.remote]

agent.remote:
  use: agent/acp-remote
  config:
    id: remote
    command: ["my-acp-agent", "--stdio"]
    autoApprove: false
    pool:
      strategy: none
  deps:
    workspace: workspace.default
    fs: filesystem.default
    sessionStore: session.store
    sessionEvents: session.events
    telemetry: telemetry.toolkit

session.events:
  use: session/events
```

其余依赖 ID 替换为宿主实例图的实际名称。两个插件分别构造，委派器只依赖
`agentkit.Agent`，不导入 ACP 插件实现，也可以注入其他 Agent 后端。
`config.agents[].agent` 对应注入 Agent 的 `config.id`；`deps.agents` 引用的是
实例 ID。仅需要时才导入或配置 ACP 插件，无须 MCP。

`freshSession: true` 为每次委派分配独立历史，即使从同一父事件快照发起也不复用。
省略时保留原有会话 ID 行为。示例默认为同步；已有 async 机制继续保留。
`pool.strategy: none` 避免为一次性任务保留 ACP 连接。

必须为 ACP Agent 接入与委派器相同的 SessionStore，以及 SessionEvents，
否则它不会持久化供委派器读取的结论。审批需要设置 `autoApprove: false` 并由
宿主提供父审批 broker。子执行继承父租户、用户、工作目录、取消和审批目标，
不把父历史当作任务输入；调用方应提供自包含任务。宿主负责所有依赖的生命周期，
退出时收集并调用 ACP Agent 的 ShutdownHooks。

结果沿用现有语义：显式 finish 决定 completed/blocked；没有 finish 时返回
stopped 与最后一条 assistant 文本。角色目录、人格文件及业务路由由宿主负责。

## 公共工具桥接库

宿主通过 `runtime/mcpbridge.New(runtime)` 传入已有 ToolRuntime，得到
`*mcpbridge.Bridge`。无需增加配置实例或插件注册；公共库不启动监听，
不要求 ACP、Agent 或 Workspace。

* `Tools(ctx)`：按当前执行上下文调用 Visible，返回排序后的 MCP 工具目录。
* `CallTool(ctx, request)`：生成调用 ID，将参数交给 ToolRuntime.Execute，保留
  runtime 的策略、审批、超时和审计结果；不会直接调用底层 Tool.Call。

没有参数时映射为 `{}`。deny/timeout/error 审计结果映射为 MCP isError。
Execute 返回错误时，同时返回 MCP 错误结果和原始 Go error；宿主应向 MCP
客户端发送工具错误结果，并检查原始 error 是否为 turn-abort，而非把普通工具
失败转换为 JSON-RPC 协议错误。

桥接不持有执行状态或缓存目录：请求身份、认证、传输、请求取消、turn 生命周期、
State、串行化及目录刷新均由宿主负责。不要用未经认证的客户端 Header 直接构造
执行身份。构建共享 MCP registry 时，宿主必须按调用者隔离目录；不能把一个租户
的 Tools 结果用作所有租户的共享目录。底层 ToolRuntime 需要支持宿主的并发方式。

已有 `acp/session-mcp-tools` 的 runtime 模式复用该桥接，并继续负责每会话作用域、
目录刷新、审批上下文和取消；其配置与 provider 模式保持兼容。
