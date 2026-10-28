# Child Execution Scope Implementation Plan

**Goal:** 提供同步子执行的历史隔离、父审批和取消所有权公共入口。

**Architecture:** runtime/rctx 构造子 scope，通过已有 context 控制接口接入父 Control。loop Control 按注册 ID 清理步骤；同步 loop-agent 消费新 API，异步委派保持既有行为。

**Tech Stack:** Go context、现有 permission broker、Go testing 与 race detector。

本计划记录第一批实现。后续调整与当前入口见 [执行架构统一计划](2026-10-28-execution-unification-plan.md)；第一批 scope API 尚未发布，已从 rctx 迁到 runtime/execution。

## Task 1：取消所有权

- 修改 `runtime/loop/control.go`，测试 `runtime/loop/cancel_test.go`。
- 先添加旧 cleanup、父子注册和启动前取消测试，运行 `go test ./runtime/loop -run TestControl -count=1` 确认旧实现失败。
- 为步骤注册分配 ID，取消全部活动注册，cleanup 只移除自身；复跑测试。

## Task 2：公共子执行 scope

- 新增 `runtime/rctx/child_execution.go` 和外部包测试 `runtime/rctx/child_execution_test.go`。
- 测试先行覆盖路由/历史身份、父状态隔离、取消、broker 转发、无 broker、唯一审批 ID、非法输入与嵌套拒绝。
- 实现可信宿主选项、cleanup 与父 broker wrapper，不增加租户或路由的第二套字段。

## Task 3：同步 loop-agent 集成

- 修改 `runtime/subagent/loopagent.go`，新增 `runtime/subagent/child_scope_test.go`。
- 用真实 loop Control 和受控测试 agent 验证审批回到父 conversation、结束子执行后父停止仍然有效、父停止取消子任务。
- 同步路径使用公共 scope，async 保持原行为；不改变临时历史键或结果语义。

## Task 4：验证与交付

- 新增公共 API 使用说明，注明同步、非嵌套边界。
- 执行 `go test ./runtime/rctx ./runtime/loop ./runtime/subagent ./plugins/agent/acpremote -count=1`。
- 执行上述相关包的 `go test -race` 与 `go vet`，再执行 `go test ./... -count=1`。
- 检查 diff，仅报告已完成和已验证能力，不宣称持久角色等后续批次完成。

## 执行结果（2026-10-28）

本计划四项均已完成。公共说明见 `docs/child-execution.zh.md`。变更尚未提交或发布。

- 新增测试先复现旧 cleanup 清掉新步骤、只取消最后步骤、启动前停止丢失，以及审批回复使用子 conversation/子输出通道的问题，再验证修复。
- `go test ./... -count=1` 通过，96 个有测试的包。沙箱内因回环端口 bind 限制失败，获自动审批后在沙箱外完成重跑。
- `go vet ./...` 通过。
- `go test -race ./runtime/rctx ./runtime/loop ./runtime/subagent ./plugins/agent/acpremote -count=1` 通过。
- `git diff --check` 通过；独立只读代码审查未发现阻塞缺陷。
- 本机默认 Go 缓存目录受限，验证使用 `GOCACHE=/private/tmp/agentkit-go-build`。

后续：持久角色与 ACP backend、动态目录、生命周期事件、router 和 Windows 支持均不在本次实现中。
