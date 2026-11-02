# 可注入入站策略

Feishu/Lark 支持可选 `deps.inboundPolicy`，类型为 `cap/ingress.Policy`。
未注入或返回零值 Decision 时保持默认行为。插件构造函数按通常的 pluginkit
机制注册，并在平台 deps 中引用其实例。平台仍负责 SDK 校验、去重、准入和处理执行。

```go
func (p *Policy) Evaluate(ctx context.Context, in ingress.Input) (ingress.Decision, error) {
    if in.Kind == ingress.Message && in.Message != nil && in.Message.AttachmentOnly {
        if in.Context != ingress.Existing {
            return ingress.Decision{Agent: ingress.Defer, Reason: "wait_for_instructions"}, nil
        }
    }
    return ingress.Decision{}, nil
}
```

配置示意（`my/ingress-policy` 由宿主注册）：

```yaml
policy.default:
  use: my/ingress-policy
platform.default:
  use: platform/lark
  config:
    # 其他平台配置……
  deps:
    inboundPolicy: policy.default
    sessionStore: sessionStore.default
    workspace: workspace.default
```

## 决策

- Reject：拒绝本次业务输入，包括内建业务交互；权限确认不走该策略。
- Agent：空=继承，submit=立即提交，skip=不提交，defer=暂存等待。
- Handlers：空=继承，enable/disable=开启/关闭已配置的业务 Handler。
- Reason：用于日志诊断，勿放敏感正文。

消息的 submit 可以覆盖 @ 触发条件，但不能绕过 allowFrom/allowChat/groupOnly。
Message.AddressedToAgent 是不考虑 groupReplyAll 的正常触发结果，包含 @bot、@all、
已激活话题/附件例外，以及机器人身份未知时的原有行为。
事件和卡片不能通过 submit 开启配置外的 Agent 目的地，也不能通过 enable 新增 Handler。
自定义事件只有 Handler 订阅或 forwardEventsToAgent 声明的类型才会注册。
卡片 skip 约束后续 fallback 转 Agent，不影响 Handler 与内建命令；如需全部拒绝用 Reject。

策略收到 SDK 无关的标准上下文及只读 Raw JSON。事件缺少 chat/user 时字段为空，
ConversationKind 保持 unknown，不把 chat_id 自动认定为群。策略不修改身份、正文或路由。
策略必须并发安全、尊重 context；执行预算两秒。错误、取消、超时和非法返回值拒绝本次分发，
不自动重试。超时是合作式的，不强制终止忽略取消的插件。

## 会话与附件

Context 为 existing/new/unknown：按实际 instance、会话 scope 和 /new active mapping
解析逻辑会话；用户正文历史与本平台最近已接收正文算上下文。系统消息、收件提示和标准
附件占位不算上下文。未配置 sessionStore 或读取失败为 unknown。

仅纯附件消息支持 defer。平台保存拥有所有权的临时文件，并固定用户、话题与逻辑会话归属，
不启动模型。默认等待三分钟，连续上传会重置等待时间；过期和停止释放临时文件。
同一平台实例的不同用户互不消费附件。未知逻辑归属不能自动消费，需要重新上传。

DMThreadMode 下先回复固定收件提示。原话题中的正文直接续接；新的顶层正文/附件在同用户、
同私聊只有一个待处理话题时接回原话题。多候选时正文提示用户返回对应话题，附件留在其自身话题。
/new 不消费附件，后续不能把旧逻辑会话附件带入新会话。
附件在队列接受后才从等待索引移除。队列失败保留到 TTL；第一版无持久重试与重启恢复保证。
输入按聊天入口接收顺序处理，防止下载 IO 导致文字越过文件；大群附件下载也会延后同群后续输入。
