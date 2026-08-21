# Coding Agent 工作区（Filesystem）

Coding preset 的文件与 Shell 边界策略。

## 结论

早期 `config.yaml` 中 `fs.local.agent`（`.agent/`）与 `fs.local`（`.`）混用属于**配置疏漏**，不是有意隔离。当前统一为：

| 实例 | Kind | 作用 |
|---|---|---|
| `fs.workspace` | `fs/local` | 项目工作区，root = `.` |
| `fs.workspace.readonly` | `fs/readonly` | 包装 `fs.workspace`，供 read 工具只读访问 |
| `shell.bash` | `shell/bash` | `workDir: .`，与 workspace 对齐 |

Session 日志（`.agent/sessions/`）是 Agent 运行时状态，**不属于**工具 FS 工作区。

## 工具绑定

```yaml
tool.read-file:
  deps:
    fs: fs.workspace.readonly

tool.write-file:
  deps:
    fs: fs.workspace

tool.edit-file:
  deps:
    fs: fs.workspace

fs.workspace:
  use: fs/local
  config:
    root: .

fs.workspace.readonly:
  use: fs/readonly
  deps:
    fs: fs.workspace
```

- **read** 走只读包装，即使工具实现有 write 能力也无法落盘。
- **write / edit** 直接绑定 workspace，可修改项目内文件。
- **shell** 在 workspace 根目录执行，与文件工具路径语义一致。

## 只读审查模式

只读 Agent 可将 write/edit 工具从 tools 列表移除，或把 write/edit 的 `fs` deps 改为 `fs.workspace.readonly`（写入会在 FS 层被拒绝）。

## 相关

- [plugin-catalog.zh.md](plugin-catalog.zh.md) — `fs/local`、`fs/readonly`
- [presets/coding.yaml](../presets/coding.yaml) — 默认实例图
