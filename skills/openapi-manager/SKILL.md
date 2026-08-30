---
name: openapi-manager
description: OpenAPI 索引与 HTTP 动态工具的维护约定。编辑 api.json 或 api/*.json 前先加载本 Skill。
---

# OpenAPI 维护指南

## 文件布局

- `api.json`：纯索引，顶层 `apis` 按名字列出每个 HTTP API 的 wiring。
- `api/<name>.json`：独立 OpenAPI 3 文档（paths、schema、components）；不含 auth/bind/baseUrl。
- 默认查找顺序：`local:api.json` → `global:api.json`（先命中者赢，按 `apis` 名字去重）。

路径经 workspace 解析，可用 `local:` / `global:` 前缀。

## 推荐工作流

1. 用 `read` / `edit` / `write` 修改 `api.json` 或 `api/*.json`。
2. 请用户执行 **`/openapi -u`** 重读磁盘并刷新动态 HTTP 工具（Agent 工具列表里没有 slash command）。
3. 查看状态时请用户执行 **`/openapi`**（显示已加载 API 与帮助）。
4. 追加单条索引时，用户也可用 **`/openapi add <name> <json>`**（校验、写盘、失败回滚）。
5. 调用 `petstore__getPet` 等动态工具发起 HTTP 请求。

## api.json 索引条目

```json
{
  "apis": {
    "petstore": {
      "path": "api/petstore.json",
      "baseUrl": "https://petstore.example.com",
      "prefix": "petstore__",
      "auth": { "type": "bearer", "token": "env:PETSTORE_TOKEN" },
      "bind": {
        "uid": { "from": "ctx:user_id", "in": "header", "name": "X-User-Id" }
      },
      "allowOperations": ["getPet"],
      "denyOperations": ["deletePet"],
      "timeoutSeconds": 30
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `path` | 指向 OpenAPI 文档（推荐） |
| `specFile` | `path` 的遗留别名 |
| `paths` | **遗留**：索引内联 paths；与 `path` 互斥 |
| `baseUrl` | API 根地址；优先于 OpenAPI 文档里的 `servers` |
| `prefix` | 工具名前缀，默认 `<name>__` |
| `headers` | 每次请求附带的静态 header |
| `auth` | `bearer` / `header` / `query` / `basic`；敏感值用 `env:NAME` |
| `allowOperations` / `denyOperations` | 按 `operationId` 白/黑名单 |
| `timeoutSeconds` | 单次请求超时，默认 30 |
| `bind` | 从 context 注入参数，不暴露给模型 |

## bind 约定

- `from` 必须为 `ctx:` 前缀（如 `ctx:user_id`、`ctx:metadata.org_id`）。
- `in`：`path` / `query` / `header`；`name` 为 HTTP 字段名，省略时用 bind key。
- bind key 与 OpenAPI spec 参数名对应，用于从模型 schema 中隐藏该参数。
- ctx 值为空时不发起 HTTP 请求。

## 动态工具命名

`<prefix><operationId>`，例如 `petstore__getPet`。输入 schema 由 parameters + `body`（requestBody JSON）组成。

## 注意

- 解析结果缓存在内存；改文件后必须 **`/openapi -u`**。
- OpenAPI 文档支持 `$ref` / `components`，由 kin-openapi 解析。
- 详细字段与 slash command 说明见 `docs/guides/tools.zh.md`。
