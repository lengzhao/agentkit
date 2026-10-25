# TurnEnvelope 迁移指南

本次重构将分散的 context key 与 `MessageEvent` 平行字段收敛为 `TurnEnvelope`。

## 破坏性变更

| 旧 API | 新 API |
|--------|--------|
| `MessageEvent.AgentID/PlatformID/UserID/Metadata` | `Envelope.AgentID/Route.Instance/Actor.UserID/Metadata` |
| `OutboundEvent.PlatformID` | `Route.DestinationID()`（`Instance` 缺省时回退 `Platform`） |
| `cap/delivery.Route`、`cap/schedule.Route` | `agentkit.RouteRef`；schedule 的 Actor、AgentID 分开保存 |
| `MessageEvent.SessionID` | `MessageEvent.Envelope.Conversation`（Runner 在 dispatch 前填充） |
| `MessageEvent.DeliverySessionID` | `MessageEvent.Envelope.Route`（Platform 入站设置） |
| `OutboundEvent.SessionID` | `OutboundEvent.Route` |
| `ctx.Value(KeySessionID)` 等 | `rctx.EnvelopeFromContext(ctx)` / `rctx.SessionIDFromContext(ctx)` |
| `LoopRequest.Envelope` | `LoopRequest.Event.Envelope`（单一来源） |
| `cap/tenant.FromContext` | `rctx.WorkspaceFromContext`（优先读 `Envelope.Workspace`） |
| `tool/send` deps `platform` | `sender`（`cap/delivery.Sender`，通常 `platform.default`） |
| `tool/chat-history` deps `platform` | `history`（`cap/chathistory.Router`） |
| agent/hook 重复 compaction 链 | `compaction/pipeline` 单点引用 |

路由辅助接口已收敛到 `runtime/rctx`：`runtime/delivery` 的 `ResolveRoute`、`NormalizeSessionID`、`OutboundRoute`、`OutboundRouteID` 转发函数已删除。原 `rctx.SlackSessionID`、`SlackSessionIDForScope` 改用通用的 `BuildDeliverySessionID` 和 `ApplyScope`。

## 插件依赖分层

**`cap/*` 只定义接口与 DTO，不放函数实现；实现放在 `runtime/*`。**

| 包 | `cap`（类型/接口） | `runtime`（实现） |
|----|-------------------|-------------------|
| session | `Todo`、`RunState` 等 | `LoadRunState`、事件读写与派生… |
| 路由上下文 | `agentkit.RouteRef`、`TurnEnvelope` | `runtime/rctx`：`EnvelopeFromContext`、`ApplyEnvelopeToContext`、`BuildSessionRoute`、`DeliveryParts`… |
| delivery | `Sender`、`Route`、`RouteInput` | 发送实现位于 `runtime/delivery`；`ResolveRoute`、`NormalizeSessionID`、`OutboundRoute` 位于 `runtime/rctx` |
| bind | —（无 `cap/bind`） | `Resolver`、`ResolveCtxValue`、`Default`（`runtime/bind`） |
| chathistory | `Provider`、`Router` | `RouterFromPlatform` 适配器 |
| compaction | `Service`、`Request`、`EventData`、`Preparation` 等 | `ApplyAll`、`Prepare`、`PruneToolResults`、`SerializeConversation`… |
| workspace | `Service`、`ScopeGlobal`/`ScopeLocal` | `ParseScoped`、`Resolve`、`ResolveRel`、`Static`… |
| credentials | `Store`、`EnvPairResolver`、`Secret`、`GlobalScope` | 实现在 `plugins/credentials`（密文 / scoped 查找）；见 [guides/credentials.zh.md](guides/credentials.zh.md) |
| permission | `Broker`、`Request`、`Reply`、DTO | `MatchReply`、`MarshalReply`、`EffectiveTimeout`、`CapabilityFrom`… |
| schedule | `Registry`、`Runtime`、`Engine`、`Cron`、`Job`、`SubmitFunc` | `ParseCron`、`NextFire`、`IsFireTurn`… |
| skill | `Registry`、`Descriptor`、`Content` | `RenderSkillLoaded`（`runtime/session/derive`）；`SanitizeRelativePath`、`ReadFile`（`runtime/skill`） |
| learning | `cap/learning`（`SkillProposer`、`ReviewHost`、`DreamSweepScheduler`）；`cap/memory`（`Capture`、`Service`）；`runtime/learning.MemoryEntry` | `ParseMemory`、`RenderMemory`、`MemoryStore`、`RunReview`、`ApplyCapture` |
| media | `ContentTypeAttachmentRef`（`runtime/media` 常量） | `IsImage`、`DataURL`、`LoadWorkspaceImage`、`FormatReadImageResult`… |

插件典型用法：

```go
import (
    capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
    "github.com/lengzhao/agentkit/runtime/rctx"
)

route, err := rctx.ResolveRoute(ctx, capsdelivery.RouteInput{SessionID: input.SessionID})
```

- **插件应优先依赖**：`agentkit`（根包语义接口）、`cap/*`（类型）、`runtime/*`（读/写上下文与路由的函数）、`pluginkit`
- **插件包之间禁止直接 import**：跨插件协作只通过配置图 `deps` 注入 `cap/*` 或根包接口；共享纯函数实现放 `runtime/*`，不要放在 `plugins/*` 子包互相引用
- **`runtime/*` 内核**：loop、runner、platform 等；少数写 session 事件的插件（如 `compaction/summary`、`agent/acp-remote`）同样使用 `runtime/session`

