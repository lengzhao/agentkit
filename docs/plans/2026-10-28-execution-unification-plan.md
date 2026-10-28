# Execution Unification Implementation Plan

**Goal:** 将已有子执行收敛成身份明确、职责分离、两种 backend 共用的执行基础。

**Architecture:** rctx 保留上下文协议，runtime/execution 管理 scope，runtime/permission 管理审批代理。runtime/subagent 提取共享执行流程，不新增顶层调度系统。

**Tech Stack:** Go context、既有 Agent/Session/permission、Go testing/race。

## 1. 身份与上下文快照

先为 `cap/execution.Identity`、`rctx.SessionContext`/旧别名与身份上下文写失败测试；新增类型和 accessor。为 loop 根 turn 添加 Identity，子执行继承 root 并生成新 ID。测试同历史多次调用和 parent 关联。

## 2. 生命周期与审批分层

把上轮未发布的 `rctx.NewChildExecution` 迁到 `runtime/execution.NewChild`；测试移到新包并保留所有断言。父审批代理移到 `runtime/permission`。loop 注册保留 Identity 并支持按 root 取消，补取消隔离测试。不得留下 rctx → execution → rctx 环依赖。

## 3. 公共执行流程

在 `runtime/subagent` 提取上下文派生与执行/结果读取函数。输入使用完整 ModelMessage。先补附件透传、错误提取、不同 backend 的身份/取消测试，再让 inprocess 和 loop-agent 复用。构造、telemetry 和旧 async control 策略仍在明确边界内。

## 4. 文档与验证

更新 `docs/child-execution.zh.md` 与架构说明，标明本批完成与后续目录/角色/router 规划。

依次运行：定向失败/通过测试；相关包 `go test -race`；`go test ./... -count=1`；`go vet ./...`；`git diff --check`。使用 `GOCACHE=/private/tmp/agentkit-go-build`；全仓测试需要回环端口时复用已授权的沙箱外验证方式。独立只读代码审查后处理缺陷。

## 完成记录（2026-10-28）

1–4 已完成，改动尚未提交或发布。

- 新增执行 Identity/Registrar，loop 每次调用分配独立身份，子调用继承 parent/root；取消注册支持按 root 取消活动执行。
- SessionContext 与单次 Identity 分离，旧 Execution 快照入口兼容；sessctx 使用新名称。
- 未发布的子 scope 迁到 runtime/execution；父审批代理迁到 runtime/permission，消除 rctx 中的新增生命周期编排。
- 两种 backend 复用上下文派生、超时、完整消息执行、输出转发、按 After 范围提取结果；异步原控制策略显式保留。
- 第一轮曾以 local-only AllowUnscoped 适配缺省 Workspace；后续兼容性复核已撤回此开关，两个旧 Spawner 统一使用既有 WorkspaceFromContext 规则。新 NewChild API 保持严格，不改变原 fallback。
- `go test ./... -count=1` 通过，97 个有测试的包。全量测试获自动审批后在沙箱外运行，以满足回环端口测试。
- `go test -race ./runtime/rctx ./runtime/execution ./runtime/permission ./runtime/loop ./runtime/subagent ./runtime/session/sessctx ./plugins/agent/acpremote -count=1` 通过。
- `go vet ./...`、`git diff --check` 通过；独立静态审查及兼容修复增量审查无阻塞缺陷。

后续目录固定快照、持久角色、统一 outcome/事件、router 和独立后台 job owner 仍为设计要求，本批未实现，也未改变其发布范围。
## 本地改动兼容性复核（2026-10-28）

本轮发现并修复了以下回归，均补充针对性测试：

- 取消子 scope 禁止嵌套的新增限制，继续由既有委派工具控制深度；嵌套审批保留根交互位置和叶子 Agent 身份。
- 清理父会话历史快照时保留标准 WorkingDirectory，避免第三方 Agent 丢失工作目录；旧 Spawner 统一沿用既有 workspace resolver，撤回 AllowUnscoped 专用开关。
- 保留原 Execution 定义及反射类型名，SessionContext 使用类型别名；只有一个历史读取错误时保留原错误对象，兼容 sentinel 判断。
- 独立回调 context 保留普通取消原因及标准超时分类；停止原因按执行身份归属并清理，避免污染下一轮调用。
- 后台任务捕获原宿主 control，避免继承同步子 scope 关闭后的生命周期，同时保留原审批能力。

最终源代码验证：全仓 go test ./... -count=1 通过（97 个测试包），go vet ./... 通过；相关包 race 已通过，最后一次错误返回修正后另行重跑 runtime/subagent race 并通过。git diff --check 通过。两轮独立审查及增量复核未发现新的已确认阻塞问题。

保留限制：跨独立回调 context 桥接 WithDeadlineCause 的自定义超时原因时，可能归一为 DeadlineExceeded；标准超时分类已验证。后台任务仍使用原兼容行为，独立 job owner 不属于本批实现。上述边界见 docs/child-execution.zh.md。
