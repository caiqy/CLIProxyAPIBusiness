# AuthFiles Provider 缺口补齐设计（Kimi / GitHub Copilot / Kilo / iFlow）

## 背景

当前管理员平台 AuthFiles 板块中：

- 新增认证（`New`）已支持 `codex/anthropic/antigravity/gemini-cli/kiro/iflow-cookie/qwen`；
- 但后端 management 层已具备更多能力（`iflow`、`kimi`、`github-copilot`、`kilo`）；
- 导入认证（Provider Import）尚未覆盖 `kimi/github-copilot/kilo`。

这会导致“后端可用、前端不可见”的能力断层。

## 目标

1. 补齐 AuthFiles 新增认证 provider：`iflow`、`kimi`、`github-copilot`、`kilo`。
2. 补齐 AuthFiles 导入认证 provider：`kimi`、`github-copilot`、`kilo`。
3. 新增导入 provider 必须提供**专有可用字段示例**（可复制、可填充到文本导入）。
4. 不引入“仅占位不可用”的 provider，保持“可见即能用”。

## 非目标

1. 本期不把 `vertex/aistudio/openai-compatibility` 纳入 AuthFiles 新增认证（它们更偏 API key/配置流）。
2. 本期不改动 AuthFiles 主体布局与核心流程（仅做增量扩展）。

## 方案范围（确认版）

### 新增认证（New）补齐

- `iflow`（OAuth）
- `kimi`（Device Flow）
- `github-copilot`（Device Flow）
- `kilo`（Device Flow）

并保留既有：`iflow-cookie`、`kiro` 等。

### 导入认证（Provider Import）补齐

- `kimi`
- `github-copilot`
- `kilo`

## 架构与改造点

### 1) 后端 admin 路由与权限

在 `internal/http/api/admin/admin.go` 追加：

- `POST /v0/admin/tokens/kimi`
- `POST /v0/admin/tokens/github-copilot`
- `POST /v0/admin/tokens/kilo`

在 `internal/http/api/admin/permissions/permissions.go` 追加对应权限定义。

### 2) SDK 暴露层补齐

`third_party/CLIProxyAPIPlus/sdk/api/management.go` 的 `ManagementTokenRequester` 新增：

- `RequestGitHubToken(*gin.Context)`
- `RequestKiloToken(*gin.Context)`

并实现转发到 management handler。

### 3) 导入解析规则补齐

`internal/http/api/admin/handlers/auth_files_provider_import.go`：

- `providerAliasToCanonical` 增加 `github-copilot`、`kilo`、`kimi`；
- `providerImportRules` 增加三者校验规则；
- 保持现有策略：
  - `type` 自动生成；
  - `key` 自动生成（provider+email 优先，无 email 回退哈希）；
  - 忽略冗余字段。

### 4) 前端 AuthFiles 扩展

`web/src/pages/admin/AuthFiles.tsx`：

- `AUTH_TYPES` 增加 `iflow`、`kimi`、`github-copilot`、`kilo`（按权限控制显示）；
- 对 `github-copilot`、`kilo` 复用设备码展示区（`verification_url + user_code`）与轮询逻辑；
- `kimi` 复用 URL + state + 轮询路径。

## 导入示例策略（新增需求）

对新增导入 provider 提供**专有可用字段示例**：

1. `kimi`：示例体现 `access_token`（可选附 `refresh_token`）。
2. `github-copilot`：示例体现 `access_token`（可选 `username/email`）。
3. `kilo`：示例体现 `access_token`（可选 `organization_id`）。

要求：

- 示例来源于 `providerImportTemplates.ts` 单一映射；
- 每个示例可一键复制与填充到“文本导入”；
- 示例不要求填写 `key/type`（系统自动生成）。

## 数据流

1. 用户在 New 里点击新增 provider。
2. 前端请求 `/v0/admin/tokens/{provider}` 获取 `state`（部分携带 `url/user_code`）。
3. 前端轮询 `/v0/admin/tokens/get-auth-status?state=...`。
4. 成功后刷新 auth files 并走统一收口。

导入流：

1. 选择 provider（`kimi/github-copilot/kilo`）→ 查看专有示例。
2. 文件或文本输入 → 统一提交 `import-by-provider`。
3. 后端规范化校验并返回 `imported + failed[]`。

## 错误处理

1. 启动失败：显示 provider 启动错误，保持弹窗可重试。
2. 轮询失败：显示 `error` 并停止轮询。
3. 设备码字段不完整：提示外部授权并继续轮询。
4. 导入部分失败：展示逐条失败，不阻断成功条目。

## 测试计划

### 后端

1. 路由/权限测试：新增三条 token endpoint。
2. 导入规则测试：`kimi/github-copilot/kilo` 最小字段校验。
3. 自动 key/type 与冗余字段忽略回归。

### 前端

1. `AUTH_TYPES` 新增 provider 可见性（权限驱动）。
2. Copilot/Kilo 设备码卡片渲染与轮询流程。
3. Provider Import 示例包含新增三项且可复制/填充。

### 联调验收

1. New 可发起 `iflow/kimi/github-copilot/kilo`。
2. 导入可处理 `kimi/github-copilot/kilo`。
3. 现有 provider 流程无回归。

## 验收标准

1. AuthFiles 新增认证入口补齐上述四项。
2. 导入入口补齐 `kimi/github-copilot/kilo`。
3. 新增导入 provider 具备专有可用字段示例（可复制、可填充）。
4. 无“仅展示不可用”的新增 provider。
