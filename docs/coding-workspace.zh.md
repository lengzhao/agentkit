# Coding Agent 工作区（Filesystem）

Coding preset 的文件与 Shell 边界策略。

## 结论

`workspace/default` 同时定义 **global**（默认 `~/.agentkit`）与 **local**（默认 `<cwd>/.agentkit`）两个根，并通过 `scope` 选择默认使用哪一个（L0 默认 `global`）。

AgentKit 运行时数据（session、schedule、agents 定义等）落在 **scope 默认根** 下。L0 默认 `scope: global`，工具只在 `work/` 子目录读写，避免误伤 `sessions/` 或 `mcp.json`。coding preset 覆盖为 `scope: local`，并通过 `..` 解析到**项目根目录**。

需要解析相对路径的插件（`tool/fs-workspace`、`tool/shell-bash`、`session/store`、`skill/filesystem` 等）调用 `workspace.Resolve(ctx, rel)`。路径可加前缀显式指定根：

| 写法 | 含义 |
|---|---|
| `sessions` | 相对当前 `scope` 默认根 |
| `global:skills` | `~/.agentkit/skills` |
| `local:skills` | `<cwd>/.agentkit/skills` |
| `..` | 项目根（local 根 `.agentkit` 的父目录） |
| `~/foo` | 绝对路径（不受 scope 影响） |

| 实例 | Kind | 配置 | 解析结果 |
|---|---|---|---|
| `workspace.default` | `workspace/default` | `global` + `local` + `scope` | 默认根由 scope 决定 |
| `tool.fs-workspace.default` | `tool/fs-workspace` | `root: work`（L0） | `~/.agentkit/work/`（scope=global） |
| `tool.fs-workspace.default` | `tool/fs-workspace` | `root: ..`（coding preset） | 项目根目录 |
| `tool.shell-bash.default` | `tool/shell-bash` | `workDir: work`（L0） | `~/.agentkit/work/`（scope=global） |
| `tool.shell-bash.default` | `tool/shell-bash` | `workDir: ..`（coding preset） | 项目根目录 |
| `sessionStore.default` | `session/store` | `dir: sessions` | `<cwd>/.agentkit/sessions`（scope=local） |

`config.base.yaml`（L0）默认 `scope: global`，fs/shell 在 `work/` 子目录操作；`presets/coding.yaml`（L1）覆盖为 `scope: local`，并把 fs/shell 指到 `..`（项目根）。

Session 日志不属于工具 FS 工作区，但目录同样通过 `workspace` 解析。

## 工具绑定

```yaml
workspace.default:
  use: workspace/default
  config:
    scope: local

tool.fs-workspace.default:
  use: tool/fs-workspace
  config:
    root: ..
  deps:
    workspace: workspace.default
```

- **read / grep / find / ls** 走只读包装，即使工具实现有 write 能力也无法落盘。`grep` / `find` 会跳过 `.gitignore` 匹配路径（并默认忽略 `.git`、`node_modules`、`.agent`）。
- **write / edit** 直接绑定 workspace，可修改项目内文件。
- **shell** 在 workspace 解析后的 workDir 执行，与文件工具路径语义一致。

## Skills 目录叠加

```yaml
skills.default:
  use: skill/filesystem
  config:
    dirs:
      - local:skills
      - local:.agentkit/skills
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
