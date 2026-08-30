# AgentKit 路线图

本文回答：**接下来做什么、按什么顺序、做到什么算完**。改完插件后用 [核对命令](#核对文档与代码) 复查。

## 现状

64 个已注册 kind。spine、Coding 闭环、自主运行、网络工具、多租户、IM/HTTP 接入（slack/feishu/chat-api）、Langfuse、子 Agent、MCP、ACP 均已落地。

**缺口**：

| 缺口 | 影响 |
|---|---|
| `StartStop` / `Root.Stop` 空实现 | daemon 退出无法有序 flush |
| follow-up inbox 纯内存 | 崩溃后排队消息丢失 |
| `hook/llm-request`、`BeforeStep` 决策类型 | 请求改写与步级 reject |
| `session/sqlite` + `sessionquery` | 无法跨 session 检索 |
| `platform/http`、`platform/rpc` | 通用 HTTP/RPC 接入（`platform/http` 已落地：服务 DefaultServeMux） |
| `policy/network-deny` | SSRF 仍在 `web/http-fetch` 内 |
| OS 级沙箱 | 暂缓，靠 policy + approval |

## M2 — 守护收尾（下一步）

`StartStop` 真实关停、follow-up 持久化、`policy/network-deny`。

**验收**：`Ctrl-C` 关停 daemon 时 session 完整、follow-up 可恢复。

## M3 — 可运营（剩余）

`platform/http` / `platform/rpc`、`telemetry/otel`、成本汇总 CLI、`session/sqlite` + `tool/session-query`。

## M4 — 并行与多 Agent（需求驱动）

subagent 并行 fan-out、`loop/harness`、Prompt Context 分离、permission preset、Code Mode。

## 小项

`llm/replay`、`credentials/file`、`hook/before-tool` / `hook/after-tool` 独立注册。

## 专题文档

| 主题 | 文档 |
|---|---|
| 自主运行 | [guides/autonomous-run.zh.md](guides/autonomous-run.zh.md) |
| 子 Agent | [guides/subagent.zh.md](guides/subagent.zh.md) |
| 多租户 | [guides/multi-tenant.zh.md](guides/multi-tenant.zh.md) |
| 网络 / MCP | [guides/tools.zh.md](guides/tools.zh.md) |
| 人机交互 | [guides/platform-interaction.zh.md](guides/platform-interaction.zh.md) |

## 核对文档与代码

```sh
grep -rhno 'pluginkit.Register("[^"]*"' --include='*.go' . | sed 's/.*Register("//;s/"$//' | sort -u

comm -23 <(sed -n '/^## 3\./,/^## 4\./p' docs/plugin-catalog.zh.md \
            | grep -o '`[a-z][a-z0-9-]*/[a-z0-9-]*`' | tr -d '`' | sort -u) \
         <(grep -rhno 'pluginkit.Register("[^"]*"' --include='*.go' . \
            | sed 's/.*Register("//;s/"$//' | sort -u)
```
