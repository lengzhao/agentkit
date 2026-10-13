---
name: workflow-manager
description: 编写、调优、排查 AgentKit tool/workflow 声明式管线（workflows/*.json）。在 (1) 新增/修改 workflow 或 workflows/scripts、(2) 用 workflow 工具试跑或迭代失败 step、(3) 设计 script→tool/model/agent 串联且大 payload 只传路径、(4) 说明或修改 tools.runtime.workflow（含 bash 与 step 所需动态工具）、(5) workflow 未被发现、args 校验、bash/model/agent step 报错时使用。字段与模板细则读 skill 内 references/schema.md；YAML 装配读 references/assembly.md。
---

# Workflow 维护

## 运行态

| 项 | 行为 |
|---|---|
| 路径 | `local:workflows`（`work/workflows/`，优先）→ `global:workflows`（如 `.agentkit/workflows/`）；**name = 文件名去 `.json`** |
| 生效 | 保存 JSON/脚本后**下一轮**即生效；`workflow` 描述与 **`/workflow`** 每次重读目录，**无 reload 命令** |
| 不变量 | 主对话不接触大 payload；中间数据用 `{{ .workflow.runDir }}` 与 `.steps.<id>.path` |
| 前置 | `tool/workflow` 已挂主 runtime；`deps.runtime` 含 **`bash`** + tool step 工具，**不含** `tool/workflow` |
| 重启 | 仅改 YAML（dirs、sibling runtime、挂 workflow 工具）需重启进程 |

仓库设计文档：`docs/guides/workflow.zh.md`（与代码冲突时以代码为准）。

## 作者流程

1. 确认 inputs 仅为小参数（ID、日期）；大文件只在 step 间用路径传递。
2. 在 `work/workflows/`（租户）或 `global:workflows/`（共享）写 `*.json`；脚本与 JSON **同目录**或 `scripts/` 子目录；script 经 **`bash`**（`cd <workflow目录> && run`）。
3. 对照 [references/schema.md](references/schema.md) 填 `description`（必填）、`inputs`、`steps`、`result`；可从 [assets/minimal-workflow.json](assets/minimal-workflow.json) 复制骨架。
4. **试跑**：`workflow` → `{"name":"<name>","args":{…}}`；列表用 **`/workflow`**。错误含 **step id**，按 id 改对应 step。
5. 收紧 `description`（模型选路）与 `result`（短摘要）；勿把 step 全文日志放进 `result`。

`tool` step 使用**模型可见工具名**（如 `prefix__operation`）。动态工具未出现时，先请用户 `/mcp -u` 或 `/openapi -u`，再查工具列表。

## 分场景读参考

| 任务 | 读 |
|---|---|
| 字段、step 类型、模板、`dig`/`quote`、场景与反模式 | [references/schema.md](references/schema.md) |
| `tool.workflow` / `tools.runtime.workflow` YAML | [references/assembly.md](references/assembly.md) |
| 冒烟样例 | 本仓库 `global:workflows/ping.json`、`marker.json` |

## 快速排查

| 现象 | 先查 |
|---|---|
| 描述里无新 workflow | 目录、`description` 必填、JSON 合法；描述中 `[load error]` |
| script 失败 | sibling runtime 是否含 `bash`；`run` 与 `quote` |
| tool 失败 | 工具是否在 `tools.runtime.workflow`；工具名是否为模型侧名称 |
| model/agent 失败 | 是否配 `deps.llm` / `deps.subagent`；prompt 是否超 `maxInputBytes` |

更多条目见 [references/schema.md#排查](references/schema.md#排查)。
