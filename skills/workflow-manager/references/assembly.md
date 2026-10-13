# Workflow 配置装配

改以下 YAML 后**重启** AgentKit 进程。

```yaml
tools.runtime.workflow:
  use: tools/runtime
  config:
    defaultTimeoutSeconds: 120
  deps:
    hooks: hooks.default
    tools:
      - tool.shell-bash.default
      # 追加 workflow 内 tool step 需要的静态/MCP/OpenAPI 工具
    policies:
      - policy.dangerous-shell.default

tool.workflow.default:
  use: tool/workflow
  config:
    dirs:
      - local:workflows
      - global:workflows
  deps:
    runtime: tools.runtime.workflow
    workspace: workspace.default
    shell: tool.shell-bash.default   # 推荐：script step 走 StructuredShell，不解析 bash 文案
    telemetry: telemetry.toolkit
    # llm: llm.gpt          # 含 model step
    # subagent: subagent.inprocess.default  # 含 agent step

tools.default:
  deps:
    tools+:
      - tool.workflow.default
```

`telemetry` 必须是 **`telemetry.toolkit`**（进程内 Toolkit），不能填 `telemetry.default`（Langfuse exporter）。
