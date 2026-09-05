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
