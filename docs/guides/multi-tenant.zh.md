# 多租户：一个进程服务多个群

一个常驻 agent 进程同时服务多个 Slack 频道（或飞书群、多个项目）时，要同时回答三个**互相独立**的问题：

| 问题 | 由谁回答 | 落点 |
|---|---|---|
| 这条消息接到哪段历史后面？ | `runner.config.sessionScope` 折叠 delivery SessionID | `runtime/session.ApplyScope` |
| 这句话是谁说的？ | `MessageEvent.UserID` → 事件信封 → 回放渲染 | `SessionEvent.UserID`、`runtime/session/derive.go` |
| 这个 turn 在哪个目录干活？ | effective SessionID 推出的**租户键** | `cap/tenant.Key`、`workspace/tenant` |

把三者拆开是这套设计的核心。**会话粒度与工作目录粒度可以分别决定**：一个群从"整群共用一段历史"改成"每个 thread 一段历史"，工作目录不会跟着分裂 —— 因为三种 scope 推出的租户键都是同一个。

```mermaid
flowchart LR
  Msg["Slack 消息<br/>channel=C001 user=U111 thread=17.9"]
  Msg -->|BuildDeliverySessionID| D["delivery SessionID<br/>slack:C001:t:17.9:u:U111"]
  D -->|runner sessionScope| E["effective SessionID<br/>slack:C001"]
  Msg -->|UserID| UID["U111"]
  E -->|"Loop 按此加锁 + session/store"| Hist["历史：一个 effective ID 一个 JSONL"]
  UID -->|"落在 user 事件信封上"| Attr["回放渲染 &lt;user id=\"U111\"&gt;"]
  E -->|"cap/tenant.Key"| TK["租户键<br/>slack:C001"]
  TK -->|workspace/tenant| Root["local 根<br/>~/.agentkit/tenants/slack_C001"]
  Root --> Runtime["sessions / agents / mcp / skills"]
  Root --> Work["work/ — fs 与 shell 操作区"]
  D -->|"OutboundEvent 仍用 delivery"| Reply["Platform.Send 投递"]
```

## 1. 会话隔离

Platform 只生成 **delivery SessionID**（最细粒度，含 channel / thread / user 路由信息）。Runner 按 `sessionScope` 折叠为 **effective SessionID**，Loop 按 effective 串行加锁，`session/store` 一个 effective ID 一个 JSONL。Outbound 仍回写 delivery ID，platform 据此投递。

Platform 侧：

```go
delivery := session.BuildDeliverySessionID("slack", channelID, threadTS, userID)
// MessageEvent.SessionID = delivery
```

Runner 侧（`config.base.yaml` 或 preset）：

```yaml
runner.default:
  config:
    sessionScope: channel   # channel | thread | user，默认 channel
```

| sessionScope | effective SessionID | 语义 |
|---|---|---|
| `channel` | `slack:C001` | 整群共用一段历史，靠用户归属区分发言人 |
| `thread` | `slack:C001:t:1712345678.9` | 每个 thread 一段历史，群里的顶层消息共用一段 |
| `user` | `slack:C001:u:U456` | 共享工作目录、各自私有历史 |

未知 scope 配置值回落到 `channel`。

## 2. 识别不同用户

`sessionScope: channel` 下整个频道共用一段历史，模型看到的是一串 user 消息。如果不标注发言人，它分不清"这是刚才那个人的追问"还是"另一个人的新需求"，也注意不到"我问的人和回答我的人不是同一个"。

所以 `UserID` 落在**事件信封**上（`SessionEvent.UserID`），而不是只存在于 turn 的 ctx 里 —— 重启后回放依然认得。`derive.go` 在回放时把 user 消息包成：

```
<user id="U111">
改一下 README
</user>
```

三条固定规则：

- **只标 user 消息。** 把 assistant 也标成提问者，回放时那句回答会读成那个人说的话。
- **逐条独立判定，只看该条事件自己的 `UserID`。** 不做"出现第二个人才追溯标注全部历史"—— 那会在对话刚变热闹的那一刻让整段 prompt cache 失效。
- **`UserID` 为空就完全不动。** CLI、timer、worker 这些单用户入口不设 `UserID`，回放结果与没有这套机制时逐字节相同。

