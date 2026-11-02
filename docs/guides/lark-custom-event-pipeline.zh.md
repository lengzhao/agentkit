# Lark 自定义事件管线

Feishu/Lark 平台负责事件接收、解析、去重、路由和入站消息组装；owner、租户策略等业务判断由 `deps.inboundPolicy` 插件负责。不新增独立 ForwardGate 接口。

## 配置

```yaml
policy.events:
  use: my/event-policy  # 由宿主注册，实现 cap/ingress.Policy
platform.default:
  use: platform/lark
  config:
    forwardEventsToAgent:
      - minutes.minute.generated_v1
    dmThreadMode: true
  deps:
    inboundPolicy: policy.events
    sessionStore: sessionStore.default
    workspace: workspace.default
```

`forwardEventsToAgent` 保持现有名称，只接受事件类型，不支持 `event_type:agentId`。Agent 选择沿用现有平台与 Loop/Runner 路由，不引入按事件类型选择 agent 的配置。

## 处理顺序

1. `deps.eventHandlers` 声明的类型与 `forwardEventsToAgent` 合并后注册 SDK 回调。
2. 解析结构化事件并按 `event_id` 去重。
3. 配置了 `inboundPolicy` 时，使用 `ingress.Event`、事件 Type/Raw/ChatID/UserID 调用策略。
4. 按策略分别决定是否调用 Handler、是否转发 agent。策略在创建事件话题前执行。
5. 允许转发后创建回复上下文，组装 `[事件] {type}\n{raw JSON}` 消息并交给 inbox/Runner。

策略返回 `Agent: ingress.Skip` 可以阻止 agent 转发而保留 Handler；`Reject: true` 拒绝整个业务分发。策略不能开启配置之外的事件目的地。错误、超时和非法结果拒绝分发，不自动重试；当前去重与 SDK 成功响应也不等价于业务处理成功。

策略执行预算为两秒，且依赖插件合作处理取消。调用外部 API 的插件应设置更短超时，明确失败时放行还是拒绝。构造函数和配置校验不应发起业务订阅；用户 OAuth、服务端订阅及 owner 查询由宿主维护。

## DM 事件话题

未开启 `dmThreadMode` 时，保持独立事件 session（`<platform>:evt:<event_id>[:u:<open_id>]`）。带 chat_id 的事件及无回复目标的事件保持原路由。

开启后，仅用户身份的事件会先给该用户发送一条 `[事件] {type}` 根消息，再以实际 chat_id 和根消息 ID 构建普通 DM 话题 session。后续回复使用该话题，用户可在话题中继续交流。创建失败时记录日志并回退原事件 session。

根消息创建使用稳定 UUID，减少瞬时重试导致的重复根消息。此模式改变事件的 session 标识；依赖旧 `evt:` session 的绑定或历史索引需相应调整。根消息只是事件锚点，不代表模型已经完成处理。

## 扩展边界

策略输入 Raw 为只读快照，不通过修改共享 map 向并行 Handler 传递上下文。目前未新增自定义事件 Metadata 壳、Metadata patch 或 Enricher 接口；工具需要额外结构化元数据时，应单独定义并验证传递契约。

参考：[用户身份事件订阅](lark-user-events.zh.md)、[入站策略](inbound-policy.zh.md)。
