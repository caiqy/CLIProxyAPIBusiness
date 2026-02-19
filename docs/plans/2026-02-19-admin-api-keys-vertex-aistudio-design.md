# Admin API Keys 扩充 Vertex 与 AI Studio 命名澄清设计

## 背景

当前管理端对 provider 能力的分层已经逐步清晰：

- **AuthFiles** 主要承载 OAuth/device-flow/token-file 这一类账号态凭据；
- **Admin / API Keys** 承载 API key 与配置驱动的 provider。

围绕 `vertex / aistudio / openai-compatibility` 的讨论中，已明确结论：

1. 这些 provider 更偏 API key/配置驱动，不应继续塞进 AuthFiles 的 New 流程；
2. 要让能力“可运营、可管理、可落地配置”，应落在管理员 **API Keys** 板块。

基于此，本设计聚焦一次最小可行扩充：在 API Keys 中补齐 Vertex（API Key）管理，并把现有 Gemini 展示文案改为 **AI Studio（Gemini API Key）**，降低概念歧义。

## 目标

1. 在 Admin API Keys 支持 `vertex` 类型的新增、编辑、启用/禁用、删除。
2. 后端以 `provider=vertex` 作为 canonical 值，配置同步落地到 YAML `vertex-api-key`。
3. 前端将 `gemini` 的显示文案调整为「AI Studio（Gemini API Key）」，仅改展示不改语义。
4. 与现有 BillingRules/Models provider 口径保持一致，不新增额外兼容层。

## 非目标

1. 本期不重构 API Keys 的 provider 类型为“后端下发 catalog”。
2. 本期不改 AuthFiles 的 New 能力边界。
3. 本期不引入新的管理接口路径（复用既有 `/v0/admin/provider-api-keys`）。

## 方案选型

### 方案 A：增量扩充 API Keys（采用）

- 在现有 CRUD/校验/配置同步框架上新增 `vertex` 分支；
- 前端 Type 选项补 `vertex`，文案修正 `gemini`；
- 优点：改动面小、风险低、上线快。

### 方案 B：API Keys 类型完全改为后端 catalog（未采用）

- 优点：长期避免前端硬编码漂移；
- 缺点：本期改动较大，超出“先补齐能力”的范围。

## 架构与数据契约

### 接口与权限

复用现有接口与权限定义：

- `POST /v0/admin/provider-api-keys`
- `GET /v0/admin/provider-api-keys`
- `PUT /v0/admin/provider-api-keys/:id`
- `DELETE /v0/admin/provider-api-keys/:id`

不新增路由，不新增权限项。

### provider canonical 规则

- DB/API canonical：`vertex`
- 可选输入别名：`vertex-api-key`（归一化为 `vertex`）

这样可与已有 provider catalog（Models/Billing）及运行时模型查询保持一致，避免 `provider=vertex-api-key` 导致模型列表失配。

### 字段规则（vertex）

`provider=vertex` 时：

- 必填：`api_key`、`base_url`
- 可选：`priority`、`prefix`、`proxy_url`、`headers`、`models`
- 不适用：`api_key_entries`、`excluded_models`

后端在规范化步骤清理不适用字段，防止 UI/DB 残留造成配置语义漂移。

### 配置同步映射

在 `syncSDKConfig()` 中将 `provider=vertex` 的行转换为 `sdkconfig.VertexCompatKey`：

- `APIKey <- api_key`
- `Priority <- priority`
- `Prefix <- prefix`
- `BaseURL <- base_url`
- `ProxyURL <- proxy_url`
- `Headers <- headers`
- `Models <- models`

最终写入 `cfg.VertexCompatAPIKey` 并由 `SaveConfigPreserveComments` 落地到 YAML `vertex-api-key` 段。

## 前端设计（Admin API Keys）

### Type 与文案

在 `ApiKeys.tsx`：

1. 增加 `vertex` 类型选项（建议展示为 `Vertex (API Key)`）；
2. 将 `gemini` 文案改为 **AI Studio（Gemini API Key）**；
3. `getProviderLabel()`、`getProviderStyle()` 同步支持 `vertex`。

### 表单与校验

- 继续沿用“非 openai-compatibility”通用 API key 表单；
- `provider=vertex` 时前端校验 `base_url` 必填；
- `api_key_entries` 区块仍仅对 `openai-compatibility` 显示。

### 跨页面联动预期

由于后端使用 canonical `vertex`，`provider-api-keys?options=1` 返回后，BillingRules 既有“合并 provider 选项”逻辑可直接纳入 vertex；随后调用 `available-models?provider=vertex` 能与现有 model registry 正常对齐。

## 错误处理

保持现有错误语义一致：

- 400：`invalid provider`、`api_key is required`、`base_url is required` 等输入错误；
- 500：DB 写入失败、配置同步失败。

前端沿用统一错误提示机制，新增 vertex 仅增加一个 base_url 前置校验，减少无效请求。

## 测试策略

### 后端

1. `normalizeProvider`：`vertex` 与别名归一化；
2. `validateProviderRow`：vertex 必填校验；
3. `syncSDKConfig`：vertex 记录正确写入 `VertexCompatAPIKey`；
4. CRUD handler 回归：create/list/update/delete 在 vertex 下可用。

### 前端

1. API Keys 类型下拉包含 vertex；
2. gemini 文案显示为 AI Studio（Gemini API Key）；
3. vertex 表单缺少 base_url 时阻止提交并提示；
4. openai-compatibility 专属字段不在 vertex 下显示。

### 联调冒烟

1. UI 新增 vertex key 成功；
2. 配置文件产生/更新 `vertex-api-key` 条目；
3. BillingRules 可选择 `vertex` 并拉取可用模型。

## 上线与回滚

### 上线顺序

1. 先发后端（保证 API 能接收/同步 vertex）；
2. 再发前端（开放 vertex 创建入口与新文案）。

### 回滚策略

- 前端可单独回滚，不影响后端已具备的 vertex 能力；
- 后端回滚时需评估已写入的 `vertex` 记录与配置段读取兼容性。

## 验收标准

1. 管理员可在 API Keys 完整管理 `vertex` 类型凭据；
2. `provider=vertex` 的记录可正确同步到 `vertex-api-key` 配置；
3. API Keys 页面 Gemini 文案统一为 AI Studio（Gemini API Key）；
4. BillingRules 侧可用 `provider=vertex` 正常联动模型加载。
