---
name: tool-developer
description: 开发 AgentKit 原生 Go 工具插件（tool/*）。在新增 tool 插件、注册 pluginkit kind、挂载 tools/runtime、实现 Tool/ToolPack/ToolProvider、推导 input schema 或编写工具单元测试时使用。
---

# Tool 开发指南

## 选型

| 方式 | 适用场景 |
|---|---|
| **Go 插件 `tool/*`** | 需要强类型、与 session/workspace 深度集成、复杂业务逻辑 |
| **MCP `tool/mcp`** | 已有 MCP server 或希望进程外隔离 |
| **OpenAPI `tool/openapi`** | 已有 REST API，用 operationId 暴露为工具 |

本 Skill 覆盖 **Go 原生工具插件**。MCP / OpenAPI 只需写 `mcp.json` / `api.json` 磁盘配置并重启进程，不必写 Go 代码。

## 三种返回类型

| 返回类型 | kind 示例 | 挂载槽位 |
|---|---|---|
| `agentkit.Tool` | `tool/finish`、`tool/todo` | `tools/runtime` → `deps.tools` |
| `agentkit.ToolPack` | `tool/fs-workspace` | `deps.toolPacks` |
| `agentkit.ToolProvider` | `tool/mcp`、`tool/openapi` | `deps.dynamicTools` |

大多数新工具是单工具 `Tool` 或多工具 `ToolPack`。

## 目录与注册

```
plugins/tool/<name>/
├── <name>.go      # Config、Deps、New 构造函数、handler
└── register.go    # init() + pluginkit.Register
```

`register.go`：

```go
package mytool

import "github.com/lengzhao/pluginkit"

func init() {
    pluginkit.Register("tool/my-tool", New)
}
```

kind 命名：`tool/<小写-连字符>`，与 `pluginkit.Register` 第一个参数一致。

新增包后执行 **`go generate ./...`**，将 blank import 写入 `plugins/all.go`；否则 `init()` 不会执行，kind 无法装配。

## 单工具模板

参考 `plugins/tool/finish/finish.go`：

```go
package mytool

import (
    "context"
    "fmt"

    "github.com/lengzhao/agentkit"
    "github.com/lengzhao/agentkit/cap/workspace"
)

type Config struct {
    TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type Deps struct {
    Workspace workspace.Service `json:"workspace"`
}

type Input struct {
    Query string `json:"query" jsonschema:"What to look up"`
}

type Output struct {
    Result string `json:"result"`
}

func New(cfg Config, deps Deps) (agentkit.Tool, error) {
    if deps.Workspace == nil {
        return nil, fmt.Errorf("tool/my-tool requires workspace")
    }
    return agentkit.NewTool[Input, Output]("my_tool", func(ctx context.Context, in Input) (Output, error) {
        // 业务逻辑
        return Output{Result: in.Query}, nil
    }).
        Description("One-line description for the model.").
        Build()
}
```

构造函数签名只能是 `New()`、`New(cfg Config)`、`New(cfg, deps Deps)` 三种之一；依赖通过 **Deps struct** 注入，不从全局变量或 context 偷拿服务。

## 多工具 ToolPack

参考 `plugins/tool/fs/fs_workspace.go`：在 `New` 里用多个 `agentkit.NewTool` 构建，最后 `agentkit.Pack(read, write, ...)` 返回 `ToolPack`。

## Input Schema 约定

- JSON Schema 由输入类型 `In` 自动推导（`json` tag 定字段名，`jsonschema` tag 定描述）。
- 无 `omitempty` / `omitzero` 的字段为 **required**。
- handler 输出：`string` 原样返回；其他类型 JSON 序列化后给模型。
- handler 返回 `error` 时，错误文本会作为 tool result 给模型（不 panic）。
- 需要自定义 schema 时用 `.Schema(...)` 覆盖。

## Context 常用键

从 `ctx` 读取（勿写入）：

| 键 | 类型 | 用途 |
|---|---|---|
| `session.SessionIDFromContext(ctx)` | `SessionID` | 当前 conversation（历史/锁 key） |
| `session.AgentIDFromContext(ctx)` | `AgentID` | 当前 agent |
| `session.PlatformFromContext(ctx)` | `string` | 当前 platform |
| `session.UserIDFromContext(ctx)` | `string` | 当前用户 |

需要 session 历史或写事件时，通过 `deps.sessionStore` 注入 `agentkit.SessionStore`。

## 挂载到配置

在 `config.base.yaml`、preset 或 `config.yaml` 增加实例，并挂到 `tools.default`：

```yaml
tool.my-tool.default:
  use: tool/my-tool
  config:
    timeoutSeconds: 30
  deps:
    workspace: workspace.default

tools.default:
  use: tools/runtime
  config:
    defaultTimeoutSeconds: 120
    toolTimeouts:
      my_tool: 60          # 按模型工具名单独放宽
  deps:
    hooks: hooks.default
    tools:
      - tool.my-tool.default   # 追加，保留原有条目
      - tool.shell-bash.default
      # ...
    policies:
      - policy.dangerous-shell.default
```

**同 id 整颗替换**：改 `tools.default` 时须写全 `deps.tools` 列表，漏写会从图中消失。

子 agent 工具集是独立实例 `tools.subagent.default`，默认不含 `delegate`；新工具若只给主 agent 用，只挂 `tools.default`。

## 开发与验证流程

1. 实现 `plugins/tool/<name>/` + 单元测试。
2. `go generate ./...` 注册 import。
3. 在 YAML 挂载实例。
4. `go test ./plugins/tool/<name>/...` 与相关 smoke。
5. **重新编译并重启 agent 进程**，新工具才会出现在模型侧。

Go 插件**不支持热加载**；改 `.go` 或 YAML 后必须重新 build + restart。

## 单元测试

直接用 `agentkit.NewTool` 构造工具，无需完整 Runner：

```go
func TestMyTool(t *testing.T) {
    tool, err := New(Config{}, Deps{Workspace: ws})
    if err != nil {
        t.Fatal(err)
    }
    ctx := agenttest.TurnContext(agentkit.SessionID("test"), agentkit.AgentID("test"))
    out := agenttest.CallTool(t, ctx, tool, `{"query":"hello"}`)
    if !strings.Contains(out, "hello") {
        t.Fatalf("unexpected: %s", out)
    }
}
```

需要 tool runtime（policy、超时）时用 `agenttest.ToolsRuntime`；端到端用 `agenttest.NewScriptedAgent` + `agenttest.RunTurn`（见 `testing/smoke/tool_test.go`）。

文件类工具测试优先用 `tool/fs-memory`（`NewFSMemory`），避免碰真实磁盘。

## 设计原则

- **构造函数轻量**：不启动 goroutine、不读全局配置、不向 bus 注册。
- **返回模型可见纯文本**：`Tool.Call` 输出即模型看到的内容；审计信息走 tool runtime 的 `Audit` 字段，不由 handler 写 session。
- **拒绝走 Policy**：危险操作通过 `policy/*` 插件拦截，不在 tool handler 里静默吞掉。
- **日志用 `slog`**：Warn 级别记录可恢复异常（如单个远端连不上），不 println。
- **依赖 cap 接口**：路径解析用 `workspace.Service`，密钥用 `credentials`，不硬编码 `~/.agentkit`。
- **超时由 runtime 管**：在 `tools/runtime` 的 `defaultTimeoutSeconds` / `toolTimeouts` 配置，handler 内另设 HTTP 超时除外。

## 动态工具 ToolProvider

仅当工具列表在运行时变化（读 JSON、远程发现）时实现 `ToolProvider`：

```go
type Provider struct { /* ... */ }

func (p *Provider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
    // 每次 turn 可重新发现；通常内部有缓存 + 显式 reload 入口
}
```

参考 `plugins/tool/mcp/mcp.go`、`plugins/tool/openapi/openapi.go`。维护磁盘配置比写 ToolProvider 简单时，优先 MCP / OpenAPI。

## 检查清单

- [ ] `pluginkit.Register("tool/...", New)` 与 YAML `use:` 一致
- [ ] `go generate ./...` 后 `plugins/all.go` 含 blank import
- [ ] Config / Deps 字段有 `json` tag，与 YAML `config` / `deps` 对齐
- [ ] 模型工具名稳定、小写、语义清晰（如 `my_tool`，不是包名）
- [ ] Description 写清何时调用、输入含义、失败表现
- [ ] 单元测试覆盖主路径与输入校验
- [ ] YAML 已挂载且告知用户重新编译重启
