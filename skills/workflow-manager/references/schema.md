# Workflow JSON 与模板参考

## 顶层

```json
{
  "description": "必填：模型靠此选 workflow",
  "inputs": { "type": "object", "properties": {}, "required": [] },
  "steps": [ { "id": "s1", "type": "script", "run": "echo ok" } ],
  "result": "必填：text/template，渲染结果返回主模型"
}
```

| 字段 | 说明 |
|---|---|
| `description` | 必填 |
| `inputs` | 可选 JSON Schema；有则 `workflow` 的 `args` 校验失败即工具错误 |
| `steps` | 必填，**串行**；`id` 唯一 |
| `result` | 必填 |

## Step 类型

| type | 必填 | 要点 |
|---|---|---|
| `script` | `run` | **优先 `deps.shell`**（StructuredShell）直接执行；未配时回退 sibling **`bash`** 并解析文案（不推荐）。可选 `dir`（默认 workflow 文件目录）、`timeoutSeconds` |
| `tool` | `tool`, `args` | sibling `ToolRuntime`；policy/hooks 生效；`args` 叶子字符串可模板化 |
| `model` | `prompt` | 单次补全；可选 `model`、`outputSchema`、`maxInputBytes`（默认 16KB，超限 **fail**） |
| `agent` | `agent`, `task` | 同步子 agent；task 自包含；大正文用路径 |

公共：`continueOnError`、`timeoutSeconds`（≥0）。

### 示例：script → tool（by reference）

```json
"steps": [
  { "id": "gen", "type": "script",
    "run": "python3 scripts/gen.py {{ .inputs.date | quote }} --out {{ .workflow.runDir }}/out.json" },
  { "id": "submit", "type": "tool", "tool": "myapi__import",
    "args": { "path": "{{ .workflow.runDir }}/out.json" } }
],
"result": "status={{ dig .steps.submit.json \"status\" }}"
```

### 示例：model 做格式转换

```json
{ "id": "cvt", "type": "model", "model": "gpt-5.4-mini",
  "prompt": "把小摘要转为写库 JSON：{{ .steps.gen.stdout }}",
  "outputSchema": { "type": "object", "properties": { "id": { "type": "string" } }, "required": ["id"] } }
```

## 模板上下文

```
.inputs.<name>
.workflow.name / .workflow.dir / .workflow.runDir
.steps.<id>.stdout | .text | .path | .json | .status | .session
```

函数：**`dig`**、**`quote`**、**`join`**  only（无读文件、无 sprig）。

## 数据传递

- 大 payload：**`.path`** 或 **`runDir` 下文件**；下游脚本/工具自己读。
- 小数据：**`dig`** 或短 `.stdout`。
- 需消化长文：**`agent` step**，task 里给路径，不要塞进 model `prompt`。

## 场景

| 场景 | 链 |
|---|---|
| 生成再调 API | script → tool（args 仅 path） |
| 结构化再写库 | script → model（outputSchema）→ tool |
| 长文分析 | agent（路径在 task） |

## 反模式

- 主 turn `read` 大 JSON 再整段填入 `workflow` args。
- `result` 或 model `prompt` 展开全文日志/文件内容。
- workflow 内嵌套调 `workflow`（runtime 通常不含且易环）。
- script 里 `curl` 绕过 `tool` step（失去 policy/审计）。

## 排查

| 现象 | 常见原因 |
|---|---|
| `script step requires tools runtime (bash)` | `tools.runtime.workflow` 未挂 shell-bash |
| 工具不存在 | 工具未加入 sibling runtime；名称非模型可见名 |
| `maxInputBytes` / model 依赖 | 未配 `deps.llm`；prompt 过大 |
| `agent step requires a subagent dependency` | 未配 `deps.subagent` |
| bash 非零退出 | `run` 语法、`quote`、路径 |
| `telemetry` 装配失败 | 用 `telemetry.toolkit`，不是 Langfuse exporter |
