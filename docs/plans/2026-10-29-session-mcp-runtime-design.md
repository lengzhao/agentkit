# Issue #21：ToolRuntime Session MCP 需求与设计

来源：https://github.com/lengzhao/agentkit/issues/21

用户确认（2026-10-29）：优先 per-turn；存在无法可靠解决的问题时允许采用会话级作用域。

## 目标与范围

扩展现有 `acp/session-mcp-tools`，支持通过 `ToolRuntime.Visible/Execute` 服务外部 ACP Agent，保留 ToolProvider 配置兼容性。覆盖渐进披露、目录变更、策略审批、取消、超时和错误诊断。不包含企业身份、AgentHub JWT 或客户端专属业务配置。

## 已核实的现状

- `plugins/acp/sessionmcptools/bridge.go` 将 Provider 工具同步到共享 MCP 注册表，调用 `Tool.Call`；请求级 filter 只调整 description。
- `plugins/agent/acpremote/bridge.go:ensureSession` 在已有远程会话时提前返回；MCP 配置仅在建立或恢复远程会话时解析。不能假设 SessionMCPServers 每轮都会被调用。
- `plugins/agent/acpremote/acpremote.go:RunTurn` 已有 turn 开始、结束和可取消上下文，可作为显式生命周期接入点。
- `plugins/tools/deferred/reveal.go` 将揭示状态放在 rctx.State 中；没有 state 时揭示不保留。远程路径需要明确注入每轮状态。
- 当前依赖 mcp-go v1.0.0 提供 session tools、定向通知和 SetTools。可复用其能力，但还需验证初始化时机、通知与并发行为。

## 方案选择

1. 每轮创建新远程 ACP 会话及 MCP 端点：作用域直观，但影响会话复用、恢复和开销，不作为默认方案。
2. 稳定会话连接 + 显式 turn 绑定：推荐。MCP 配置随会话建立，执行上下文由宿主每轮绑定；请求捕获当前绑定的不可变快照。
3. 会话级上下文与揭示状态：允许的回退方案。必须记录选择原因，审批与取消仍绑定当前执行，不能持有已结束 turn 的审批对象。

推荐方案不是严格的客户端 per-turn 身份证明：如果客户端不携带轮次标识，旧请求恰好在新 turn 开始后到达，仅凭稳定连接无法判断它属于旧 turn。必须在实现和测试中承认这个限制；不得宣称凭证已具备严格跨轮次隔离。若该歧义影响正确性，则明确采用方案 3，不静默切换。

## 接口与生命周期

- 在 cap/acp 增加可选的 turn 生命周期接口，由 acpremote 在每轮调用；原 SessionMCPProvider 保持兼容。接口具体名称在实现阶段确定。
- 插件后端为 provider/runtime 二选一，两者都缺失或同时配置时报错。原 provider 配置仍有效。
- 会话连接使用不透明绑定标识，作用域包含宿主会话、agent 和执行上下文代次，避免仅靠 session_id 混用多个 agent 或重建后的连接。
- 每轮 begin 在 session 初始化前建立上下文和独立 rctx.State；结束、错误和取消路径都必须执行 end。建立 MCP 会话期间允许读取目录，执行工具要求活动 turn。
- 同一绑定同一时刻最多一个活动 turn，冲突明确报错。end 使用 generation 比较，旧 end 不能清除新绑定。
- 每个 HTTP 请求捕获当前 turn 快照，执行中不切换到下一轮上下文；取消同时受 HTTP 请求和宿主 turn 控制。turn 返回时显式取消，即使调用方没有取消父 ctx。
- turn 结束后拒绝工具执行。传输保活可以继续，不能让 GET/SSE 的寿命决定 turn 寿命。
- Close、连接淘汰和重建必须撤销相应绑定并释放资源；不能无限累积会话和上下文。

## 目录与调用

