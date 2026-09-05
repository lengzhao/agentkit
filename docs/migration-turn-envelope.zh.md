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
| bind | —（无 `cap/bind`） | `Resolver`、`ResolveCtxValue`、`Default`（`runtime/bind`） |
| chathistory | `Provider`、`Router` | `RouterFromPlatform` 适配器 |
| compaction | `Service`、`Request`、`EventData`、`Preparation` 等 | `ApplyAll`、`Prepare`、`PruneToolResults`、`SerializeConversation`… |
| workspace | `Service`、`ScopeGlobal`/`ScopeLocal` | `ParseScoped`、`Resolve`、`ResolveRel`、`Static`… |
| credentials | `Store`、`Secret` | `WithSecrets`、`SecretFromContext`、`EnvKey` |
| permission | `Broker`、`Request`、`Reply`、DTO | `MatchReply`、`MarshalReply`、`EffectiveTimeout`、`CapabilityFrom`… |
| schedule | `Registry`、`Runtime`、`Job`、`SubmitFunc` | `ParseCron`、`NextFire`、`IsFireTurn`、`JobKind`… |
| skill | `Registry`、`Descriptor`、`Content` | `RenderLoaded`、`SanitizeRelativePath`、`ReadFile`、`RunScript` |
| learning | `agentkit.CommandProvider`（deps）、`runtime/learning.MemoryEntry` | `ParseMemory`、`RenderMemory` |
| media | `ContentTypeAttachmentRef`（`runtime/media` 常量） | `IsImage`、`DataURL`、`LoadWorkspaceImage`、`FormatReadImageResult`… |

插件典型用法：

```go
import (
    capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
    rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
    "github.com/lengzhao/agentkit/runtime/session"
)

route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{SessionID: input.SessionID})
```

- **插件应优先依赖**：`agentkit`（根包语义接口）、`cap/*`（类型）、`runtime/*`（读/写上下文与路由的函数）、`pluginkit`
- **插件包之间禁止直接 import**：跨插件协作只通过配置图 `deps` 注入 `cap/*` 或根包接口；共享纯函数实现放 `runtime/*`，不要放在 `plugins/*` 子包互相引用
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
