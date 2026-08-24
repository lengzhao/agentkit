# Coding Agent 工作区（Filesystem）

Coding preset 的文件与 Shell 边界策略。

## 结论

`workspace/default` 同时定义 **global**（默认 `~/.agentkit`）与 **local**（默认当前目录 `.`）两个根，并通过 `scope` 选择默认使用哪一个（L0 默认 `global`）。

需要解析相对路径的插件（`fs/local`、`shell/bash`、`session/store`、`skill/filesystem` 等）调用 `workspace.Resolve(ctx, rel)`。路径可加前缀显式指定根：

| 写法 | 含义 |
|---|---|
| `sessions` | 相对当前 `scope` 默认根 |
| `global:skills` | `~/.agentkit/skills` |
| `local:skills` | `./skills`（项目目录下） |
| `~/foo` | 绝对路径（不受 scope 影响） |

| 实例 | Kind | 配置 | 解析结果 |
|---|---|---|---|
| `workspace.default` | `workspace/default` | `global` + `local` + `scope` | 默认根由 scope 决定 |
| `fs.workspace.default` | `fs/local` | `root: .` | `workspace.Resolve(ctx, ".")` |
| `shell.bash.default` | `shell/bash` | `workDir: .` | 工作区默认根 |
| `sessionStore.default` | `session/store` | `dir: sessions` | 相对默认根 |

`config.base.yaml`（L0）默认 `scope: global`；`presets/coding.yaml`（L1）覆盖为 `scope: local`，文件工具与 shell 在项目目录执行。

Session 日志不属于工具 FS 工作区，但目录同样通过 `workspace` 解析。

## 工具绑定

```yaml
workspace.default:
  use: workspace/default
  config:
    global: ~/.agentkit
    local: .
    scope: local

fs.workspace.default:
  use: fs/local
  config:
    root: .
  deps:
    workspace: workspace.default
```

- **read / grep / find / ls** 走只读包装，即使工具实现有 write 能力也无法落盘。
- **write / edit** 直接绑定 workspace，可修改项目内文件。
- **shell** 在 workspace 默认根执行，与文件工具路径语义一致。

## Skills 目录叠加

```yaml
skills.default:
  use: skill/filesystem
  config:
    dirs:
      - local:skills
      - local:.cursor/skills
      - global:skills
  deps:
    workspace: workspace.default
```

列表顺序决定扫描优先级；同名 skill 以先出现的为准。

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