## Platform 入站

```go
delivery := rctx.BuildDeliverySessionID("slack", channelID, threadTS, userID)
event := rctx.WithInboundRoute(agentkit.MessageEvent{
    Envelope: agentkit.TurnEnvelope{Actor: agentkit.ActorRef{UserID: userID}},
    Message:    msg,
}, agentkit.SessionRouteInput{
    Platform:    "slack",
    DeliveryID:  delivery,
    ChannelID:   channelID,
    ThreadID:    threadTS,
    ReplyTo:     messageID, // 本轮回复锚点（如 IM message id）
    ScopeUserID: userID,
})
// Runner 会填充 Envelope.Conversation、Workspace
```

仅需 delivery id、无 ReplyTo 时可用 `rctx.WithDeliverySession`（内部仍走 `BuildSessionRoute`）。

## 出站

```go
agentkit.OutboundEvent{
    Route:      env.Route,
    Type:       typ,
    Data:       data,
}
```

## 直接调用 Loop.Dispatch

测试或内部调用必须设置 `Event.Envelope.Conversation`（及建议设置 `Workspace`、`Route`）：

```go
agentkit.LoopRequest{
    Event: agentkit.MessageEvent{
        Envelope: agentkit.TurnEnvelope{
            Conversation: "cli:default",
            Workspace:    "cli:default",
            Route:        rctx.SessionRoute("cli", "cli:default"),
        },
        Message: userMsg,
    },
}
```

## 平台类型与实例

`Route.Platform` 表示协议/codec 类型，例如 `slack`；`Route.Instance` 表示具体适配器，例如第二个 Slack 实例 `slack1`。`DestinationID()` 优先返回 Instance，旧数据缺省时回退 Platform。multiplex 入站统一写入 Instance，出站与历史查询只按该目标选择适配器；平台原始 delivery ID 保持不变，会话、租户和绑定键使用实例命名空间，避免同名频道相互污染。

Runner 从单一 Envelope 解析 Conversation、Workspace；核心层不再维护平行的 AgentID、PlatformID、UserID 或 Metadata。原 `SyncMessageEvent` 已删除。

## 持久化定时任务

`Job.Route` 保存 `RouteRef.Stable()`，不保留临时 ReplyTo。`Job.Actor` 与 `Job.AgentID` 保存归属。旧 `route.deliverySessionId/platformId/userId/agentId` 仅在 Job JSON 读取边界转换，写回统一输出新格式。reuse 模式的逻辑 session 同样使用实例命名空间。

`tool/chat-history` 只依赖 history router，通过纯路由解析获得 RouteRef；删除配置中的 `deps.delivery`。历史 Provider 收到的是完整 RouteRef。

## `/new` active mapping

`/new` 从 `Envelope.Route` 推导 stable mapping key（`rctx.ActiveEntryKeyFromContext`），写入 `sessions/<stable>/current.json` 指向新 `Conversation`。`Route` 与 `Workspace` 不变；Runner 下次入站时把 resolved `Conversation` 更新为新会话 id。

## L0 配置（`config.base.yaml`）

**`cap/*` → `runtime/*` 的包搬迁不需要改 L0**：YAML 只引用 plugin kind（`use:`）与 `deps` 实例 id，不引用 Go 包路径。`TestPresetsBuild` / `TestPresetsChainedBuild` 会校验 L0 与 preset 叠加后可 `build.Build[Runner]`。

与本次迁移相关的 L0 片段（仓库根目录 [config.base.yaml](../config.base.yaml) 已对齐）：

```yaml
# tool/send、tool/chat-history：deps 键名已迁移，仍注入 platform.multiplex 实例
tool.send.default:
  use: tool/send
  deps:
    sender: platform.default
    workspace: workspace.default

tool.chat-history.default:
  use: tool/chat-history
  deps:
    history: platform.default

# compaction：hook 与 agent 共用 pipeline 实例，避免重复链
hook.before-step.default:
  use: hook/before-step
  deps:
    services:
      - compaction.pipeline.default

agent.assistant.default:
  use: agent/coding
  deps:
    compaction:
      - compaction.pipeline.default

# telemetry：接口在 cap/telemetry，实现插件 telemetry/none；接 Langfuse 用 presets/langfuse.yaml 覆盖
runner.default:
  deps:
    telemetry: telemetry.default

loop.default:
  deps:
    telemetry: telemetry.default

telemetry.default:
  use: telemetry/none
```

可选能力仍保留在 L0 注释中（如 `learning.dreamSweep`），启用时取消注释并挂到 `runner.deps.schedules`；见 [guides/learning-dreaming.zh.md](guides/learning-dreaming.zh.md) §7。

同协议的多个适配器由 multiplex 在 Receive 启动前通过可选的 `PlatformInstanceSetter` 注入实例名，原生 slash、卡片命令和撤回停止任务均使用该身份。显式 `slack1:C2` 先选择实例，再转换成协议地址 `slack:C2`；平台编解码器不会收到实例前缀。
