# 后台插件消息投递

`delivery/messages` 为提醒、状态通知、审批和 TaskHub 等消费者提供可注入的消息服务。它复用平台实例的客户端和凭据，不依赖 agent turn、当前 inbox 或平台入站缓存。

原有 `Platform.Send`、`delivery/assistant` 和 `tool/send` 默认行为保持兼容。流式回复、权限事件和轮次生命周期继续走原有路径。

## 可组合能力

`cap/delivery` 定义独立的 `Publisher`、`Editor`、`Reader`、`TopicCreator`。插件只需依赖实际使用的窄接口；`Messages` 是四者的组合。路由服务返回组合接口，具体操作若没有对应叶子能力，返回 `unsupported`。

**话题完全可选。** `Publish` 在没有 `ThreadID` 时直接发到会话，不要求话题支持。`CreateTopic` 是显式操作，初始 Lark/Slack 实现发布根消息并返回可继续回复的路由，不承诺平台原生的标题、话题频道或预先创建空话题。已有话题由 `RouteRef.Target.threadId` 指定；不支持的操作不会静默改发到普通会话。

初始两个适配器支持：文本与结构化用户提及、长文本拆分、发布、编辑单条消息、按引用读取、建立根消息回复目标。图片、文件、卡片及其他内容类型明确返回 `unsupported`，在发送任何部分前检查；原有 assistant 路径的媒体能力不受影响。`Part{Type:"mention", UserID:"..."}` 的 ID 由目标实例解释，不要求消费者构造 XML 或 Slack 标记。提及不可携带 Text；展示名由平台决定。

文本以 Lark post 文本节点和 Slack mrkdwn 文本发送，Slack 中 `&<>` 会转义以避免文本隐式构造提及；平台样式表现不保证一致。当前每条最多 3000 个 Unicode 字符和 100 个节点，提及保持完整且计入预算。编辑超过单条预算时拒绝，不自动增删其他物理消息。读取返回平台提取的展示文本，不承诺无损恢复原始节点或作为内容编辑的乐观锁。

## 配置与实例身份

```yaml
# 与既有 platform.slack / platform.feishu 叶子定义共用客户端，不重复配置 secret。
delivery.messages:
  use: delivery/messages
  config:
    instances: [work, cn]
  deps:
    platforms: [platform.slack, platform.feishu]

platform.default:
  use: platform/multiplex
  config:
    instances: [work, cn]
  deps:
    platforms: [platform.slack, platform.feishu]

# 在既有 tool.send.default 的 deps 中追加，sender/delivery/workspace/fs 保持原配置。
tool.send.default:
  use: tool/send
  deps:
    sender: platform.default
    delivery: delivery.default
    messages: delivery.messages
```

上面是合并到现有配置的片段，叶子平台仍使用对应 preset 的完整定义。单平台可只注入一个叶子，不需要 multiplex。

`instances` 与 `platforms` 一一对应。省略时使用叶子已有实例身份，尚未分配时使用协议名；重复身份报错。多个同协议实例必须使用稳定、不同的显式名称。与 multiplex 共用叶子时，两边配置相同的实例名称，不依赖构建顺序。自动顺序后缀不适合作为需要跨重启持久化的部署身份。

服务按 `RouteRef.Instance` 选择叶子，再校验协议和目标；不会只按协议挑选某一个账号。引用中必须保留完整路由。更换机器人身份时使用新的实例名称，不能将旧名称重新分配给另一个账号后继续使用旧引用。运行期间不要重设叶子身份；身份漂移会拒绝操作。

若业务插件本身也是 Platform，投递服务的 `platforms` 只绑定真正的叶子平台。不要绑定包含该业务插件自身的聚合平台，避免依赖环。

## 普通插件无 turn 投递

```go
// 自定义插件的依赖：无需 SDK 或平台 secret。
type Deps struct {
    Messages delivery.Publisher `json:"messages"`
}

route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{
    Platform: "slack", Instance: "work", ChannelID: "C123",
})
receipt, err := deps.Messages.Publish(ctx, delivery.PublishRequest{
    Route: route,
    Content: []delivery.Part{
        {Type: "text", Text: "报告已生成，请查看 "},
        {Type: "mention", UserID: "U123"},
    },
    Caller: "report", OperationID: "report:2026-11-03:publish",
})
// 无论 err 是否为空，都保存 receipt.Messages 和结果状态。
```

导入 `cap/delivery`、根包 `agentkit` 和 `runtime/rctx`。路由在调用前由宿主或消费者确定，不需要保留一个已结束 turn 的 context。后台任务应使用自身受宿主生命周期管理的 context。

`tool/send` 的新模式是可选的：

```json
{
  "text": "报告已生成",
  "receipt": true,
  "operationId": "report:2026-11-03:publish",
  "route": {
    "platform": "slack", "instance": "work", "kind": "session",
    "target": {"channelId": "C123"}
  }
}
```

