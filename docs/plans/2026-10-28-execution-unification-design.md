# 执行架构统一

日期：2026-10-28（Asia/Hong_Kong）。用户已确认整体架构评审中的调整方向。

## 目标

保留 Runner → Loop → Agent 主结构与构造期注入。直接角色、delegate 和 router 最终复用同一执行流程，backend 只负责构造/运行 Agent。本批完成已有运行路径的基础收敛，后续动态目录和 ACP 角色消费这些机制。

## 概念与边界

- Conversation：历史归属；SessionContext：一次准备后的 cwd/epoch 快照；Identity：一次实际执行的 ID、ParentID 和 RootTurnID。
- `cap/execution` 只提供执行身份与控制注册契约，不依赖具体 backend。
- `runtime/rctx` 只提供身份与 SessionContext 的类型化读写；保留原定义类型 Execution 的反射身份，SessionContext 作为新别名。
- `runtime/execution` 持有同步子 scope 的生命周期及本地 control；`runtime/permission` 持有父审批代理。
- `runtime/subagent` 内的公共执行函数处理 scope、超时、输出转发、运行和结果提取，两个 backend 只保留构造、模型限制及 telemetry 差异。不建立新的顶层 loop 或全局容器。

上轮新增的 `rctx.NewChildExecution` 尚未提交/发布，直接迁移到 `runtime/execution.NewChild`，不为临时 API 建立反向依赖。保留已经发布的 Spawner、Execution 快照名、backend 名称和历史键。

## 控制所有权

每次调用生成独立 Identity；RootTurnID 继承外层轮次，不因子 telemetry turn 改变。loop 为每个实际 turn 建立根 Identity。子 control 只操作子 context，不消费父 steering/cancel reason。父注册带执行身份，cleanup 只释放自己的注册。新增按 root 取消入口可隔离不同 owner；旧会话级 Cancel 继续保持行为。

同步两种 backend 共用此 scope，审批均通过真实父 broker；无 broker 不批准。旧 async 的父取消及 broker 差异作为显式兼容策略保留，不能通过切到 Background 冒充已实现独立 job owner。共享身份和执行流程不等于强行共享生命周期。

兼容复核确认旧 inprocess 与 loop-agent 都接受缺省 Workspace 的可信调用上下文，旧适配器统一复用 WorkspaceFromContext 规则，不引入 AllowUnscoped 或本地/有路由的分叉。新 NewChild API 仍要求显式 Workspace。标准工作目录保留给第三方 Agent，历史快照独立清除；嵌套深度沿用现有工具策略，审批锚定根交互并保留叶子角色身份。

loop 按独立 ExecutionID 绑定待决 stop，退出只清理自己的状态；较早 cleanup 不影响新轮次。嵌套同步任务发起后台 job 时捕获原宿主 control，而非即将关闭的同步 scope。保留旧异步策略，不提前引入完整 job 系统。

## 公共执行流程

固定目标/定义 → 设置执行超时 → 派生子 scope → 应用后端上下文配置 → 转发输出 → Agent.RunTurn → 从本次历史范围提取结果。scope 必须从创建时就继承执行 deadline，以覆盖独立审批回调。

执行输入使用 ModelMessage，现有 Task 字符串只在适配入口转换。结果读取只保留一份实现，并保留原始执行错误。后续持久执行必须传入本次起始事件位置，不能把旧轮次结果当成本次结果。

## 后续能力遵循的设计

- 角色定义、会话策略、同步/后台方式为独立维度，ACP 只是执行目标适配。
- Catalog 返回 scope 内固定快照；列表可最新，在途计划绑定已校验定义与目标，执行时不重新按名字查找。能力不支持即明确诊断。
- 历史键与 ContextEpoch 由统一会话策略解析，不在 ACP/role 各自重复计算 cwd 失效。acpd legacy resolver 留在迁移适配层。
- 执行状态、最终 outcome、活动信息分离。事件用 ExecutionID/ParentID/RootTurnID 关联，旧 thinking-delta 展示仅保留在平台兼容转换中。
- router 只做固定候选选择和校验，再调用公共执行器。Windows 进程管理独立推进。

## 本批验收

身份独立且父链正确；SessionContext 兼容旧 API；真实父审批不丢；取消和 cleanup 所有权不回归；本地/loop 的同步、异步和模型/cwd 测试通过；完整消息附件通过公共执行入口；原始错误保留。相关包/race、全仓测试/vet 和独立审查通过后报告。
