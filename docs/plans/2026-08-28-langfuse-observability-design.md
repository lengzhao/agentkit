# Langfuse 可观测性设计

本文描述 AgentKit 第一版 Langfuse 直连可观测性方案，与 [roadmap.zh.md](../roadmap.zh.md) M3 观测项对齐。

## 目标

- 在 Langfuse UI 中还原一次 agent turn 的完整链路：用户输入、LLM generation、tool call、usage、错误。
- AgentKit 内部只依赖 `cap/telemetry` 抽象；runner/agent/tools 不直接依赖 Langfuse SDK。
- Session JSONL 仍是权威事实源；Langfuse 为旁路 best-effort 导出，失败不阻断 turn。

## 架构

```mermaid
flowchart TB
  inbound["Platform or Schedule"] --> runner["Runner"]
  runner --> loop["Loop Dispatch"]
  loop --> agent["Agent RunTurn"]
  agent --> session["Session JSONL"]
  agent --> llm["LLM Stream"]
  agent --> tools["Tools Runtime"]
  loop -.-> telemetry["cap/telemetry"]
  agent -.-> telemetry
  tools -.-> telemetry
  telemetry --> langfuse["telemetry/langfuse OTLP"]
  langfuse --> lfui["Langfuse UI"]
```

## Langfuse 映射

| AgentKit 概念 | Langfuse 概念 | 插入点 |
|---|---|---|
| 一次 RunTurn | Trace `agent.turn` | `loop.Dispatch` |
| LLM 调用 | Generation | `agent.runStep` |
| Tool 执行 | Tool observation（generation 子节点） | `tools.Execute` |
| Token 用量 | `usage_details` | `agent.recordUsage` |
| Turn 错误 | Trace/span error | `runner.dispatch` |
| 子 Agent 委派 | `tool.delegate` → `subagent.<name>` scope span → 子 generation/tool | `subagent/inprocess` + `tools.Execute` |

子 agent 与父 agent 共享同一 Langfuse trace（一次用户 turn）。层级规则：

- 父 agent 的 generation 挂在 trace 根下；同一步触发的 tool 挂在该 generation 下。
- `tool.delegate` 执行期间，子 agent 包在 `subagent.<name>` scope span 内；子 agent 的 generation/tool 挂在此 span 下。
- 每个 observation 的 metadata 携带 `agent_id` 与 `session_id`，用于在 UI 中区分 `coder` 与 `sub:researcher` 等活动。

Go 侧通过 `github.com/henomis/langfuse-go` SDK 写入 `{baseUrl}/api/public/ingestion`（Langfuse 无官方 Go SDK）。

## 配置

```yaml
telemetry.default:
  use: telemetry/none

telemetry.langfuse:
  use: telemetry/langfuse
  config:
    baseUrl: https://cloud.langfuse.com
    publicKeyRef: env:LANGFUSE_PUBLIC_KEY
    secretKeyRef: env:LANGFUSE_SECRET_KEY
    environment: production
    maxPayloadBytes: 8192
  deps:
    credentials: credentials.default

loop.default:
  deps:
    telemetry: telemetry.default

runner.default:
  deps:
    telemetry: telemetry.default
```

启用 Langfuse 时使用 `presets/langfuse.yaml` 覆盖 `telemetry.default` 为 `telemetry.langfuse`。

## 隐私与可靠性

- input/output 按 `maxPayloadBytes` 截断；敏感 key 名脱敏。
- 导出失败写 `slog.Warn`，不影响 Session 写入。
- `runner.Stop` 与 exporter `Shutdown` 负责 flush，避免短生命周期进程丢 trace。

## 后续

- `telemetry/otel`：通用 OTel 后端。
- `session/sqlite` + 成本汇总 CLI。
- schedule fire 独立 trace。
