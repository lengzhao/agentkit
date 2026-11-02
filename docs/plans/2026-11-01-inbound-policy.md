# 通用入站策略与附件等待完整方案及实施计划

**Goal:** 通过可选 deps.inboundPolicy 统一消息、事件、业务交互的处理决策；Buddy 实现按群免 @ 和新会话附件等待。
**Architecture:** cap/ingress 定义与 SDK 无关的事实和决策。平台保留校验、去重、准入、路由、附件生命周期和执行责任；下游插件只作决策。未注入策略保持原行为。先实现 Feishu/Lark，不建立规则语言或中间件链。
**Tech Stack:** Go、pluginkit、Lark SDK、现有 SessionStore 与附件暂存。

## 1. 接口与输入

Policy.Evaluate(ctx, Input) (Decision, error)。Input 包含平台实例、kind(message/event/interaction)、事件类型与 ID、chat/user/thread/session 上下文、原始 JSON。消息额外包含是否纯附件、是否含用户文字、是否提及机器人、默认触发结果以及不考虑 groupReplyAll 时的正常 @/话题触发结果。事件 chat/user 缺失不推断为群。会话状态为 existing/new/unknown，按实际路由和 active-session 映射解析，不按是否存在 session ID 判断。

Decision 包含 Reject、AgentAction(inherit/submit/skip/defer)、Handlers(inherit/enable/disable) 和 Reason。零值继承。defer 仅允许纯附件消息；其他类别返回非法动作视为策略错误。自定义事件不可借 submit 绕过 forwardEventsToAgent 注册，Handler enable 不新增订阅。卡片禁止 Agent 时必须约束后续 Forward，但保留正常 Handler 执行。

## 2. 优先级与执行

协议/结构校验 → 去重和已有时间规则 → 原有准入 → 解析路由/事实及默认计划 → 可选策略 → 执行。策略不得绕过 allowFrom、allowChat、groupOnly 等原有限制。事件保持现有准入语义，不突然套用消息限制。权限确认及保留权限 token 走原有控制路径。

策略合作式取消，固定短超时；超时、错误和非法动作拒绝本次业务分发，记录输入 ID 与原因，不打印完整 payload，不自动放行。后台执行避免阻塞 SDK。插件必须并发安全；不以无限 goroutine 包装强制超时，不提供持久重试承诺。

## 3. 消息与会话

提取现有正常 @ 触发判断，保留 @all、已激活话题、附件例外和 bot 身份未知时的行为。groupReplyAll 仅决定默认触发，策略可回到正常触发或显式提交。只有准入及最终触发通过后才激活话题。

用与 runner 一致的 route policy、instance、scope、active mapping 读取当前逻辑会话。用户正文历史视为已有上下文；系统/欢迎/附件占位不算。记录本平台已接收但尚未落盘的正文，避免首轮处理中上传被误认为新会话；该记录必须按逻辑会话隔离。查询失败或无 sessionStore 为 unknown，不错误放行。

## 4. 附件等待

Buddy 策略：带正文要求直接处理；纯附件且 existing 直接处理；纯附件且 new/unknown 返回 defer。groupReplyAllChats 仅细化 groupReplyAll=true 的范围，空名单兼容所有群免 @，false 时不启用名单；名单外回到正常 @/话题判断。

defer 下载并保存附件，不启动模型。暂存按实例、用户、路由和逻辑会话归属隔离。DM 话题回复固定提示“文件已收到，请在此话题说明需要我做什么。”，不算用户任务。话题内正文与附件合并；顶层正文只有唯一待处理候选且身份/会话归属校验通过时接续原话题，多个候选提示回到对应话题。引用原路由，不直接改写附件 owner 跨会话消费。

连续文件合并；正文与文件近同时到达按入口接收顺序串行化，避免下载慢导致正文先执行。/new 不消费旧附件，切换后不将旧附件带入新会话。拒绝/失败不消费。超时释放暂存索引，文件清理只针对本机制拥有的临时文件；不删除既有用户 workspace 文件。第一版沿用有限 TTL 和进程内索引，重启不承诺恢复，文档明示。

## 5. 两仓库配置

agentkit 新增 cap/ingress、Feishu/Lark deps、平台实现及通用示例/测试文档。acpd 新增 ingress-policy/buddy 插件并在 base 固定装配。管理端只允许写已固定策略的安全标量配置，不开放任意 deps/use/新实例；配置 groupReplyAllChats 默认空，附件策略默认 context。更新配置示例、配置文档和 guard 测试。

尚未发布 agentkit 前使用本地专用 go.work 做跨仓库验证，不把绝对本机 replace 或未发布版本写入 go.mod。发布后再替换为真实版本，推送/发版等待用户审查。

## 6. 实施任务与测试

1. cap/ingress：先写决策继承、合法性、目标限制测试，运行观察失败，再实现契约与决策解析。命令 go test ./cap/ingress。
2. 飞书消息：先写群名单策略覆写与准入不可绕过、会话状态测试，再接 deps 和入口。命令 go test ./runtime/platform/feishu。
3. 附件：先写新话题等待、已有会话立即提交、文字合并、用户隔离、/new、并发与 TTL 测试，再实现暂存和串行化。保持无策略旧测试不变。
4. 事件/卡片：先写仅 Handler/仅 Agent/拒绝/权限确认/错误测试，再接入口及 Forward 限制。禁止新增订阅与越过路由验证。
5. acpd：先写策略矩阵及 management guard 测试，再注册插件、固定装配和配置白名单。go.work 关联本地 agentkit 验证。
6. 验证：相关包普通与 race 测试、go vet、两仓库构建和必要全量测试。独立代码评审，修复后复测。

## 7. 验收边界

不推送、不发布、不重启或替换正在运行的 Buddy。最终提供代码、完整测试结果和已知限制供用户审查。当前原工作区未提交 events.go/events_test.go 修改独立保留，不混入功能分支。

## 8. 联合验证中补充的 TaskHub 边界

TaskHub 的真实上下文属于任务轮次，不属于源飞书历史。因此 acpd Buddy 策略支持可选
ContextResolver，只有已验证绑定、当前用户和有效轮次才提供 existing。
在 resolve/prune 后由 taskruntime.AdaptGraph 注入，避免未配置 TaskHub 时普通策略被连带裁剪。

策略暂存的上传携带可选 InboundUpload.Owner，runner 附件物化前再次检查，避免排队期间 /new
导致跨会话消费。TaskHub Transform 验证源 owner、真实绑定和当前轮次后显式转交到任务 owner；
在记录 AcceptTaskInput 前完成，失败不抢占输入幂等键。Gateway 不无条件重写 owner。

已增加真实策略 → TaskHub Transform → Gateway 包装 → PrepareInboundAttachments 集成回归，
以及未知归属、/new、纯附件历史占位、富文本正文、队列顺序和失败不提前消费测试。
