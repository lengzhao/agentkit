# Credentials 密钥服务

AgentKit 在运行期通过 `cap/credentials.Store` 解析 `env:NAME` 引用。加载期 loader **不** 实例化 Store；`${env:VAR}` gate 由 `GraphEnvSource` 处理。详见 [config-simplification.zh.md](config-simplification.zh.md)。

本文档：**收缩版目标**（默认实施范围）、**现状行为**、**迁移路线**、**附录**（仅在有明确需求时再做的演进）。

---

## 1. 收缩版目标（默认方案）

### 1.1 原则

| 原则 | 说明 |
|------|------|
| 对外稳定 | 插件只依赖 `Store.Resolve`；shell 额外可选 `EnvPairResolver`。 |
| 配置优先 | L0/L1 **config.yaml** 提供 **`scopedEnv`**、主密钥；`/env add` 为运行期补充/覆盖。 |
| cap 极简 | `Store` + `EnvPairResolver`；**不**引入 Writer/Catalog/Backend/ProviderChain 等 cap 接口。 |
| 实现内聚 | 链式查找、scoped 存储、密文、manifest 留在 `plugins/credentials`；scope 字符串由各 tool 插件自行拼接；shell 基线 env 留在 `tool/shell`。 |

### 1.2 读路径（语义，不必拆成多个 Go 接口）

**GlobalScope `""`**（`credentials/env`）：context → process env → config.env → encrypted flat → dotenv。

**非空 scope**（`credentials/integrations`）：

1. Context  
2. Scoped 存储 `SCOPE::KEY`（L1 `scopedEnv`、enc、`/env add`；**同键 enc/文件优先于 scopedEnv**）  
3. MCP/OpenAPI：若 JSON 里声明了 `env:` 名且尚无 scoped 值，可 **flat 回退**（实现上由 integrations 读 workspace 默认索引，**不是** L1 配置项）  
4. 否则错误（integration scope 带 `/env add` 提示）

### 1.3 配置供给

| 层级 | 内容 |
|------|------|
| **L0** `config.base.yaml` | `credentials.default` / `credentials.integrations`、`encryptedFile` |
| **L1** `config.yaml` | `config.env`（`AGENTKIT_SECRETS_KEY` 等）、**`scopedEnv`**（gh/npm/MCP 等 token） |
| **运行期** | `/env add` 写 scoped 密文（**同键覆盖**）；写盘后 **reload + verify**；无法解开的条目 **保留密文并设 `abnormal: true`**（`/env` 状态列出），不阻断本次写入 |

**加密库（推荐单文件）**：`credentials.default` 与 `credentials.integrations` 共用 `global:secrets.enc.json`（local 即 `.agentkit/secrets.enc.json`）。Flat 键与 scoped 键（`SCOPE::KEY`）在同一 `entries`。L1 **`scopedEnv`** 启动载入；同键以 enc `/env add` 为准。可选 `config.files`（dotenv）作 dev 补充。

L1 示例（bash 首 token → `shell-bash.<cmd>`；L0 已给 `tool.shell-bash` 挂 `integrations`）：

```yaml
credentials.integrations:
  config:
    env:
      AGENTKIT_SECRETS_KEY: ${var:AGENTKIT_SECRETS_KEY}
    scopedEnv:
      shell-bash.gh:
        GH_TOKEN: ${var:GH_TOKEN}
      shell-bash.npm:
        NPM_TOKEN: ${var:NPM_TOKEN}
      mcp.knowledge:
        KNOWLEDGE_API_KEY: ${var:KNOWLEDGE_API_KEY}
```

MCP/OpenAPI 的 **env 变量名**仍在 `mcp.default` / `openapi.default` 的 `config.files`（如 `global:mcp.json`）里声明；**值**用 `scopedEnv` 或 `/env add`，不必在 `credentials.integrations` 上再配 manifest 路径列表。

### 1.4 cap 契约

```go
type Store interface {
    Resolve(ctx context.Context, scope string, ref string) (Secret, error)
}

type EnvPairsOptions struct {
    Keys []string // 非空时覆盖默认键枚举（tool/shell-bash 一般不必配置）
}

type EnvPairResolver interface {
    EnvPairs(ctx context.Context, scope string, opts EnvPairsOptions) ([]string, error)
}
```

- **`EnvPairResolver`**：`tool/shell-bash` 经 `subprocessEnv` 调用；键顺序：`opts.Keys` → 该 scope **已存储键**（`scopedEnv`/enc/`/env add`）。  
- **子进程基线 env**：有注入时 `tool/shell` 使用裁剪后的宿主 env + pairs；无注入时用全量 `os.Environ()`。

