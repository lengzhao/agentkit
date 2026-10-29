# Lark / 飞书私聊话题模式

私聊默认平铺。用户可以在与机器人的私聊中发送：

- `/dm-thread on`：开启话题模式，此后的每条主界面消息开启一个新话题。
- `/dm-thread off`：恢复平铺新消息；已有话题内可以继续对话。
- `/dm-thread status`：查看当前模式。

在话题详情内发送消息即可继续该话题的上下文；在主界面发送消息则开始另一个话题。文字、文件和卡片共用话题回复路由，审批卡片携带原会话标识。用户设置按平台实例、应用和用户隔离，保存在 workspace 的 `preferences/dm-thread/` 目录中，重启后保留。更改应用或实例标识后使用新的偏好设置。

## 配置独立上下文

**话题展示和 agent 上下文隔离是两项配置。** runner 默认 `channel` 会将同一 DM 的话题合并成一个上下文。要进行多会话交互，需要把 runner 和平台的会话范围都设置成 `thread`：

```yaml
runner.default:
  use: runner
  config:
    sessionScope: thread

platform.default:
  use: platform/lark # 国内版使用 platform/feishu
  config:
    appId: cli_REPLACE_ME
    appSecret: REPLACE_ME
    runnerSessionScope: thread # 平台命令 /new、/stop 等与 runner 保持一致；优先于 sessionScope
    dmThreadMode: false # 可选，默认 false；true 为所有未设置偏好的用户默认开启
```

以上为配置片段，需合并到现有配置，保留 deps。`thread` 范围也作用于同一 runner 的其他渠道；群聊若需隔离话题，仍需单独开启 `threadIsolation`。

`dmThreadMode` 和用户偏好独立于群聊的 `replyInThread` / `threadIsolation`。DM 话题必须使用回复 API，因此话题内的出站消息会忽略 `replyToTrigger: false`；已有话题在默认模式关闭后仍按话题路由。`disablePlatformSlash: true` 时不处理 `/dm-thread` 命令。

纯附件消息使用现有附件暂存逻辑。在话题中上传附件后，继续在同一话题发送提示文字。主界面每条新消息在话题模式下都是独立话题，主界面先发附件再发文字不会合并成同一会话。

## 验收

自动化测试覆盖默认兼容、独立话题路由、用户偏好保存/失败、实例隔离、回复请求及卡片恢复。真实 Lark/飞书机器人还需验证：开启后分别创建两个话题，在各话题追问确认历史隔离，测试进度卡片、文件和审批回复，并确认关闭后新消息平铺、旧话题可继续。
