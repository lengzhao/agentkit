# TurnEnvelope 迁移指南

本次重构将分散的 context key 与 `MessageEvent` 平行字段收敛为 `TurnEnvelope`。

## 破坏性变更

| 旧 API | 新 API |
|--------|--------|
| `MessageEvent.SessionID` | `MessageEvent.Envelope.Conversation`（Runner 在 dispatch 前填充） |
| `MessageEvent.DeliverySessionID` | `MessageEvent.Envelope.Route`（Platform 入站设置） |
| `OutboundEvent.SessionID` | `OutboundEvent.Route` |
| `ctx.Value(KeySessionID)` 等 | `session.EnvelopeFromContext(ctx)` / `session.SessionIDFromContext(ctx)` |
| `LoopRequest.Envelope` | `LoopRequest.Event.Envelope`（单一来源） |
| `cap/tenant.FromContext` | `session.WorkspaceFromContext`（优先读 `Envelope.Workspace`） |
| `tool/send` deps `platform` | `sender`（`cap/delivery.Sender`，通常 `platform.default`） |
| `tool/chat-history` deps `platform` | `history`（`cap/chathistory.Router`） |
| agent/hook 重复 compaction 链 | `compaction/pipeline` 单点引用 |

## 插件依赖分层

**`cap/*` 只定义接口与 DTO，不放函数实现；实现放在 `runtime/*`。**

| 包 | `cap`（类型/接口） | `runtime`（实现） |
|----|-------------------|-------------------|
| session | `Todo`、`RunState`、`DeliveryParts` 等 | `EnvelopeFromContext`、`LoadRunState`、`ApplyEnvelopeToContext`… |
| delivery | `Sender`、`Route`、`RouteInput` | `ResolveRoute`、`NormalizeSessionID`、`OutboundRoute`… |
| bind | `Resolver` | `ResolveCtxValue`、`Default` |
| chathistory | `Provider`、`Router` | `RouterFromPlatform` 适配器 |
| compaction | `Service`、`Request`、`EventData`、`Preparation` 等 | `ApplyAll`、`Prepare`、`PruneToolResults`、`SerializeConversation`… |
| workspace | `Service`、`ScopeGlobal`/`ScopeLocal` | `ParseScoped`、`Resolve`、`ResolveRel`、`Static`… |
| credentials | `Store`、`Secret` | `WithSecrets`、`SecretFromContext`、`EnvKey` |
| permission | `Broker`、`Request`、`Reply`、DTO | `MatchReply`、`MarshalReply`、`EffectiveTimeout`、`CapabilityFrom`… |
| schedule | `Registry`、`Runtime`、`Job`、`SubmitFunc` | `ParseCron`、`NextFire`、`IsFireTurn`、`JobKind`… |
| skill | `Registry`、`Descriptor`、`Content` | `RenderLoaded`、`SanitizeRelativePath`、`ReadFile`、`RunScript` |
| media | `ContentTypeAttachmentRef`（常量） | `IsImage`、`DataURL`、`LoadWorkspaceImage`、`FormatReadImageResult`… |

插件典型用法：

```go
import (
    capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
    rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
    "github.com/lengzhao/agentkit/runtime/session"
)

route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{SessionID: input.SessionID})
```

- **插件应优先依赖**：`agentkit`、`cap/*`（类型）、`runtime/*`（读/写上下文与路由的函数）、`pluginkit`
- **`runtime/*` 内核**：loop、runner、platform 等；少数写 session 事件的插件（如 `compaction/summary`、`agent/acp-remote`）同样使用 `runtime/session`

## Platform 入站

```go
delivery := session.BuildDeliverySessionID("slack", channelID, threadTS, userID)
event := common.WithInboundRoute(agentkit.MessageEvent{
    PlatformID: "slack",
    UserID:     userID,
    Message:    msg,
}, session.SessionRouteInput{
    Platform:    "slack",
    DeliveryID:  delivery,
    ChannelID:   channelID,
    ThreadID:    threadTS,
    ReplyTo:     messageID, // 本轮回复锚点（如 IM message id）
    ScopeUserID: userID,
})
// Runner 会填充 Envelope.Conversation、Workspace
```

仅需 delivery id、无 ReplyTo 时可用 `common.WithDeliverySession`（内部仍走 `BuildSessionRoute`）。

## 出站

```go
agentkit.OutboundEvent{
    Route:      env.Route,
    PlatformID: env.Route.Platform,
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
            Route:        agentkit.SessionRoute("cli", "cli:default"),
        },
        Message: userMsg,
    },
}
```

## `/new` active mapping

`/new` 从 `Envelope.Route` 推导 stable mapping key（`session.ActiveEntryKeyFromContext`），写入 `sessions/<stable>/current.json` 指向新 `Conversation`。`Route` 与 `Workspace` 不变；Runner 下次入站时把 resolved `Conversation` 更新为新会话 id。