platform 侧只需在 `MessageEvent.UserID` 填上发言人，其余自动。

## 3. 不同群不同工作目录

**为什么这一层不需要改任何下游插件**：`workspace.Service.Resolve(ctx, rel)` 本身带 ctx，而 fs 工具、shell、skills、AGENTS.md、mcp.json、`session/store`、subagent 定义目录**全部**经它解析路径，且都是每次调用现算、不缓存解析结果。所以把 local 根按租户分开，隔离就自动贯穿到全部文件访问。

### local 根 vs tool 工作区

local 根放**运行时与配置**，tool 只在 **`work/` 子目录**读写，避免 `rm`、`mv` 之类操作误伤 `sessions/` 或 `mcp.json`：

```
tenants/slack_C001/
├── sessions/          # session/store
├── agents/            # 子 agent 定义
├── mcp.json
├── skills/            # 租户私有 skill
└── work/              # fs / shell 的操作区（preset 默认 root）
```

`presets/multi-tenant.yaml` 把 `tool/fs-workspace`、`tool/shell-bash`、`prompt/section/agents-md` 的 `root` / `workDir` 都指到 `work`，与 coding preset 里「`.agentkit` 放运行时、`..` 落到项目根」是同一思路，但多租户**不允许 `..`**，所以用子目录而不是向上一级。

```yaml
workspace.default:
  use: workspace/tenant
  config:
    global: ~/.agentkit              # 全租户共享：技能库、子 agent 定义、公共 mcp.json
    localBase: ~/.agentkit/tenants   # 每租户一个子目录
    scope: local                     # 不带前缀的路径落在调用方自己的租户根下
    tenants:                         # 钉到已有项目时，local 根指向项目下的 .agentkit/
      "slack:C123ABC":
        root: ~/work/project-a/.agentkit

tool.fs-workspace.default:
  config:
    root: work                       # 只在此子目录读写
```

- **默认就是隔离的。** 没在 `tenants` 里列出的群走 `localBase/<租户键>`，新群接进来零配置。
- **`tenants` 的键是租户键，不是 SessionID。** 该频道下所有 thread、所有人共用这一条。
- **`global:` 是唯一共享的根。** 装一次的技能库对所有群可见。
- **没有 session 时落在 `localBase/_default`。** timer、cron、库直调不会掉进某个真实租户的目录里。
- **钉项目目录时指 `.agentkit/` 而不是项目根。** 运行时落在 `<项目>/.agentkit/`，工具在 `<项目>/.agentkit/work/` 操作；项目源码树不会被 `sessions/` 污染。

### 与 `workspace/default` 唯一的行为差异：`..` 不解析

`workspace/default` 允许向上一级：local 根是 `<项目>/.agentkit` 时，工具靠 `..` 落到项目根 —— 这是 coding preset 依赖的便利。

但租户根是**并列**的（`tenants/slack_C001` 与 `tenants/slack_C002` 互为兄弟），同一个豁免就成了越权通道：A 群一个 `../slack_C002` 就读写到 B 群。所以 `workspace/tenant` 全部走 `cap/workspace.ResolveRelStrict`，`..` 一律不解析，`global:` 也一样。

多租户 preset 进一步把 tool 根限制在 `work/` 子目录：`tool/fs-workspace` 在解析用户路径时还会拒绝 `../` 逃出 `work/`。路径只能落在 local/global 根下的子树里，不能靠 `..` 访问兄弟租户、父目录或 `sessions/`。

要让某个群在已有项目里干活：把 `tenants` 的 `root` 指到 `<项目>/.agentkit`，工具在 `work/` 下操作。若必须直接改项目源码树，用 [coding.yaml](../../presets/coding.yaml)（单租户 CLI）或自行把 `work/` 换成项目内其他子目录名。

## 4. 并发

