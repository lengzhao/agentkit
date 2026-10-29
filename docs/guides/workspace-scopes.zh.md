# 工作目录作用域与入站附件

`workspace/default` 和 `workspace/tenant` 支持独立的执行目录策略。`scope: local/global` 仍只选择文件存储根；`workspaceScope` 选择一轮任务的默认工作目录，两者互不替代。历史隔离由 runner 的 `sessionScope` 决定。

## 配置

例如普通聊天按 Channel，共用同一个 workspace 服务的 Lark 实例按 Thread：

```yaml
workspace.default:
  use: workspace/tenant
  config:
    global: ~/.agentkit
    localBase: ~/.agentkit/tenants
    scope: local
    workspaceScope: channel
    workspaceScopes:
      lark: thread
```

`workspaceScopes` 的键是平台实例 ID，例如 `lark1`，不是仅协议类型。其优先级高于 `workspaceScope`；空字符串覆盖表示该实例保持旧消费者默认行为。不配置这两个字段时，保留旧工作目录和文件布局。

- `global`：`global` 根下的 `workDir`，跨入口共享。
- `channel`：该入口本地根下的 `workDir/channels/<channel-key>`。
- `thread`：上述 Channel 目录下的 `threads/<thread-key>`；没有 Thread 时记录日志并回退到所属 Channel。
- `session`：上述 Channel 目录下的 `sessions/<session-key>`，使用已经解析的逻辑 Session，而非 delivery ID。

键采用结构化身份的 SHA-256，包含平台实例、原始 Channel，以及相应的 Thread 或逻辑 Session。外部 ID 不直接拼成路径；即使旧租户目录名清洗或 `omitPlatformPrefix` 使父目录重合，显式策略的工作目录仍分开。Global 模式按配置有意共享。

`workDir` 默认为 `work`，附件子目录由 `uploadSubdir` 决定，默认为 `upload`。新策略显式启用后采用新目录布局，不会搬迁旧文件。目录在实际准备轮次时创建；重启后相同配置与身份解析到相同路径。没有 Channel/Workspace 身份的入口不能启用需要该身份的策略；返回可诊断错误，不回退到 Global。

## cwd 覆盖与会话生命周期

执行目录保持既有优先级：Session cwd 绑定优先，其次 Channel cwd 绑定，再使用 workspace 默认目录。委派子执行保留父目录继承规则，显式子 Session 绑定优先。

`session_cwd(path)` 和 `/cwd use <path>` 只修改逻辑 Session 的绑定；`/cwd -c use <path>` 仍是 Channel 绑定。变更从下一轮生效，当前轮次快照不变。清除 Session 绑定后先恢复 Channel 绑定（若存在），然后才是策略默认目录。cwd 不改变身份、会话日志、凭据、技能目录或其他控制数据的根。

`/new` 创建新逻辑 Session，不复制原 Session 的 cwd；已有 Channel 绑定继续适用。Thread 策略下默认目录保持不变；Session 策略下使用新目录。恢复旧 Session 后会恢复相应默认目录或原有绑定。

启用显式策略后，失效的 cwd 绑定会阻止执行和新附件保存，错误提示通过 `/cwd reset` 或 `/cwd -c reset` 恢复。未启用策略时保留既有 warning 和消费者默认目录回退行为。

独立目录不是文件访问沙箱，也不引入模型指令或权限的自动继承。

## 附件与轮次快照

Slack、飞书/Lark 和 Chat API 的新文件与图片在入口下载后暂留于进程内。Runner 完成路由及 active-session 解析、取得会话队列执行权后，调用 `sessctx.Prepare` 固定本轮目录，再保存附件并传入 Loop。图片的 `Source` 与文件提示都使用稳定绝对路径。ACP 和本地 Agent 复用该快照；后续逻辑轮次重新解析 cwd。

带附件的消息在会话忙碌时排队为新轮次，避免 steering 的纯消息接口丢失附件状态。普通文字 steering 行为保持不变。保存失败时报告轮次错误；不会继续执行并假装附件已保存。同名文件使用排他创建并增加序号，不覆盖历史附件。

Chat API 的 `local_file` 上传是受控的前置文件保存：实际 chat 请求将其读入本轮待归档附件，并在取得 cwd 快照后保存一份。原始上传及其 API 元数据保留。`local_path` 引用已有文件，保留原路径；混合新文件与已有引用均会传给模型。

### 文件先发、文字后发

飞书/Lark 的附件缓冲在保存时解析 active session 和 cwd，保存绝对路径及原 Session/Route 归属。后来改变 cwd 不搬动这些文件，也不会把其相对路径解释到新目录。缓冲期间处理 `/new`、`/cwd` 等命令不丢弃附件；后续文字若属于不同 Session/Thread，会报告归属不匹配，要求在目标会话重新发送。

平台与 Runner 必须接入同一个 SessionStore，并保持 `runnerSessionScope` 配置一致；基础配置已经这样装配。旧配置未向平台提供 SessionStore 时，保留兼容的缓冲引用并记录缺少逻辑归属的 warning；启用新策略后这种未知归属会被拒绝，不猜测默认 Session。

缓冲元数据和延迟字节仍是进程内临时状态，重启不恢复未配对的缓冲消息。已归档文件和已写入历史的绝对引用保留；策略配置及已有 Session cwd 绑定通过原有配置与存储恢复。框架不自动清理或删除旧目录。

## 自定义集成

`common.InboundOptsFor` 保持立即保存的旧 API。Runner 集成应使用 `common.DeferredInboundOptsFor`；`MessageEvent.Uploads` 是不参与 JSON 序列化的临时数据。

自定义 Runner 需在路由/会话排队及 cwd 准备后调用 `common.PrepareInboundAttachments`，并把同一上下文传给执行器。自定义中间件若在图片前插入内容，必须同步更新待上传图片的 `ContentIndex`。文件先发的适配器可在解析归属与目录后使用 `common.SaveInboundAttachmentsAt`，后续通过 `PresavedAttachments` 交接。
