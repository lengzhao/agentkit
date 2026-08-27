# AgentKit 路线图

本文只回答一个问题：**接下来做什么、按什么顺序、做到什么算完**。

现状基线来自对代码的一次逐项核对（2026-08-25），核对方法见 [维护](#怎么维护这份路线图)——不是照着旧文档抄的，因为旧文档已经漂移过一次（见 [§0.3](#03-已知的文档漂移已在-m0-修正)）。

相关文档：[plugin-catalog.zh.md](plugin-catalog.zh.md)（kind 目录）、[reference-analysis.zh.md](reference-analysis.zh.md)（业界共性）、[plugin-interface-comparison.zh.md](plugin-interface-comparison.zh.md)（接口级取长补短）。

## 0. 现状基线（2026-08-25）

### 0.1 能力覆盖

按 [reference-analysis §9](reference-analysis.zh.md#9-提炼的通用能力清单) 的 26 项通用能力对账，共 63 个已注册 kind：

| 段 | 能力 | 状态 |
|---|---|---|
| §9.1 Spine | Runner / Platform / Loop / Agent / Session / Prompt / Tools / LLM | **全部落地** |
| §9.2 执行 | Filesystem / Shell / Policy / Approval | **全部落地** |
| §9.3 上下文与记忆 | Compaction / Skills / AGENTS.md / Credentials / Settings | 落地（Credentials 仅 `env`，无 `file`） |
| §9.4 协作与编排 | Subagent（串行）/ Commands / Web / User Questions | 落地（Web 见 [M1](#m1--网络能力已落地)） |
| | **Sandbox** | **暂缓**（见 [roadmap](roadmap.zh.md#暂缓os-级沙箱)） |
| §9.5 平台与观测 | Session Persistence（jsonl） | 落地（`session/sqlite` 未做） |
| | **Session Query / Telemetry / Host Adapters** | **未做** |
| §9.6 专项 | Terminal/PTY、LSP、Workflow、Jobs、Plan Mode… | 未做 |

一句话：**spine 与"单机 coding 闭环"已经完整，缺口集中在"把它变成可对外运营的东西"**——M1 之后已经伸得出本机（web 抓取 + 搜索），但仍然看不见（无 telemetry / query）、进不来（只有 CLI / timer / worker / multiplex）。OS 级沙箱（landlock / seatbelt 等）**短期不做**，无人值守场景继续靠 policy 字符串匹配与 tool 内硬约束。

`cap/` 下仍是空壳（`struct{}` 占位）的 3 个：`process`、`sessionquery`、`telemetry`（`cap/web` 已在 M1 填掉）。**不预留 `cap/sandbox`**——真要做隔离时再引入接口，避免占位代码与 catalog 误导。

### 0.2 接口层真缺口

以下经代码核对确认仍缺，是 [plugin-interface-comparison §5.2](plugin-interface-comparison.zh.md#52-建议从-dsh-吸收的设计) 里**没有过期**的部分：

| 缺口 | 现状 | 影响 |
|---|---|---|
| `StartStop` 生命周期 | `runtime/runner/runner.go` 的 `Root.Stop` 是 `return nil` | daemon / cron 常驻模式退出时没有有序 flush / 关闭时机 |
| follow-up inbox 持久化 | `runtime/loop/control.go` 的队列纯内存 | 进程挂掉，排队中的 follow-up 直接丢，session 日志里查不到它存在过 |
| `BeforeStep` 无决策类型 | `BeforeStepHook` 只能返回 `error` | 无法表达 "reject 这一步" vs "注入消息后继续"，只能用 error 中断整个 turn |
| `hook/llm-request` | 无接口 | 无法在请求出栈前改写（模型路由、prompt cache 标记、按 session 换模型） |
| `Session.Fork` / `SessionHeader` | 无 | 无法分支重跑；session 元数据（cwd、parent、preset）只能塞进事件 |
| Prompt Context 与 Section 未分离 | 都走 `Section.Build` | 动态用户态快照与 system 段混在一起 |
| permission preset | 靠 preset 手拼 policy 实例 | 没有"只读审查 agent"这种一等概念 |

### 0.3 已知的文档漂移（已在 M0 修正）

记录在此以免二次踩坑：`plugin-interface-comparison.zh.md` 曾把 `plugin_command.go` / Compaction / Settings 标为"未实现或已删"（三者均已落地），把 `TurnInput.Control` + 根包 `SessionControl` 当作现存接口（已重构为 ctx 值 + `runtime/agent` 私有窄接口），把 request-error 恢复标为"无接口"（provider 与 agent 两层 retry 均已落地）。

## M0 — 文档去漂移（本轮完成）

**目标**：让 README 阅读顺序上的三篇文档与代码一致，并把路线图集中到本文。

- `plugin-interface-comparison.zh.md`：重写 §3.3 控制面（`TurnInput{Message, Emit}` + `KeySessionControl` + `runtime/loop.Control` / `runtime/agent.turnControl`）；修 §3.1 `platform/multiplex`、§3.9 错误恢复、§4 三行过期、§5.2 优先级表（标注已完成项、拆分"超时 ✅ / 重试 ❌"、修正 `Inject` 落点）。
- `reference-analysis.zh.md`：§1 默认形态、§4.1 增加"AgentKit 状态"列、§9 各段标注落地状态、§11 索引补齐。
- `plugin-catalog.zh.md` §6：Phase 表指向本文。

**验收**：两篇对比文档中出现的每个 AgentKit 侧断言，都能用 `§5` 的核对命令验证。

## M1 — 网络能力（已落地）

**为什么先做**：成本最低、对单次任务成功率提升最直接，且完全不动 spine——新增 provider + tool，不改任何现有接口。实现细节见 [web.zh.md](web.zh.md)。

| 项 | kind | 落点 |
|---|---|---|
| HTTP 抓取 | `web/http-fetch` | 填 `cap/web` 的 `Fetch`；HTML → 文本、大小上限、超时、重定向与私网地址约束 |
| 搜索 | `web/exa-search` | 填 `cap/web` 的 `Search`；key 走 `credentials/env`，与 `llm/openai-compatible` 的 `apiKeyRef` 同构 |
| 无网络替身 | `web/scripted-fetch`、`web/scripted-search` | 给测试与冒烟 preset 用，同 `llm/scripted` |
| 模型可见工具 | `tool/web-fetch`、`tool/web-search` | 照 `plugins/tool/skill.go` 的写法 |
| 结构化提问 | `tool/ask-user` | Loop HIL + platform 渲染/回传，见 [platform-interaction.zh.md](platform-interaction.zh.md) |
| Preset | `presets/web.yaml`、`presets/web-smoke.yaml` | 后者不需要任何 key，也不发真实请求 |

**三个待定设计点的落定**：

| 待定项 | 决定 | 理由 |
|---|---|---|
| 搜索 provider 选谁 | **Exa**（`web/exa-search`） | `plugin-catalog.zh.md` §3.6 早就预留了这个 kind 名；wire 格式（`x-api-key` header、camelCase body、`contents.highlights`）是查官方文档确认的，不是猜的。接口是 `web.Searcher`，再加 Brave / Tavily 只是多一个 kind |
| `tool/ask-user` 无人可问时的降级 | **既不阻塞也不报错**：返回 `answered:false` + `reason` + `guidance`，让模型自己拍板并声明假设 | 与本仓库一贯的"deny 是模型可读的结果而不是 error"一致（`tool_builder.go` 把 handler error 也转成文本 result）。headless platform `Interactive=false` 时自动降级 |
| SSRF 边界要不要做成 `policy/network-deny` | **先落在 `web/http-fetch` 里**（dial 时校验解析后的 IP + scheme 白名单 + host allow/deny + 重定向重校验），`policy/network-deny` 不在本期创建 | 私网校验必须发生在 DNS 解析之后才挡得住重定向与 DNS rebinding，这个位置只有 provider 有。它是 M2 那个 policy 的雏形，不是它的替代 |

HIL **没有复用 `cap/approval` 插件来回答 question**。`approval` 插件只处理 policy `DecisionAsk` 的自动裁决（`auto-allow` / `auto-deny`）；`ask_user` 与需人输入的 ask 走 `PermissionBroker`。无人值守 preset 挂 `approval/auto-allow`——若复用来答 question，agent 问的每个问题都会被默默答成"是"。

**已落地**（见 [platform-interaction.zh.md](platform-interaction.zh.md)）：`cap/permission` + Loop `PermissionBroker`，`ask_user` 与 `DecisionAsk`（无 `approval` 插件时）共用 pending；`auto-allow` 仍在 runtime 短路，不回答 question kind。

**验收**（已通过）：`presets/web.yaml,presets/web-smoke.yaml` 跑通"搜索 → 抓取 → 提问降级 → 引用来源回答"；`env -u EXA_API_KEY` 下实例图照常构建、`web/http-fetch` 单独可用、搜索返回一句模型可读的"没有 key"；抓取/搜索失败均为模型可读结果，turn 不中断。

## M1.5 — 多租户内核（已落地）

**为什么插在这里**：一个进程服务多个 Slack 频道时，缺口不在 spine，而在"每个群在哪个目录干活"。`workspace.Service.Resolve` 本来就带 ctx，且全部文件访问都经它 —— 所以这一层是加插件，不改接口。详见 [multi-tenant.zh.md](multi-tenant.zh.md)。

| 项 | 落点 | 说明 |
|---|---|---|
| 租户键推导 | `cap/tenant` | 从 `SessionID` 取平台段 + 第一个路由段；三种会话粒度落在同一租户上 |
| 按租户分工作目录 | `workspace/tenant` | `global` 共享 + `local` 一租户一根；默认隔离，`tenants` 可钉到已有项目 |
| 越权边界 | `cap/workspace.ResolveRelStrict` | 租户根之间并列，`..` 一律不解析（与 `workspace/default` 的唯一行为差异） |
| 用户归属 | `SessionEvent.UserID` + `derive.go` | 共用会话时回放渲染 `<user id="U123">`；`UserID` 为空则逐字节不变 |
| 会话粒度 | `session.SlackSessionIDForScope` | 整群共用 / 按 thread / 按人 |
| MCP 池分槽 | `tool/mcp` | 客户端池按 `(租户键, server 名)` 建 key，两个租户不再互相踢连接 |
| Preset | `presets/multi-tenant.yaml` | 放开 `maxConcurrentTurns`（租户根分开后共享工作区的前提不再成立） |

**未做**：`platform/slack` 本体仍在 M3。入站 platform 的义务只有两件 —— 生成 `SessionID`、填 `MessageEvent.UserID`。

## M2 — 守护收尾

**为什么排第二**：`platform/worker`、follow-up 队列与常驻 preset 已经存在，但 `Root.Stop` 仍是空实现、follow-up inbox 纯内存——进程退出时 session 与排队消息不可靠。这与 OS 级隔离无关，应先行补齐。

| 项 | kind | 说明 |
|---|---|---|
| 生命周期 | `StartStop` + `Root.Stop` 真实实现 | 有序停组件、flush session |
| 队列持久化 | follow-up inbox 事件 | 常驻模式崩溃后可恢复排队消息 |
| 网络裁决 | `policy/network-deny` | 把 M1 落在 `web/http-fetch` 里的 SSRF 约束提升为 policy 层裁决（shell 出站仍靠 allowlist，无 OS 沙箱） |

**验收**：`Ctrl-C` 关停 daemon 时 session 文件完整、follow-up 队列可恢复；`policy/network-deny` 能拒绝模型可见的网络类 tool 调用。

### 暂缓：OS 级沙箱

landlock / seatbelt、`sandbox/*`、`process/sandbox`、`fs/sandbox` **短期不规划**。原因：实现与维护成本高，当前单机 coding / 可控 preset 下 policy + approval 足够；真要做时再设计 `cap/*` 接口，不在仓库留空壳。

无人值守（`presets/autonomous.yaml` + `approval/auto-allow`）的防线仍是 `policy/shell-allowlist`、`policy/path-denylist` 与各 tool provider 内约束——文档里明确这是**已知限制**，不是"下一 sprint 就填"的占位 kind。

## M3 — 可运营（观测 + 接入）

**依赖 M2 生命周期部分**：没有有序关停就对外开端口不合适；OS 沙箱不是 M3 前置条件。

| 项 | kind | 说明 |
|---|---|---|
| HTTP 接入 | `platform/http`（后续 `platform/rpc`） | 与 `platform/multiplex` 组合，多入口并存 |
| 观测 | `telemetry/none`、`telemetry/otel` | 填 `cap/telemetry`；`usage` 事件目前只被 `runtime/agent/budget.go` 消费，没人汇总 |
| 成本汇总 | — | 回答"昨晚 cron 花了多少 token / 多少钱" |
| 检索 | `session/sqlite` + `cap/sessionquery` + `tool/session-query` | 跨 session 检索与 lineage |

**验收**：HTTP 接入能创建 session、投递消息、流式取回；跑完一轮 cron 后能从命令行问出总 token 与按 agent 的分布。

## M4 — 并行与多 Agent（需求驱动）

这些是地基，**没有具体需求牵引时容易过度设计**，建议等 M1–M3 冒出真实痛点再插入：

- subagent 并行 fan-out（在 `Run` 旁边**加** `Start` / `Handle`，先解决共享 workspace 写冲突——与 `runner.maxConcurrentTurns` 默认 1 是同一个问题，见 [subagent.zh.md §7](subagent.zh.md#7-本期不做)）
- `loop/harness` + AgentSet 多 lane
- `hook/llm-request`（`BeforeLLMRequest` 接口）
- `BeforeStep` → `StepDecision`（reject / enter）
- `Session.Fork` + `SessionHeader`
- `subagent/rpc`、Prompt Context 分离、permission preset、Code Mode

## 随时可插入的小项

- **`llm/replay`** — 录制真实 session 回放成测试。目前所有端到端验证只能手写 `llm/scripted` 脚本（见 `presets/*-smoke.yaml`），成本低、杠杆高。
- **`credentials/file`** — 目前只有 `env`。
- **`hook/before-tool` / `hook/after-tool` 独立插件** — 接口已在 `plugin_hook.go`，但只注册了 `hook/before-step` 与 `hook/turn-continue`。

## 怎么维护这份路线图

改完插件后，用下面两条命令核对文档与代码是否还一致（M0 的漂移就是靠它发现的）：

```sh
# 实际注册的 kind 清单
grep -rhno 'pluginkit.Register("[^"]*"' --include='*.go' . | sed 's/.*Register("//;s/"$//' | sort -u

# 文档 §3 列了但代码里没有的 kind（事件名如 run/finish 是预期的假阳性）
comm -23 <(sed -n '/^## 3\./,/^## 4\./p' docs/plugin-catalog.zh.md \
            | grep -o '`[a-z][a-z0-9-]*/[a-z0-9-]*`' | tr -d '`' | sort -u) \
         <(grep -rhno 'pluginkit.Register("[^"]*"' --include='*.go' . \
            | sed 's/.*Register("//;s/"$//' | sort -u)
```

`cap/` 下哪些还是空壳：

```sh
for d in cap/*/; do echo "$d: $(grep -h 'struct{}' $d*.go 2>/dev/null | wc -l)"; done
```
