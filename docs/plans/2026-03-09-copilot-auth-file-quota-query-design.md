# Copilot 认证文件配额查询设计（管理执行链路）

## 1. 背景与目标

基于现有管理员配额页面能力，新增 GitHub Copilot（`github-copilot`）配额查询与展示能力，且保持当前“按管理执行”模式：

1. 列表默认读取本地 `quota` 缓存。
2. 点击“查询配额”走手动刷新任务（创建任务 -> 轮询状态 -> 回读列表）。
3. 支持首次接入（尚无 `quota` 历史行）的 Copilot 认证文件被纳入查询。

## 2. 约束与输入

### 2.1 已确认约束

- 方案采用“管理执行链路复用”（不新增 Copilot 专用页面/独立查询流程）。
- 需要同时满足“本地读取 + 手动实时刷新”。
- 首次无 `quota` 历史记录的 Copilot 账号也必须可查询。

### 2.2 上游接口（用户提供）

- Method: `GET`
- URL: `https://api.github.com/copilot_internal/user`
- Authorization: `Bearer <github_access_token>`
- 返回体包含：`quota_snapshots.chat/completions/premium_interactions`、`quota_reset_date(_utc)` 等字段。

## 3. 方案对比与结论

### 方案 A（采纳）：复用现有管理链路并扩展 provider

- 在 quota poller 中新增 `github-copilot` 刷新分支。
- 在手动刷新任务筛选中补充“无历史 quota 的 Copilot 认证项”。
- 前端在现有 `Quotas` 页面新增 Copilot payload 解析逻辑。

**优点**：权限模型、交互路径、任务机制全部复用；维护成本最低。  
**成本**：后端 provider 轮询与筛选逻辑需小幅改造，需补测试。

### 方案 B：新增 Copilot 专用实时接口

**缺点**：形成双路径（通用 quota + Copilot 特例），长期复杂度高。  
**结论**：不采纳。

### 方案 C：列表接口直接实时请求上游

**缺点**：列表响应变慢、可用性受上游波动影响、偏离现有任务化刷新模式。  
**结论**：不采纳。

## 4. 架构与数据流设计

### 4.1 查询流（保持现状）

1. 前端调用 `GET /v0/admin/quotas` 获取缓存数据。
2. 点击“查询配额”后调用 `POST /v0/admin/quotas/manual-refresh` 创建任务。
3. 前端轮询 `GET /v0/admin/quotas/manual-refresh/:task_id`。
4. 任务结束后再次调用 `GET /v0/admin/quotas` 展示最新结果。

### 4.2 刷新执行流（新增 Copilot provider）

1. `RefreshByAuthKey` 定位 `auth`。
2. `refreshAuth` 根据 provider 分发到 `pollCopilot`（新增）。
3. `pollCopilot` 调用上游 `/copilot_internal/user`。
4. 成功后 `saveQuota` 落库，统一调用 `updateAuthHealth` 记录健康状态。

## 5. 后端设计

### 5.1 Poller provider 扩展

在 `internal/quota/poller.go` 的 provider switch 中新增：

- `case "github-copilot": errRefresh = p.pollCopilot(ctx, auth, row)`

`pollCopilot` 设计要点：

- 从 auth metadata/content 读取 `access_token`（沿用当前 auth 文件数据结构）。
- 构造 `GET https://api.github.com/copilot_internal/user` 请求。
- 请求头最小集：
  - `Authorization: Bearer <token>`
  - `Accept: application/json`
  - 合理 `User-Agent`（与现有风格一致）
- 2xx: 原样保存响应 JSON 到 `quota.data`，`type=github-copilot`。
- 非 2xx: 返回包含状态码的错误，供健康状态逻辑判定。

### 5.2 手动刷新筛选改造（覆盖首次）

当前逻辑基于 `quota JOIN auths` 取 key，会漏掉首次账号。改造为：

1. 保留原有 `quota JOIN auths` 结果（兼容现有 provider）。
2. 额外查询 `auths` 中满足条件的 Copilot 账号：
   - `is_available = true`
   - `content.type = github-copilot`
   - 叠加同样的筛选条件（`key/type/auth_group_id`）
3. 两批 key 去重合并后进入任务执行。

### 5.3 健康状态语义（复用现有）

- 成功刷新：
  - `token_invalid = false`
  - `last_auth_check_at = now`
  - `last_auth_error = ""`
- 失败刷新：
  - 记录 `last_auth_error`
  - 若状态码为 `401/403`，置 `token_invalid = true`

## 6. 前端展示设计（Quotas 页面）

### 6.1 Copilot 配额解析

在 `web/src/pages/admin/Quotas.tsx` 的 `extractQuotaItems` 增加 Copilot 分支，解析：

- `quota_snapshots.chat`
- `quota_snapshots.completions`
- `quota_snapshots.premium_interactions`

映射规则：

- `name`: 使用 `quota_id`，并对 `premium_interactions` 固定翻译为 **“高级请求”**。
- `percent`: 使用 `percent_remaining`（0~100）。
- `updatedAt`: 优先 `timestamp_utc`，否则回退 `quota.updated_at`。

### 6.2 高级请求数量展示（已确认）

`premium_interactions` 额外显示 **剩余/总量**：

- `remaining / entitlement`（示例：`231 / 300`）

边界：

- 若 `unlimited=true` 且 `entitlement=0`，数量显示 `Unlimited`（避免 `0/0` 误导）。

## 7. 错误处理与边界

1. 上游超时/网络错误：任务项失败，保留已有缓存展示。
2. 上游 401/403：按 token 失效处理，前端可见“Token Invalid”与错误详情。
3. 上游返回 JSON 非法：本次刷新失败，不覆盖旧 `quota.data`。
4. 任务并发与稳定性：沿用现有手动刷新并发上限与任务状态机。

## 8. 测试设计

### 8.1 后端

1. `pollCopilot` 成功写入 `quota`。
2. `pollCopilot` 非 2xx 触发失败并保留状态码语义。
3. 手动刷新在“无 quota 历史”场景覆盖 Copilot key。
4. 健康状态：
   - 401/403 -> `token_invalid=true`
   - 后续成功 -> `token_invalid=false`
5. `key/type/auth_group_id` 筛选在补充 Copilot 路径下仍正确生效。

### 8.2 前端

1. Copilot `quota_snapshots` 可正确渲染三项。
2. `premium_interactions` 文案显示“高级请求”。
3. “高级请求”显示 `remaining/entitlement`。
4. 首次查询后可在卡片列表中出现 Copilot 项。

## 9. 验收标准

1. 管理员无需改变使用习惯，仍通过“查询配额”按钮触发任务化刷新。
2. Copilot 认证文件（包含首次无历史数据）可成功查询并落库。
3. Copilot 卡片展示 `chat/completions/高级请求` 三项。
4. 高级请求展示“剩余/总量”。
5. token 失效与恢复状态在 Quota 页可见且语义正确。

## 10. 非目标

- 不新增独立 Copilot 配额页面。
- 不改变现有 `GET /v0/admin/quotas` 接口形态。
- 不在本期引入额外异步队列/事件系统。
