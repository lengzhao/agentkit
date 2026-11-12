# AgentKit 项目规则

适用于本仓库的所有 coding agent。修改前阅读本文件；架构背景见 `docs/go-agent-harness-architecture.zh.md`，插件目录见 `docs/plugin-catalog.zh.md`。

## 组合优先

- 新增能力前，先检查现有插件、runtime、preset/overlay、chain 和 workflow。能通过现有能力组合满足需求时，不再开发重复能力。
- 确需开发时，先说明现有能力的缺口，选择最小实现位置；组合方案也必须满足权限、生命周期和错误处理语义。
- 复用 PluginKit 的注册、配置识别与实例图构建，不另建 registry、服务容器或依赖注入机制。

## 插件边界

- 插件保持高内聚、低耦合，围绕单一职责，只暴露对应职责所需的最小接口能力。
- 依赖通过 `Deps` 和配置图注入；跨插件通过根包或 `cap/*` 契约协作，不直接 import 其他插件实现，也不通过全局状态或具体类型断言绕过边界。
- 公共实现放 `runtime/*`，公共契约按需提取；不为单个实现的便利扩大公共接口。编排逻辑放在组合组件中。
- 构造函数只做轻量初始化与依赖组装；后台任务接入既有生命周期，由明确的 owner 启停和取消。

## 公共契约变更须经用户确认

- 根目录 `agentkit` 包和 `cap/*` 的接口、公共类型、字段、常量、事件结构及行为契约，任何新增、删除或调整，必须先分析**必要性、通用性、兼容性**，说明影响范围、迁移方案和验证方式，获得用户对具体方案的明确确认后才可修改。
- 一般性的功能或修复授权不等于契约变更授权；新增字段或接口方法也不视为天然兼容。不得将契约藏入 runtime 或匿名类型断言来绕过确认。
- 纯排版和不改变契约的测试修改不在此限制内；现存例外不构成新增例外的依据。

## 运行时不变量

- 区分 conversation/session、delivery route、agent、workspace 和 execution identity；通过既有 envelope/rctx 能力传递，不猜测 ID 或重复建立身份来源。
- 保持同会话保序、跨会话隔离及取消的执行归属；模型可见内容必须能从 Session 记录重建，保持工具调用/结果配对、压缩与恢复语义。
- `rctx.State` 仅用于 turn 内的优化状态，缺失时可无状态降级；持久状态沿用既有会话或存储机制。
- 文件、工作区、凭据和沙箱能力沿用已注入的接口，不绕过租户隔离；后台调度不得意外变成等待人工交互的执行。

## 配置与验证

- 修改配置或装配时，检查 `Deps` JSON tag、注册 kind、默认值、overlay 顺序、extends、禁用与裁剪，以及已有 preset 的兼容性。
- `plugins/all.go` 用 `go generate` 更新；配置目录用 `go run ./scripts/gen-config-catalog` 更新。生成文件和 golden 的差异必须能解释。
- 测试统一入口为 `scripts/test.sh`：代码改动交付前运行 `unit`；装配/preset 改动增加 `integration`；沙箱隔离改动验证 Linux `sysint`。环境不支持时明确报告未验证项，不弱化 CI。
- 装配测试复用 `testing/agenttest`、`testing/presettest` 与生产 PluginKit 路径；会话、并发、取消、恢复改动覆盖对应边界。

## 代码分析入口

- 每个项目首次分析代码前执行 `codegraph init`；优先使用 CodeGraph 定位符号和调用关系。初始化或查询失败时先说明，再使用其他方式分析。
