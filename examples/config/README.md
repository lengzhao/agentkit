# 配置示例

L1 overlay，叠加在 L0 `config.base.yaml` 之上。`-config` 接受逗号分隔的多个文件，按顺序合并，后面的覆盖前面的。

```sh
# 单文件
go run ./cmd/agent -config examples/config/local-coding.yaml "你的任务"

# 链式合并（自主运行 + headless worker）
go run ./cmd/agent -config examples/config/autonomous.yaml,examples/config/headless-worker.yaml "一次性任务"
```

## 场景索引

| 文件 | 场景 | 说明 |
|---|---|---|
| [local-coding.yaml](local-coding.yaml) | 项目目录 coding | `scope: local`，session 落在 `.agent/sessions` |
| [custom-model.yaml](custom-model.yaml) | 换模型 / 端点 | 覆盖 `llm.default` |
| [custom-prompt.yaml](custom-prompt.yaml) | 自定义 system prompt | 插入静态指令段 |
| [once-task.yaml](once-task.yaml) | 单次任务 | `platform/cli` + `once: true`，跑完退出 |
| [no-api-smoke.yaml](no-api-smoke.yaml) | 无 API Key 冒烟 | scripted LLM，本地验证装配与工具链 |
| [autonomous.yaml](autonomous.yaml) | 自主运行 | 预算、todo/finish、auto-allow + policy 白名单 |
| [headless-worker.yaml](headless-worker.yaml) | headless 一次性 | 不读 stdin，适合 CI / 系统 cron；**需链 autonomous** |
| [interval-daemon.yaml](interval-daemon.yaml) | 固定间隔守护 | `platform/timer`，每 N 秒巡检；**需链 autonomous** |
| [cron-daemon.yaml](cron-daemon.yaml) | cron 守护 | 5 段 cron + `tool/schedule`；task 支持 `prompt` 与 `script` 两种模式 |
| [custom-schedule.yaml](custom-schedule.yaml) | 自定义排期存储 | job 表路径与 agent job 上限 |

## 常用命令

```sh
export OPENAI_API_KEY=sk-...

# 交互式 REPL + 项目目录
go run ./cmd/agent -config examples/config/local-coding.yaml

# 自主运行（stdin 给首条任务，跑完退出）
go run ./cmd/agent -config examples/config/autonomous.yaml "整理 docs 并收尾"

# headless 批处理（stdin 是 /dev/null 也安全）
go run ./cmd/agent -config examples/config/autonomous.yaml,examples/config/headless-worker.yaml "跑测试并汇报"

# 常驻：固定间隔
go run ./cmd/agent -config examples/config/autonomous.yaml,examples/config/interval-daemon.yaml

# 常驻：日历 cron + agent 自主排期
go run ./cmd/agent -config examples/config/autonomous.yaml,examples/config/cron-daemon.yaml

# 无 Key 冒烟
go run ./cmd/agent -config examples/config/no-api-smoke.yaml "列出目录并读 README"
```

`presets/*.yaml` 与这里的示例等价或互补；生产环境可直接复用 `presets/`，示例侧重可读性与按需复制。

## task 两种模式：prompt 与 script

`platform/worker` 的每个 task **只能二选一**：

```yaml
tasks:
  # prompt 模式：发给 agent 的文本任务（也支持裸字符串简写）
  - prompt: "启动巡检：看看工作区状态，有问题记下来处理，没问题直接 finish。"
  - id: weekday-morning
    cron: "0 9 * * 1-5"
    prompt: "工作日早班巡检：跑测试、看未提交改动，汇总后 finish。"
  # script 模式：直接执行工作区内的 bash 脚本，不经过 agent
  - id: nightly
    cron: "0 3 * * *"
    script: examples/config/scripts/nightly.sh
```

使用 `script` 时需在 `platform.default.deps` 挂 `workspace` 与 `shell`，见 [cron-daemon.yaml](cron-daemon.yaml)。