也可在 turn 内省略 `route`，沿用 `sessionId` / `userId` / 当前 inbox 解析。显式 route 不能与 sessionId/userId 混用。指定 route 或 operationId 也会启用回执模式。回执模式当前拒绝 `path` 和 `raw`，且要求注入 `messages`；不会回退到无法取得回执的旧发送。失败时输出包含 `sent:false`、`errorCode` 和完整部分回执；原有非回执发送不改变输出结构。

## 回执、错误和重试

`MessageRef{Route, ID}` 表示一条物理消息，可 JSON 持久化。一次发布可能返回多个引用。`Receipt.Route` 是实际投递或建立的回复目标；根消息引用和话题回复目标可能不同，应分别保存。

- `confirmed`：本次发布所有部分均已取得平台确认和引用。
- `not_sent`：没有确认成功的部分，失败操作确认未发送。
- `partial`：前面的部分已确认成功，失败部分确认未发送；后续部分未尝试。
- `unknown`：失败部分可能已发送，不能因超时而推断未发送。仍保留此前确认的引用。

`FailedPart` 标明失败的物理部分索引。错误上的 `Outcome` 描述失败操作本身，回执上的 `Outcome` 描述整次发布。错误分类包括不支持、无效目标、无效内容、权限不足、限流、传输异常和其他失败。未知平台错误保守归为结果未知。`RetryAfter` 只给出等待建议，不意味着可以安全重试。

服务没有自动写入重试、本地去重缓存、队列或持久 outbox。稳定 `OperationID` 的保证由适配器决定：

- Lark/飞书：提供 OperationID 时，将调用者、实例、稳定目标、操作和分片索引映射为确定的原生 UUID；SDK 对应接口文档规定同 UUID 在 **1 小时内**至多成功发送一条。没有 OperationID 时不提供去重保证。窗口从平台首次接收计算，回执记录的是窗口长度，不能据此假定重启后仍在窗口内。
- Slack：当前返回 `Deduplication.Mode="none"`，不承诺原生去重或 exactly-once。OperationID 用于调用关联，不能据此自动重发未知结果。

OperationID 只能重用于同一业务操作及完全相同的内容、目标和拆分策略。变更内容应生成新 ID；超出去重窗口不能把旧 ID 当作可靠去重凭据。普通编辑按单个物理消息引用执行，不提供并发版本校验或写入去重保证。

## 生命周期、并发与准入

服务构造完成且凭据可用后即可调用，不要求 `Receive` 已启动。调用同步执行，透传 context 取消，不生成后台 goroutine；宿主负责停止后台业务任务并等待调用结束。网络请求开始前取消可确认未发送，请求发出后的超时/取消按结果未知处理。多个调用可并发；同一消息的并发编辑由消费者排序，服务不保证完成顺序。

可通过 `deps.admission` 注入 `delivery.Admission`。宿主接收规范化后的路由、操作、Caller、OperationID，执行自己的准入策略。未配置时，持有服务的插件视为宿主信任的消费者。Caller 是关联标签而非认证凭据；不应把该服务直接暴露给不可信客户端。业务归属检查由业务插件保留，不能用 agent turn 中的批准状态代替后台调用授权。

平台继续执行目标白名单等限制；Lark 话题写入会先验证根消息所属聊天，消息编辑也先验证所属聊天。SDK 权限、应用可用范围和成员关系仍可能导致平台拒绝。服务没有通用身份/成员查询 API，也没有独立业务权限数据库。

每次服务调用记录操作、Caller、OperationID、错误分类；日志不包含正文、完整路由或 SDK 原始错误响应。不要将 secret 放入关联标识。

## 下游 TaskHub 迁移示例

TaskHub 注入 Publisher/Editor/Reader，需要时再使用 TopicCreator，删除自己构造的飞书客户端及重复凭据配置。

1. 创建任务时可以直接 Publish 到会话；如果产品希望使用话题，再显式调用 CreateTopic。无话题不影响任务创建、提醒或状态消息。
2. 将任务自己的业务状态与 `Receipt.Route`、全部 `MessageRef`、OperationID、结果确定性一起持久化。业务 outbox 和状态事务由 TaskHub 管理。
3. 重启后通过 `ReadMessage(ctx, ReadRequest{Message: savedRef, Caller:"taskhub", OperationID: lookupID})` 核对已确认的消息，用 `EditMessage` 更新某条状态消息。被删除、权限变化或过期编辑限制必须作为业务可处理结果。
4. 旧话题绑定需由迁移代码转换为对应平台实例的 RouteRef，保留历史业务 ID。不要猜测旧绑定属于哪个机器人实例。
5. 遇到部分成功保存已确认引用；遇到未知结果先核对证据，Slack 或超出去重窗口时不要盲目重发。按单条引用读取只能核对已知消息，不能凭空定位一个没有返回引用的未知写入。

本仓库提供通用能力及示例，不修改下游 ACPD/TaskHub 的状态库、验收流程、提醒周期或投递 outbox。
