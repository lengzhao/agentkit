# 可选 ACP 子代理与 MCP 工具桥接

这两个插件可以分别导入、构造和使用，互不依赖，也不依赖 ACPD、Buddy、
NEX 或 TaskHub。导入只注册构造器；配置实例才启用功能。不修改默认 preset。

## ACP 子代理：`subagent/acp`

只导入 `github.com/lengzhao/agentkit/plugins/subagent/acp`，或使用已有的
`plugins` 全量注册包。它实现 `cap/subagent.Spawner`，可以独立调用，亦可接给
`tool/subagent` 和 `prompt/section/subagents`。

```yaml
subagent.remote:
  use: subagent/acp
  config:
    timeoutSeconds: 900
    agents:
      - name: reviewer
        description: 检查代码并返回结论
        acp:
          command: ["my-acp-agent", "--stdio"]
  deps:
    workspace: workspace.default
    fs: filesystem.default
    sessionStore: session.store
    telemetry: telemetry.toolkit
```

依赖 ID 需替换为宿主实例图中的实际名称。配置沿用 `agent/acp-remote` 的
ACP 参数；没有 id 时使用 name。子代理名不区分大小写且必须唯一，ACP id 也
必须唯一。SessionStore、Workspace、FS、Telemetry 是必要能力；SessionEvents
缺省使用标准事件实现，以保证结果可从历史读取。SessionMCP、Sandbox 和
SessionRestore 沿用 ACP 的可选依赖，不需要启用 MCP。

每次委派使用独立历史（并发委派也不复用），继承父执行的租户、用户、工作目录
与审批目标，不读取父会话内容或消费父 steer。任务必须自包含。插件仅支持同步
执行；`async=true` 返回错误。超时默认 900 秒，请求 timeoutSeconds 可覆盖。
权限交给父审批 broker；拒绝 `autoApprove=true`。每次执行结束释放 ACP 子进程，
宿主退出需调用插件 ShutdownHooks，以取消正在执行的任务并关闭后端。

结果沿用现有子代理语义：显式 finish 决定 completed/blocked；没有 finish 时
返回 stopped 和最后一条 assistant 文本。角色目录扫描、人格文件、动态路由、
业务审批规则由宿主负责。

## 工具桥接：`mcp/tool-runtime`

只导入 `github.com/lengzhao/agentkit/plugins/mcp/toolruntime`，或直接使用
`runtime/mcpbridge.New(runtime)`。它不启动监听、不要求 Agent 或 Workspace。

```yaml
mcp.tools:
  use: mcp/tool-runtime
  deps:
    runtime: tools.default
```

构造结果是 `*mcpbridge.Bridge`，供宿主的 MCP 服务注入使用：

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
