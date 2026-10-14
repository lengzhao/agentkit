---
name: workflow-manager
description: 编写、修改、试跑和排查 AgentKit 的 workflows/*.json 管线（script、tool、model、agent step）。在这些情况使用：(1) 新建或修改 workflow 文件与同目录脚本，(2) 下游 tool 的参数是 object/array 或大 JSON 不能进主模型，(3) 按 step id 修失败步骤，(4) 修改 tools.runtime.workflow 或 tool.workflow.default。没有名为 workflow 的工具，模型入口是 workflow__<文件名>。字段读 references/schema.md，YAML 读 references/assembly.md。
---

# Workflow 维护

没有名为 `workflow` 的工具。调用 **`workflow__` + 文件名（去掉 `.json`）**。

与 `docs/guides/workflow.zh.md` 冲突时以代码为准。

## 分流

1. 判断任务：
   - **写或改管线** → 写文件
   - **改 dirs、sibling runtime、telemetry** → 只读 [references/assembly.md](references/assembly.md)，改完重启进程
   - **某 step 已失败** → 读 [references/schema.md](references/schema.md) 的排查表，按报错里的 step id 改对应 step

## 写文件

1. `inputs` 只放小参数（ID、日期、datasetKey）。大 JSON 写到 `{{ .workflow.runDir }}` 下的文件。
2. 文件放本地路径（优先）或全局路径。脚本与 JSON 同目录，或放 `scripts/`。
3. 从 [assets/minimal-workflow.json](assets/minimal-workflow.json) 复制骨架。字段规则读 [references/schema.md](references/schema.md)。
4. 按下游 tool 的 schema 写 `args`：
   - 工具收 **path**：`{{ .workflow.runDir }}/文件`
   - 工具收 **object 或 array**（没有 path 字段）：整段 `readJSON`，或上一步已是 JSON 时 `{{ .steps.<id>.json }}`。从 [assets/object-args-workflow.json](assets/object-args-workflow.json) 复制。不要把 JSON 文本填进该字段。
5. `tool` 填模型可见名（如 `prefix__operation`）。看不到 MCP/OpenAPI 工具时，让用户执行 `/mcp -u` 或 `/openapi -u`。
6. 试跑：调用 `workflow__<文件名>`，入参即 `inputs`。披露开启时先 `tool_search` 这个名字。列表让用户执行 `/workflow`。
7. `description` 写给模型选路。`result` 只留短摘要，不放 step 全文。

保存 JSON 或脚本后下一轮生效，不用 reload。只改 YAML 才重启。sibling `deps.runtime` 要包含 step 调用的工具，且不能包含 `tool/workflow`。
