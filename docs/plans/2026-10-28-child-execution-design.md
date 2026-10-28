# 子执行 scope 设计

日期：2026-10-28（Asia/Hong_Kong）。范围来自本次讨论确认的第一批能力，不把 acpd 交接稿整体作为开发指令。

后续架构调整：本文是第一批实现记录；包归属及公共入口已由 [执行架构统一](2026-10-28-execution-unification-design.md) 更新。scope 移到 `runtime/execution.NewChild`，审批代理移到 `runtime/permission`，rctx 只保留上下文协议。

兼容复核更正：下文初稿的“拒绝嵌套”和“清除标准 cwd”未予保留；既有工具允许两层委派，第三方 Agent 依赖公开 cwd 上下文。最终行为见上述统一设计。

## 目标与范围

提供可信宿主可调用的公共子执行构造入口：替换子历史和 AgentID，保留父 workspace、actor、投递路由及轮次关联。审批使用父 broker 和父逻辑会话，但展示实际子 agent。子执行不持有父 broker 生命周期，不消费父 steering 和取消原因。旧委派的临时会话策略不变。

本批不实现持久角色、动态目录、router、Windows launcher 或 acpd 历史迁移。后续依赖此机制。

## 方案选择

直接共享父 Control 会让子步骤消费父状态并覆盖取消句柄；给子任务完全独立的 Control 又会切断用户停止与审批。采用子执行 wrapper：在父 Control 注册一个有所有权的取消范围，子步骤只派生本地 context；审批转交父 broker，恢复父 conversation 与投递路由，同时保留子 AgentID。

入口放在 runtime/rctx，配置只包含历史 SessionID 和 AgentID，不接受模型传入 workspace、投递对象或审批策略。为第一版明确拒绝嵌套子执行。清除父打开的 session、执行 epoch 和工作目录快照，单独继承默认 cwd，供子 runtime 自行 Prepare。

父 Control 使用有独立 ID 的步骤注册表，Cancel/Stop 取消当前全部注册，cleanup 只删除自己的注册。已挂起的 stop 会取消新注册，避免启动窗口丢失停止。

审批每次生成新的宿主关联 ID，避免远端重复 ID 或迟到回答命中其他请求；请求结果仍交还调用方。没有 broker 时明确返回 no_human。保留父 broker 的 allow-all 策略。

## 集成与验证

同步 loop-agent 使用新入口；已有 async 路径保持原生命周期，避免把长于父轮次的任务强行纳入同步 scope。新 API 暂不用于 inprocess backend，后者现有空 control 语义另行评估。

测试覆盖取消注册所有权、父停止与子 cleanup 隔离、历史/投递/租户身份、父 session 与 epoch 不泄漏、审批 conversation 与角色身份、无 broker、请求 ID 唯一、嵌套拒绝及同步 loop-agent 接入。运行相关包测试、race 和 vet；全量测试报告现场结果。