- Runtime 目录从该作用域的 Visible(ctx) 生成，调用始终进入 Execute(ctx, call)。不重新实现策略或审批。
- 优先使用作用域独立的 MCP server/stream handler，共享 HTTP listener；这样可以直接复用 SetTools，避免会话目录写进全局共享注册表。独立 server 的资源清理由绑定生命周期负责。
- 首次目录、每轮绑定、工具调用结束后和显式 Refresh 时刷新。调用后重新读取 Visible，使 search/describe 揭示工具能直接调用。
- 工具外部增删通过明确的 Refresh 入口传播；无请求、无刷新信号时不能承诺主动发现变化。本期不引入无限轮询。
- 规范化排序并比较完整对外目录，包含名称、description 和 schema；变化时替换目录并通知该作用域的客户端，不变时不执行 SetTools。
- 同步与调用使用 generation 检查，过期 turn 的刷新结果不得覆盖新 turn 的目录。不能持锁执行工具、审批或向客户端发送通知。
- 当前目录同步失败应返回明确错误，不把失败当作空目录或悄悄继续执行旧策略视图。
- 并发搜索的揭示状态更新需要验证，必要时使同一 state 的读改写原子化，避免丢失揭示结果。

## 结果和错误

- 保留 ToolCall 标识、返回文本和可用诊断；HTTP/MCP 请求标识与宿主工具调用标识需有明确映射，避免跨连接冲突。
- Execute error 和 Runtime Audit 表示的拒绝、超时、工具失败都不能转成 MCP 成功；映射表必须依据实际 Runtime 返回约定制定，不能猜测 audit 字段。
- 审批拒绝与 turn abort/cancel 分别处理。若 Runtime 要求中断 turn，需通过生命周期绑定回传到远程 RunTurn，不能仅返回 isError 后允许继续执行。
- MCP isError 不提供完整宿主审计模型，完整 Audit 留在宿主日志或追踪中；客户端保留可诊断信息。

## 验收与实施顺序

1. 生命周期契约测试：连接复用仍逐轮 begin/end；初始化失败、空 prompt、取消、异常退出都释放绑定；旧 end 不影响新 turn。
2. Runtime 后端测试：Visible/Execute 使用同一作用域；Provider 原有测试保持通过；双后端配置拒绝。
3. HTTP 集成测试：真实 MCP 客户端完成 search/describe → list_changed → 直接调用；同名工具在两个会话中具有不同 schema/description。
4. 目录测试：新增、删除、schema/description 更新，顺序变化不通知，无变化不循环，Refresh 错误不污染目录。
5. 执行测试：deny、审批拒绝/取消、父取消、HTTP 取消、超时、普通执行错误和 turn abort；检查 MCP 结果与宿主 turn 结果。
6. 跨轮次测试：连续两轮揭示重置、迟到请求、旧请求结束后不得覆盖新目录。根据证据保留 per-turn 或明确回退会话级。
7. 运行相关包测试和 race 检查，再更新插件配置目录及使用文档。

建议验证命令：

```sh
go test ./plugins/acp/sessionmcptools ./plugins/agent/acpremote ./plugins/tools/deferred ./runtime/tools
go test -race ./plugins/acp/sessionmcptools ./plugins/agent/acpremote ./plugins/tools/deferred
go run ./scripts/gen-config-catalog
go run ./scripts/gen-config-catalog -check
```

## 实施记录（2026-10-29）

已实施 Runtime 后端、可选 turn 生命周期、bridge 独立凭证和资源释放、目录指纹去重通知、执行错误映射。使用会话级连接路由回退，执行上下文与揭示状态逐轮更新；严格的客户端迟到请求轮次识别不在当前协议能力内。

发现并修复 tools/runtime 共享动态目录跨会话使用同名工具实现的问题，改为执行级不可变目录快照；本地 batch 通过 ToolCall 的非序列化 runtime state 将审批后的快照传递到工具正文。

连接凭证由 bridge 生命周期拥有。同一 bridge 的工作区变化更新 turn 上下文，不重新生成客户端无法接收的凭证；旧 bridge 的清理不能撤销新 bridge 的作用域。

验证与使用说明见 `docs/session-mcp-runtime.zh.md` 及相关测试。
