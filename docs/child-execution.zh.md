# 同步子执行 scope

`runtime/execution.NewChild` 为可信宿主提供子历史与父交互控制的组合入口。同步 inprocess 和 loop-agent 共用此 scope；历史键及 Spawner 接口保持兼容。此入口不创建持久角色、不决定历史键。

```go
childCtx, closeChild, err := execution.NewChild(parentCtx, execution.ChildOptions{
    HistorySessionID: childSessionID,
    AgentID:          childAgent.ID(),
})
if err != nil {
    return err
}
defer closeChild()

return childAgent.RunTurn(childCtx, agentkit.TurnInput{
    Message: taskMessage,
    Emit:    childEmit,
})
```

宿主必须从认证后的入口提供 `parentCtx`，其中包含非空 Workspace 和逻辑 Conversation；子历史键必须在对应租户中隔离，且不同于父会话。配置不能直接从模型工具参数反序列化。路由、租户、用户及父轮次关联由父 context 继承，不能通过选项替换。

旧 inprocess 与 loop-agent 的 Spawner 适配器在进入新 scope 前，使用既有 `WorkspaceFromContext` 规则补齐旧调用方缺省的 Workspace；有无 Route 均沿用原规则，不新增“仅本地”的特殊开关。调用方仍须保证入口会话命名空间在共享 runtime/store 中隔离。新的 `NewChild` API 本身始终要求显式 Workspace，不从任务、cwd 或模型参数推断租户。

## 历史与工作目录

子 context 替换 Conversation 和 AgentID，隐藏父打开的 Session，并清除父 SessionContext 的历史 epoch。标准 `workspace.WorkingDirectory` 及 inherited working directory 均保留父 cwd，第三方 Agent 可以直接读取公开的工作目录上下文；内置 runtime 仍可通过 `sessctx.Prepare` 解析自己的绑定并覆盖 cwd。

传入的消息与附件由宿主负责；scope 不修改消息。若宿主采用稳定的子历史键，可以在此基础上构建持久角色，但仍须另外实现版本隔离、串行锁和恢复策略。

## 执行身份与包职责

`cap/execution.Identity` 包含每次调用独立的 ID、直接父执行 ParentID 和外层交互 RootTurnID。根 loop turn 创建 Identity，子 scope 派生新 ID，后端 telemetry turn 不改变该 root 归属。同历史的多次执行仍有不同 ID。

`rctx.SessionContext` 保存 SessionID/Cwd/Epoch，与执行身份分别读写。保留原定义类型 `rctx.Execution`，让 SessionContext 成为其别名，确保反射名及旧 WithExecution/ExecutionFromContext/WithoutExecution 入口兼容。清除 SessionContext 不会清除执行身份。

生命周期归 `runtime/execution`，父审批代理归 `runtime/permission`。rctx 只提供上下文协议，不导入上述生命周期包。上轮未发布的 rctx.NewChildExecution 已直接迁移，未引入反向兼容依赖。

## 审批与取消

子控制器提供父 broker 代理。审批的 Conversation 和投递通道来自父 scope，事件中的 AgentID 是子角色。每个审批请求使用新宿主 ID，后端重复使用的 ID 不会复用到 UI；调用方不应假设后端请求 ID 等于 UI ID。父 broker 保留已有 allow-all、拒绝、超时和取消语义；没有 broker 时返回 `no_human`，不会批准。

scope 在父控制器注册自己的取消范围。父 `/stop`、context 截止时间或 `closeChild` 会终止子执行；子步骤不能读取或消费父 steering 和取消原因。`closeChild` 可重复调用，只释放该 scope 持有的注册，不关闭父 broker、不取消父步骤。等待审批也受 scope 取消约束。

嵌套子执行共享根审批交互，事件身份保留实际最内层 Agent；结束内层不关闭外层。跨独立回调 context 时保留普通取消 Cause 和标准 deadline 的超时分类。`WithDeadlineCause` 的自定义超时原因目前可能归一化为 DeadlineExceeded，不改变普通超时 outcome。

`loop.Control.RegisterExecution` 记录身份及取消所有权；旧 BeginStep 自动读取当前 Identity 后调用它。每个 cleanup 仅删除自身。Cancel 和 Stop 保留会话级取消行为；新增 CancelRoot(rootTurnID, reason) 只取消指定交互的活动注册，不写入另一个轮次的待决原因。挂起的会话停止原因会让新步骤立即取消，直到原有轮次控制逻辑消费或清除该原因。

loop 还按独立 ExecutionID 绑定和清理轮次的待决停止原因；自定义 Agent 无需主动 PopCancelReason，也不会让上一轮停止污染下一轮。较早轮次的迟到清理不能清除新轮次的原因。

两种 subagent backend 共享上下文派生、超时、完整消息执行、输出转发和结果读取函数。旧 Task 字符串仅在适配入口转为 ModelMessage。结果读取支持 After 事件位置；未来持久调用须传入本次起点，不能读到旧轮次的结论。

## 当前边界

- scope 管理同步执行；允许嵌套，深度策略由宿主或既有 delegate 工具负责（工具仍限制最多两次委派）。
- 异步 loop-agent 与异步 inprocess 共享身份、超时、执行和结果读取流程，但保留原来的不同 control 策略：前者继承原宿主 control，后者隔离。捕获后台上下文时不保留即将关闭的同步子 control。独立后台 job owner 尚未实现，不能将它们当作同步 scope 的等价物。
- 不包含动态角色目录、ACP provider 派生、持久历史键、router、执行状态事件或旧 acpd 历史迁移。
- 宿主需保留父 scope 的完整生命周期，不应把该 API 用于跨父轮次存活的后台 job。