Scope 命名：`mcp.<server>` / `openapi.<api>` / `shell-bash.<cmd>` 由各 tool 插件拼出；`credentials/integrations` 校验并存储 `SCOPE::KEY`。bash 取命令首 token 的 `filepath.Base`。

### 1.5 配置图

| 消费者 | `deps.credentials` |
|--------|-------------------|
| LLM、telemetry、web-search | `credentials.default` |
| tool/mcp、tool/openapi、tool/shell-bash（scoped env） | `credentials.integrations` |

`/env`：integrations 插件 `CommandProvider`；配置图须 **实例化** `credentials.integrations`，由 `commands/registry` 聚合。无参数时列出 **密钥名**（不含值）：loaded/abnormal 密文键、dotenv，以及按 scope 的 env 变量名与 `[manifest|loaded|config|abnormal]` 状态（不展示密文路径与 `config.env` 键名，如 `AGENTKIT_SECRETS_KEY`）。

---

## 2. 现状实现

| 项 | 行为 |
|----|------|
| cap | `Store`、`EnvPairResolver` |
| L1 scoped | **`scopedEnv`** 加载；enc `/env add` 覆盖同键 |
| shell-bash L0 | `workspace` + **`credentials.integrations`**；有 scoped 注入时裁剪 env + token，否则全量 `os.Environ()` |
| MCP/OpenAPI allowlist | 实现内读 workspace `mcp.json` / `api.json`（默认路径），**无** L1 `manifestFiles` 用户面 |
| Global 链顺序 | context → process → config → encrypted → dotenv |

Shell：`subprocessEnv` → `EnvPairs`；`config.commands` 仅少见 override。

操作速查：

```yaml
credentials.default:
  use: credentials/env
  config:
    encryptedFile: global:secrets.enc.json
  deps:
    workspace: workspace.default

credentials.integrations:
  use: credentials/integrations
  config:
    encryptedFile: global:secrets.enc.json
    env:
      AGENTKIT_SECRETS_KEY: ${var:AGENTKIT_SECRETS_KEY}
    scopedEnv:
      shell-bash.gh:
        GH_TOKEN: ${env:GH_TOKEN}
  deps:
    workspace: workspace.default
```

Context override 由 `credentials/env` 与 `credentials/integrations` 在 `Resolve` 内读取，不是 cap 公共 API。

---

## 3. 迁移路线

### Phase 1（低风险）

- [x] cap 注释与链顺序对齐（context → process → config → encrypted → dotenv）
- [x] `tool/mcp`、`tool/openapi`、`tool/shell` 自行拼接 scope，经 `deps.credentials` 注入 Store
- [x] **`EnvPairResolver` + integrations + shell**  

### Phase 2

- [x] L1 **`scopedEnv` 加载**（bash 按 scope 自动注入已配置变量）  
- [x] `/env add` 与 `scopedEnv` 同键优先级（**enc 覆盖 L1 scopedEnv**）  

### Phase 3 及以后

**见附录**；无 Vault/多 Backend 需求前 **不做**。

---

## 4. 附录：可选演进（过度设计预警）

仅在出现明确需求时再考虑：

| 项 | 何时值得做 |
|----|------------|
| `credentials.integrations.config.manifestFiles` / `manifestSources` | **默认不做**；allowlist 路径改由 tool 插件或固定约定即可 |
| `shell-bash.json` 独立 manifest | bash 用 `scopedEnv` + 首 token scope 即可 |
| `SecretProviderChain` 可配置 | 链顺序需按环境差异频繁改 |
| `Backend` + Vault / AWS SM | 有合规或集中密钥管理硬性要求 |
| Composite 单 credentials 节点 | 挂错 deps 成为高频支持问题 |
| Scope 前缀注册表 | 集成种类 > ~5 且频繁新增 |
| ManifestParser `Detect` 注册表 | manifest kind 数量多且第三方扩展 |
| cap `Writer` / `Catalog` | 有第二个 Writer 或第二个 Catalog 消费者 |

业界对照（Keychain / AWS Chain / Vault）用于**沟通概念**；**不要求**逐项落成独立 cap 组件。

---

## 5. 相关文档

- [config-simplification.zh.md](config-simplification.zh.md)  
- [tools.zh.md](tools.zh.md)  
- [plugin-catalog.zh.md](../plugin-catalog.zh.md)  
- [go-agent-harness-architecture.zh.md](../go-agent-harness-architecture.zh.md)  
