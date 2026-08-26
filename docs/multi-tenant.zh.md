# 多租户：一个进程服务多个群

一个常驻 agent 进程同时服务多个 Slack 频道（或飞书群、多个项目）时，要同时回答三个**互相独立**的问题：

| 问题 | 由谁回答 | 落点 |
|---|---|---|
| 这条消息接到哪段历史后面？ | Platform 生成的 `SessionID` | `runtime/session.SlackSessionIDForScope` |
| 这句话是谁说的？ | `MessageEvent.UserID` → 事件信封 → 回放渲染 | `SessionEvent.UserID`、`runtime/session/derive.go` |
| 这个 turn 在哪个目录干活？ | `SessionID` 推出的**租户键** | `cap/tenant.Key`、`workspace/tenant` |

把三者拆开是这套设计的核心。**会话粒度与工作目录粒度可以分别决定**：一个群从"整群共用一段历史"改成"每个 thread 一段历史"，工作目录不会跟着分裂 —— 因为三种粒度推出的租户键都是同一个。

```mermaid
flowchart LR
  Msg["Slack 消息<br/>channel=C001 user=U111 thread=17.9"]
  Msg -->|SessionIDForScope| SID["SessionID<br/>slack:C001:t:17.9"]
  Msg -->|UserID| UID["U111"]
  SID -->|"Loop 按此加锁 + session/store"| Hist["历史：一个 SessionID 一个 JSONL"]
  UID -->|"落在 user 事件信封上"| Attr["回放渲染 &lt;user id=\"U111\"&gt;"]
  SID -->|"cap/tenant.Key<br/>取平台段 + 第一个路由段"| TK["租户键<br/>slack:C001"]
  TK -->|workspace/tenant| Root["local 根<br/>~/.agentkit/tenants/slack_C001"]
  Root --> FS["fs / shell / skills / AGENTS.md / mcp / sessions"]
```

## 1. 会话隔离

`SessionID` 对 Loop 与 Agent 是不透明的路由键，只有 platform 插件能编解码。Loop 按 `SessionID` 串行加锁，`session/store` 一个 `SessionID` 一个 JSONL 文件 —— 所以**不同群天然是不同会话**，这一层不需要额外机制。

platform 侧用 `SlackSessionIDForScope` 表达粒度：

```go
id := session.SlackSessionIDForScope(scope, channelID, threadTS, userID)
```

| scope | SessionID | 语义 |
|---|---|---|
| `ScopeChannel` | `slack:C001` | 整群共用一段历史，靠用户归属区分发言人 |
| `ScopeThread` | `slack:C001:t:1712345678.9` | 每个 thread 一段历史，群里的顶层消息共用一段 |
| `ScopeUser` | `slack:C001:u:U456` | 共享工作目录、各自私有历史 |

未知 scope 回落到 `ScopeThread` 而不是报错：会话被过度共享是隐私问题，两个候选里 thread 是更窄的那个。

## 2. 识别不同用户

`ScopeChannel` 下整个频道共用一段历史，模型看到的是一串 user 消息。如果不标注发言人，它分不清"这是刚才那个人的追问"还是"另一个人的新需求"，也注意不到"我问的人和回答我的人不是同一个"。

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

```yaml
workspace.default:
  use: workspace/tenant
  config:
    global: ~/.agentkit              # 全租户共享：技能库、子 agent 定义、公共 mcp.json
    localBase: ~/.agentkit/tenants   # 每租户一个子目录
    scope: local                     # 不带前缀的路径落在调用方自己的租户根下
    tenants:                         # 只有要钉到已有项目目录时才列
      "slack:C123ABC":
        root: ~/work/project-a
```

- **默认就是隔离的。** 没在 `tenants` 里列出的群走 `localBase/<租户键>`，新群接进来零配置。
- **`tenants` 的键是租户键，不是 SessionID。** 该频道下所有 thread、所有人共用这一条。
- **`global:` 是唯一共享的根。** 装一次的技能库对所有群可见。
- **没有 session 时落在 `localBase/_default`。** timer、cron、库直调不会掉进某个真实租户的目录里。

### 与 `workspace/default` 唯一的行为差异：`..` 不解析

`workspace/default` 允许向上一级：local 根是 `<项目>/.agentkit` 时，工具靠 `..` 落到项目根 —— 这是 coding preset 依赖的便利。

但租户根是**并列**的（`tenants/slack_C001` 与 `tenants/slack_C002` 互为兄弟），同一个豁免就成了越权通道：A 群一个 `../slack_C002` 就读写到 B 群。所以 `workspace/tenant` 全部走 `cap/workspace.ResolveRelStrict`，`..` 一律不解析，`global:` 也一样。

代价是不能再靠 `..` 落到项目根 —— 要让某个群在已有项目里干活，把该租户的 `root` 直接指向项目目录（见上面的 `tenants`）。

## 4. 并发

`runner.maxConcurrentTurns` 默认 1，理由写在 `config.base.yaml` 里：不同 session 的 turn 共享同一个工作区，两个 agent 并发跑 `go build` 或改同一个文件是真实风险。

**租户根分开之后这个前提不再成立**，所以 `presets/multi-tenant.yaml` 把它放开到 4。同一 session 内的顺序始终由 Loop 的 per-session 锁保证。

同一租户内若配了多种会话粒度（例如按 thread 建会话），这些 session 仍然共享一个工作目录 —— 它们之间的并发写冲突风险和单租户时一样，需要靠粒度选择或并发上限控制。

## 5. MCP 客户端池按租户分槽

`tool/mcp` 的客户端池原先按 `server.Name` 建 key。多租户下两个群常各自声明一个同名 server（比如都叫 `filesystem`、各指向自己的工作区），共用一个槽位会让**每次交替调用都把对方的客户端踢掉并重启子进程**。

现在池按 `(租户键, server 名)` 建 key：两个租户各持自己的连接；租户内部仍按 config 指纹判定替换，改了 `mcp.json` 照常重连。

## 6. 上手

```sh
go run ./cmd/agent -config presets/multi-tenant.yaml

# 多租户 IM 通常是无人值守的，两个 overlay 可以叠：
go run ./cmd/agent -config presets/autonomous.yaml,presets/multi-tenant.yaml
```

`presets/multi-tenant.yaml` 只装内核。**入站 platform 需自行接入**（`platform/slack` 尚未实现，见 [roadmap.zh.md](roadmap.zh.md) M3）。platform 侧的全部义务就两件：

1. 用 `session.SlackSessionIDForScope` 生成 `MessageEvent.SessionID`；
2. 在 `MessageEvent.UserID` 填上发言人。

出站 `OutboundEvent` 回带同一个 `SessionID`，由 platform 自己解析投递目标。

## 7. 验收

| 测试 | 覆盖 |
|---|---|
| `multitenant_test.go` | 两个群写同一个相对路径 → 落在各自根下；某群钉到项目目录；同群两人共用一段历史且各自具名 |
| `runtime/workspace/tenant_test.go` | 默认隔离、三种粒度同租户、pin 生效、`global:` 共享、`..` 越权被拒、无 session 落 `_default` |
| `runtime/session/attribution_test.go` | 只标 user 消息、无 `UserID` 不改变回放、重启后归属仍在、图片消息也具名 |
| `cap/tenant/tenant_test.go` | 租户键推导规则、目录名不可能变成 `..` |
| `config/presets_test.go` | preset 单独可构建，且与 `autonomous.yaml` 可叠加 |
