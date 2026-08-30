# Presets

L1 overlay，叠加在 L0 `config.base.yaml` 之上。`-config` 接受逗号分隔的多个文件，按顺序合并，后面的覆盖前面的。

```sh
# 单文件
go run ./cmd/agent -config presets/coding.yaml "你的任务"

# 链式合并（自主运行 + headless worker）
go run ./cmd/agent -config presets/autonomous.yaml,presets/worker.yaml "一次性任务"
```

## 场景索引

| 文件 | 场景 | 说明 |
|---|---|---|
| [coding.yaml](coding.yaml) | 项目目录 coding | `scope: local`，session 落在 `.agentkit/sessions` |
| [coding-smoke.yaml](coding-smoke.yaml) | 无 API Key 冒烟 | scripted LLM，验证装配与工具链 |
| [autonomous.yaml](autonomous.yaml) | 自主运行 | 预算、todo/finish、auto-allow + policy 白名单 |
| [autonomous-smoke.yaml](autonomous-smoke.yaml) | 自主运行冒烟 | scripted LLM，验证 turn-continue / todo / finish |
| [worker.yaml](worker.yaml) | headless 一次性 | 不读 stdin，适合 CI / 系统 cron；**需链 autonomous** |
| [daemon.yaml](daemon.yaml) | 固定间隔守护 | `platform/timer`，每 N 秒巡检；**需链 autonomous** |
| [cron.yaml](cron.yaml) | cron 守护 | `schedule/cron` + `tool/schedule`；worker 只做启动 task |
| [subagent-smoke.yaml](subagent-smoke.yaml) | 子 agent 冒烟 | scripted LLM；L0 已默认挂载 subagent |
| [web.yaml](web.yaml) | 网络能力 | 搜索 + 抓取 + 向用户提问 |
| [web-smoke.yaml](web-smoke.yaml) | 网络能力冒烟 | scripted web；**需链 web** |
| [openapi-smoke.yaml](openapi-smoke.yaml) | OpenAPI 动态工具冒烟 | scripted LLM + fixture api.json；**真实 HTTP 见 testing/openapitest mock** |
| [multi-tenant.yaml](multi-tenant.yaml) | 多租户 IM | 按群分目录；tool 在 `work/` 子目录；放开并发；**入站 platform 需自行接入** |
| [p1-context.yaml](p1-context.yaml) | P1 能力片段 | 复制进自定义 overlay，非完整能力栈 |

常见 override 片段（换模型、自定义 prompt、排期路径、`once: true`）见根目录 [config.example.yaml](../config.example.yaml)。

## 常用命令

```sh
export OPENAI_API_KEY=sk-...

# 交互式 REPL + 项目目录
go run ./cmd/agent -config presets/coding.yaml

# 自主运行（stdin 给首条任务，跑完退出）
go run ./cmd/agent -config presets/autonomous.yaml "整理 docs 并收尾"

# headless 批处理（stdin 是 /dev/null 也安全）
go run ./cmd/agent -config presets/autonomous.yaml,presets/worker.yaml "跑测试并汇报"

# 常驻：固定间隔
go run ./cmd/agent -config presets/autonomous.yaml,presets/daemon.yaml

# 常驻：日历 cron + agent 自主排期
go run ./cmd/agent -config presets/autonomous.yaml,presets/cron.yaml

# 无 Key 冒烟
go run ./cmd/agent -config presets/coding-smoke.yaml "列出目录并读 README"
```

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
    script: presets/scripts/nightly.sh
```

使用 `script` 时需在 `platform.default.deps` 挂 `workspace` 与 `shell`，见 [cron.yaml](cron.yaml)。
