# Coding Agent 工作区（Filesystem）

Coding preset 的文件与 Shell 边界策略。

## 结论

工作目录由 `workspace/default` 插件定义；需要解析相对路径的插件（`fs/local`、`shell/bash`、`session/store` 等）**依赖** `workspace` 实例，在运行时调用 `workspace.Resolve(ctx, rel)`。要换隔离策略（如按 session 分目录、沙箱根），只需替换 `workspace` 插件实现。

| 实例 | Kind | 配置 | 解析结果 |
|---|---|---|---|
| `workspace.default` | `workspace/default` | `root: .` 或 `~/.agentkit` | 工作区根（绝对路径） |
| `fs.workspace.default` | `fs/local` | `root: .` | `workspace.Resolve(ctx, ".")` |
| `shell.bash.default` | `shell/bash` | `workDir: .` | 工作区根 |
| `sessionStore.default` | `session/store` | `dir: .agent/sessions` | `工作区根/.agent/sessions` |

`config.base.yaml`（L0）默认 `workspace.default.config.root: ~/.agentkit`；`presets/coding.yaml`（L1 overlay）覆盖为 `.`（当前项目目录）。

Session 日志不属于工具 FS 工作区，但目录同样通过 `workspace` 解析。

## 工具绑定

```yaml
workspace.default:
  use: workspace/default
  config:
    root: .

runner.default:
  deps:
    platform: platform.default
    loop: loop.default

fs.workspace.default:
  use: fs/local
  config:
    root: .
  deps:
    workspace: workspace.default

tool.read-file.default:
  deps:
    fs: fs.workspace.readonly.default
```

- **read / grep / find / ls** 走只读包装，即使工具实现有 write 能力也无法落盘。
- **write / edit** 直接绑定 workspace，可修改项目内文件。
- **shell** 在 workspace 根目录执行，与文件工具路径语义一致。

## 隔离扩展

自定义 `workspace/*` 插件可实现 `cap/workspace.Service`：

```go
type Service interface {
    Resolve(ctx context.Context, rel string) (string, error)
}
```

实现内可读取 `ctx.Value(agentkit.KeySessionID)` 等，将不同 session 映射到不同根目录；下游 `fs/local`、`session/store` 等无需改动。

## 只读审查模式

只读 Agent 可将 write/edit 工具从 tools 列表移除，或把 write/edit 的 `fs` deps 改为 `fs.workspace.readonly`（写入会在 FS 层被拒绝）。

## 相关

- [plugin-catalog.zh.md](plugin-catalog.zh.md) — `workspace/default`、`fs/local`、`fs/readonly`
- [presets/coding.yaml](../presets/coding.yaml) — 项目目录 L1 preset
- [config.base.yaml](../config.base.yaml) — L0 默认实例图
- [config.example.yaml](../config.example.yaml) — L1 override 示例
