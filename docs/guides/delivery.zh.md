# 自定义插件发送卡片

自定义插件注入 `cap/delivery.CardSender`，复用已有平台的客户端、凭据和卡片渲染器。无需 agent turn，不需要新增 delivery 服务或实例命名配置。

## 插件入口

```go
import (
    "context"

    "github.com/lengzhao/agentkit"
    "github.com/lengzhao/agentkit/cap/delivery"
    "github.com/lengzhao/agentkit/runtime/rctx"
)

type Deps struct {
    Cards delivery.CardSender `json:"cards"`
}

func notify(ctx context.Context, deps Deps) (delivery.MessageRef, error) {
    route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{
        Platform: "lark", ChannelID: "oc_target",
    })
    card := delivery.NewCard().Title("任务待确认", "blue").Markdown("请确认执行结果。").Build()
    card.Elements = append(card.Elements, delivery.CardActions{
        Buttons: []delivery.CardButton{{Text: "确认", Type: "primary", Value: "my-plugin:confirm"}},
    })
    return deps.Cards.SendCard(ctx, route, card)
}
```

假设自定义插件已注册为 `example/notify`，配置只引用现有平台：

```yaml
plugin.notify:
  use: example/notify
  deps:
    cards: platform.default
```

可以注入 Lark/飞书、Slack 叶子平台，也可以注入 `platform/multiplex`。multiplex 按既有 RouteRef 的实例 ID 转发；不支持卡片的平台返回 `unsupported`。多实例场景应保留原始事件中的完整 `Envelope.Route`，不用猜测账号。跨重启保存引用时，也应保持既有平台账号和实例 ID 对应关系不变。

## 卡片与交互

卡片模型来自已有的通用 Card，现在公开在 `cap/delivery`；原有 `runtime/platform/common` 类型和 builder 保留兼容别名。Lark 使用已有 interactive card 渲染器，Slack 使用已有 Block Kit 渲染器。Slack 暂不支持 CardSelect，会明确返回不支持。

按钮回调仍由平台已有的 `deps.interactionHandlers` 处理，本入口不新增回调或权限机制。宿主与业务插件负责调用准入和任务归属；平台继续执行既有目标白名单及 SDK 权限检查。Lark 向已有话题发送时会先验证根消息所属聊天，发送失败不会改发普通会话。

不要让插件构造时依赖包含自身的聚合平台，或与其所在平台的 handler 依赖形成环。宿主应分离这类构造依赖；本入口不做全局平台发现。

## 返回值与边界

`SendCard` 返回 `MessageRef{Route, ID}`，可完整 JSON 持久化。只有平台确认成功并返回消息 ID 后才返回成功。卡片整体发送，不自动拆分，也不自动重试；平台大小和编辑限制仍由 SDK/API 决定。

错误类型 `*delivery.Error` 提供 Code 分类。`Unknown=true` 表示可能已发送，例如请求超时或网关异常；不能据此盲目重发。错误文本不包含卡片正文或 SDK 原始响应。

平台构造完成后即可调用，Slack 不要求先启动 Receive。调用同步执行并传递 context；宿主负责后台任务生命周期，同一张 Card 在发送期间不要并发修改。

本次仅提供自定义插件的卡片发送入口。`Platform.Send`、`tool/send` 和原有 assistant 回复保持原样；通用编辑、读取、话题创建、队列及幂等服务不在本次范围。
