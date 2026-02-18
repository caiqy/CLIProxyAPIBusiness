# 配额页更新时间与 Token 健康状态展示设计

## 目标

在现有配额能力基础上完成两类增强：

1. 配额页每个账号卡片底部显示“最后更新时间”。
2. 当更新配额发现 token 失效时记录状态并在界面展示，同时影响调用选路：
   - 普通业务调用排除失效 token。
   - 配额定时刷新与手动刷新保留探测能力，用于自动恢复。

## 设计原则

- 人工开关与系统健康状态分离。
- 复用现有定时配额轮询能力，不新建独立探测系统。
- 仅以 401/403 认定 token 失效，避免误判。
- 界面信息集中在卡片底部，减少视觉跳跃。

## 状态模型（核心）

将健康状态存放在 `auths` 表，而不是 `quota` 表：

- `is_available`（已有）：人工启用/禁用。
- `token_invalid`（新增，bool，默认 false）：系统判定的 token 失效状态。
- `last_auth_check_at`（新增，timestamp，nullable）：最近一次健康探测时间。
- `last_auth_error`（新增，text，nullable）：最近一次失效错误原文（完整错误）。

`quota.updated_at` 继续作为“最后更新时间”来源，不复用为健康状态字段。

## 语义边界（必须区分）

### 1) 人工可用性（运营语义）

- 来源：`is_available`
- 含义：管理员是否允许该账号参与系统运行。
- 控制方式：管理员按钮（启用/禁用）。

### 2) Token 健康性（运行语义）

- 来源：`token_invalid`
- 含义：上游鉴权是否已判定失效。
- 控制方式：系统自动写入与自动恢复，不由启用/禁用按钮直接改写。

## 状态迁移规则

### 失效判定

仅当配额更新请求返回 `401/403` 时：

- `token_invalid = true`
- `last_auth_check_at = now`
- `last_auth_error = 完整错误原文`

### 非失效错误

429/5xx/网络超时等：

- 不置 `token_invalid=true`
- 可更新 `last_auth_check_at`（记录探测尝试）
- 不覆盖失效判定语义

### 自动恢复

后续配额探测成功（2xx 且配额写入成功）时：

- `token_invalid = false`
- `last_auth_check_at = now`
- `last_auth_error = NULL`

## 调用排除策略（你确认的规则）

- **普通业务调用**：selector 过滤 `token_invalid=true` 的账号，不参与分配。
- **配额定时刷新 / 手动刷新**：允许继续探测（但仍遵守 `is_available=true` 前置条件）。
- **禁用账号**（`is_available=false`）：不参与定时/手动探测。

## 后端改造点

1. 数据库迁移：为 `auths` 增加 `token_invalid` / `last_auth_check_at` / `last_auth_error`。
2. 配额刷新链路（定时 + 手动）统一写健康状态：
   - 401/403 标记失效
   - 成功探测自动恢复
3. selector 增加健康过滤：普通流量排除 `token_invalid=true`。
4. `GET /v0/admin/quotas` 响应补充字段（来自 `auths` join）：
   - `is_available`
   - `token_invalid`
   - `last_auth_check_at`
   - `last_auth_error`

## 前端交互设计（Quotas 页面）

每张卡片底部增加三块：

1. **启用状态**：启用 / 禁用（来自 `is_available`）。
2. **Token 状态**：正常 / 失效（来自 `token_invalid`）。
3. **最后更新时间**：相对时间显示，hover 展示绝对时间（来自 `quota.updated_at`）。

当 `token_invalid=true` 且 `last_auth_error` 非空时：

- 显示按钮：`查看错误信息`
- 点击弹窗展示：
  - 账号 key
  - 最近检查时间
  - 完整错误原文（不脱敏，按需求）
- 提供“复制错误信息”按钮。

## 接口与数据流

1. 定时刷新或手动刷新触发探测。
2. 探测结果写 `quota`（成功）与 `auth` 健康字段（成功/失效）。
3. 配额页查询通过 join 返回聚合数据。
4. 前端按字段渲染状态与弹窗。

## 错误处理与边界

- `last_auth_error` 存储建议设置长度上限（例如 16KB），超长截断并标记，防止异常膨胀。
- 弹窗允许查看完整原文，但仅管理员可见。
- 并发探测时采用“最后写入生效”即可，不引入版本锁（YAGNI）。

## 测试方案

### 后端

- 401/403 触发 `token_invalid=true`。
- 非 401/403 不触发失效。
- 成功探测自动恢复。
- selector 在普通请求路径排除 `token_invalid=true`。
- `is_available=false` 时不参与探测。
- 配额列表接口返回新增健康字段。

### 前端

- 卡片底部正确展示三类信息。
- 失效时出现“查看错误信息”按钮。
- 弹窗展示完整错误原文与复制功能。
- 恢复后按钮消失、状态回到“正常”。

## 验收标准

1. 每个配额卡片底部显示最后更新时间（相对+绝对）。
2. token 失效可记录并展示。
3. 失效 token 被普通业务调用排除。
4. 定时/手动刷新可继续探测并自动恢复。
5. 启用/禁用与 token 健康状态清晰分离并同时展示。

## 非目标（本期不做）

- 不新增独立健康探测服务。
- 不做按模型粒度的 token 失效状态。
- 不做弹窗内容脱敏策略（按当前确认展示完整原文）。