`runner.maxConcurrentTurns` 默认 **64**，限制跨 effective session 的并行 turn 数。同一 effective session 内的顺序始终由 Loop 的 per-session 锁 + scheduler FIFO 保证。

单租户 coding CLI 若多个 session 共享同一工作区、担心并发写冲突，可在 L1 显式调低 `maxConcurrentTurns`。多租户场景下租户根已分开，默认 64 即可。

同一租户内若把 `sessionScope` 设为 `thread` 或 `user`，这些 effective session 仍共享一个工作目录 —— 它们之间的并发写冲突需要靠 scope 选择或调低并发上限控制。

## 5. MCP 客户端池按租户分槽

`tool/mcp` 的客户端池原先按 `server.Name` 建 key。多租户下两个群常各自声明一个同名 server（比如都叫 `filesystem`、各指向自己的工作区），共用一个槽位会让**每次交替调用都把对方的客户端踢掉并重启子进程**。

现在池按 `(租户键, server 名)` 建 key：两个租户各持自己的连接；租户内部仍按 config 指纹判定替换，改了 `mcp.json` 照常重连。

## 6. 上手

```sh
go run ./cmd/agent -config presets/multi-tenant.yaml

# 多租户 IM 通常是无人值守的，两个 overlay 可以叠：
go run ./cmd/agent -config presets/autonomous.yaml,presets/multi-tenant.yaml
```

`presets/multi-tenant.yaml` 只装内核。可与 `presets/slack.yaml`、`presets/feishu.yaml`、`presets/chat-api.yaml` 等 overlay 组合。platform 侧的全部义务就两件：

1. 用 `session.BuildDeliverySessionID` 生成 `MessageEvent.SessionID`（delivery，最细粒度）；
2. 在 `MessageEvent.UserID` 填上发言人。

`sessionScope` 由 runner 配置（默认 `channel`），不在 platform 重复实现。出站 `OutboundEvent` 回带 **delivery** SessionID，由 platform 解析投递目标。

`platform/chat-api` 在 workspace 下维护两层持久化：

| 层 | 路径 | 用途 |
|---|---|---|
| 会话索引 | `chat-api/conversations/<channel>.json` | 重启后恢复会话列表 |
| 展示历史 | `sessions/chat-api_<channel>_t_<conv>.jsonl` | 调试页 / messages API 按 conversation 隔离 |

Runner 仍按 `sessionScope` 折叠 delivery SessionID 做 Loop 加锁；**chat-api 的 agent 读写 session 时改用 delivery id**（`runtime/session.AgentStoreSessionID`），因此每个 `conversation_id` 对应独立 JSONL，新建会话不会读到 channel 级 `chat-api_<channel>.jsonl` 里其它会话或其它 agent 的历史。`DeriveMessages` 还会按当前 `agent_id` 过滤回放，避免同一会话文件里切换 agent 时串上下文。

每轮结束后 chat-api 仍会把 user/assistant 摘要镜像到同一 delivery session，供 messages API / 调试页展示。

## 7. 验收

| 测试 | 覆盖 |
|---|---|
| `multitenant_test.go` | 两个群写 `work/` 下同一相对路径 → 落在各自根下；某群钉到项目 `.agentkit/`；同群两人共用一段历史且各自具名 |
| `runtime/session/scope_test.go` | ApplyScope 三种粒度、legacy delivery 格式、CLI passthrough |
| `runtime/runner/runner_test.go` | scope 折叠调度键、outbound 仍用 delivery ID |
| `runtime/workspace/tenant_test.go` | 默认隔离、三种粒度同租户、pin 生效、`global:` 共享、`..` 越权被拒、无 session 落 `_default` |
| `runtime/session/attribution_test.go` | 只标 user 消息、无 `UserID` 不改变回放、重启后归属仍在、图片消息也具名 |
| `cap/tenant/tenant_test.go` | 租户键推导规则、目录名不可能变成 `..` |
| `config/presets_test.go` | preset 单独可构建，且与 `autonomous.yaml` 可叠加 |
