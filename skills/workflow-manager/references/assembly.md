# Workflow 配置装配

`config.base.yaml` 已**默认装配**：`tools.runtime.workflow`（sibling 执行面：shell-bash + fs + MCP/OpenAPI）、`tool.workflow.default`（dirs 默认 `local:workflows → global:workflows`，deps 含 llm/subagent/telemetry/shell），并挂进 `tools.default` 的 `deps.dynamicTools`。**零配置即可用**；仅需定制（改 dirs、换执行面工具集、关 telemetry 等）时才在本地 YAML 覆盖同 id 实例，改后**重启** AgentKit 进程。

以下为 base 默认形态（覆盖时同 id 整颗替换）：

```yaml
tools.runtime.workflow:
  use: tools/runtime
  config:
    defaultTimeoutSeconds: 120
  deps:
    hooks: hooks.default
    tools:
      - tool.shell-bash.default
    toolPacks:
      - tool.fs-workspace.default
    dynamicTools:                  # tool step 可调 MCP/OpenAPI 工具
      - mcp.default
      - openapi.default
    policies:
      - policy.dangerous-shell.default

tool.workflow.default:
  use: tool/workflow
  deps:
    runtime: tools.runtime.workflow
    workspace: workspace.default
    shell: tool.shell-bash.default   # script step 走 StructuredShell；sandbox preset 下 shell-bwrap 同样实现
    llm: llm.default                 # model step
    subagent: subagent.default       # agent step
    telemetry: telemetry.toolkit

tools.default:
  deps:
    dynamicTools+:                 # ToolProvider 槽位，与 mcp.default / openapi.default 并列
      - tool.workflow.default
```

每个 workflow 文件暴露为 **`workflow__<name>`** 工具（`inputs` 即工具 InputSchema）；主 runtime 是 `tools/deferred` 时随 MCP/OpenAPI 走渐进披露三桥。

`telemetry` 必须是 **`telemetry.toolkit`**（进程内 Toolkit），不能填 `telemetry.default`（Langfuse exporter）。
