# Workflow JSON 与模板

写文件前先读 [SKILL.md](../SKILL.md) 的分流。本文件是字段和模板。

## 顶层

| 字段 | 说明 |
|---|---|
| `description` | 必填。即 `workflow__<文件名>` 的模型侧描述 |
| `inputs` | 可选 JSON Schema，即该工具的 InputSchema。引擎校验失败则返回工具错误 |
| `steps` | 必填。默认按文件顺序串行；`dependsOn` 可组 DAG（同层并行）。`id` 唯一 |
| `result` | 必填。text/template，渲染后返回主模型 |

骨架：[assets/minimal-workflow.json](../assets/minimal-workflow.json)。object 参数骨架：[assets/object-args-workflow.json](../assets/object-args-workflow.json)。条件/并行 DAG 骨架：[assets/dag-workflow.json](../assets/dag-workflow.json)。

## Step

| type | 必填 | 要点 |
|---|---|---|
| `script` | `run` | 优先 `deps.shell`。未配时回退 sibling `bash`（不推荐）。`dir` 默认 workflow 文件目录 |
| `tool` | `tool`, `args` | sibling `ToolRuntime`，policy/hooks 生效。`args` 叶子字符串按模板渲染 |
| `model` | `prompt` | 单次补全。可选 `model`、`outputSchema`、`maxInputBytes`（默认 16KB，超出即失败） |
| `agent` | `agent`, `task` | 同步子 agent。task 自包含；长文只传路径 |

公共字段：`continueOnError`、`timeoutSeconds`（≥0）、`when`、`dependsOn`。

- `when`：模板，依赖完成后渲染；结果为 `""`/`"0"`/`"false"` 时跳过该 step（`status: "skipped"`）。条件用模板内置 `eq`/`ne`/`lt`/`gt`/`and`/`or`/`not`，如 `{{ eq (dig .steps.check.json "ok") true }}`。
- `dependsOn`：缺省 = 依赖前一个 step（串行）；显式数组只依赖列出的 id，空数组 `[]` 断开串行边、与前面 step 并行。引用必须存在、全图无环。同层并行 step 不要写同一文件。
- `status` 取值：`ok` / `error` / `skipped`。依赖失败（`continueOnError`）或跳过**不会**自动阻止下游，下游用 `when` 自行裁决。

## 模板

```
.inputs.<name>
.workflow.name / .workflow.dir / .workflow.runDir
.steps.<id>.stdout | .text | .path | .json | .status | .session
```

函数只有 `dig`、`quote`、`join`、`readJSON`。`readJSON` 只读 workflow 目录或本次 `runDir` 下的 JSON object/array。

script 的 stdout、tool 的结果若整段是 JSON object/array，写入 `.steps.<id>.json`。

## args 怎么写

| 下游 schema | 写法 |
|---|---|
| 收 path | `"path": "{{ .workflow.runDir }}/out.json"` |
| `type: object` 或 `array`，且没有 path | `"payload": "{{ readJSON (printf \"%s/payload.json\" .workflow.runDir) }}"`，或 `"payload": "{{ .steps.<id>.json }}"` |
| 整数，且整段是一次插值 | `"expectedVersion": "{{ dig .steps.prev.json \"version\" }}"` |
| 字符串 | 静态文本或 `quote`。`"42"` 仍是字符串 |

object/array 叶子会解析后再传给 tool。数字保持 JSON 字面量，不变成 `float64`。路径、备注、静态 `"42"` 保持字符串。

```json
{ "id": "update", "type": "tool", "tool": "capitaldesk_update_dataset",
  "args": {
    "datasetKey": "{{ .inputs.datasetKey }}",
    "expectedVersion": "{{ dig .steps.prev.json \"version\" }}",
    "payload": "{{ readJSON (printf \"%s/payload.json\" .workflow.runDir) }}"
  } }
```

model step 用 `outputSchema` 把小摘要收成对象后，同样用 `"meta": "{{ .steps.cvt.json }}"` 传入下一步。prompt 超过 `maxInputBytes` 会失败，不要把大文件贴进 prompt。

## 反模式

- 调用名为 `workflow` 的工具。入口是 `workflow__<文件名>`。
- 主 turn 读取大 JSON，再填进 `workflow__*` 的入参。
- 把 JSON 文本放进 `type: object` 字段。
- `result` 或 model `prompt` 展开全文。
- step 里再调 `workflow__*`（sibling runtime 不含 `tool/workflow`）。
- script 里用 `curl` 代替 tool step。

## 排查

| 现象 | 处理 |
|---|---|
| 找不到工具 `workflow` | 改调 `workflow__<文件名>`。列表用 `/workflow` |
| 工具列表没有 `workflow__<name>` | 查目录、`description`、JSON 是否合法。看 `/workflow` 里的 `[load error]` |
| expected object, got string | 该字段改成整段 `readJSON` 或 `.steps.<id>.json` |
| `readJSON` outside the workflow directory | 路径限制在 workflow 目录或本次 `runDir`，且内容是 object/array |
| `script step requires tools runtime (bash)` | `tools.runtime.workflow` 挂上 shell-bash，或配置 `deps.shell` |
| 工具不存在 | 加入 sibling runtime；`tool` 用模型可见名 |
| `maxInputBytes` / model 依赖 | 配置 `deps.llm`；缩小 prompt |
| `agent step requires a subagent dependency` | 配置 `deps.subagent` |
| bash 非零退出 | 查 `run`、`quote`、路径 |
| telemetry 装配失败 | 用 `telemetry.toolkit`，不用 Langfuse exporter |
