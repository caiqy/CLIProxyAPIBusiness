# Billing Rules 防重复与重启自动导入设计

## 1. 背景

当前系统存在两条相关链路：

1. `models`（ModelReference）由 `modelreference.Syncer` 自动从 `https://models.dev/api.json` 同步；
2. 实际计费读取 `billing_rules`，其更新来源主要是管理端 CRUD 与 `batch-import`。

现状下：

- `models` 同步是幂等的（`OnConflict` 更新 + 过期清理）；
- `billing_rules` 的 `BatchImport` 采用“先查后更/建”流程，在并发情况下存在重复插入竞态；
- `billing_rules` 缺少数据库唯一约束，无法在库层兜底阻断重复。

目标是在不改变计费语义的前提下，解决重复风险，并实现“重启后自动把最新参考价同步到默认组计费规则”。

---

## 2. 目标与非目标

### 2.1 目标

1. `billing_rules` 在数据库层具备唯一性约束，避免重复行；
2. `BatchImport` 改为真正幂等的 Upsert；
3. 应用重启后自动执行一次默认组导入，使计费规则可随参考价更新。

### 2.2 非目标

1. 不修改 `usage` 计费公式；
2. 不自动覆盖所有 group（仅默认 auth/user group）；
3. 不改变管理员手工配置/编辑规则接口语义。

---

## 3. 方案选择

### 方案 A（采用）

- 唯一约束：`(auth_group_id, user_group_id, provider, model)`；
- `BatchImport`：按唯一键 `ON CONFLICT DO UPDATE`；
- 启动自动导入：仅默认组执行一次 per-token 导入。

**选择理由：**

- 能同时覆盖“并发重复”与“重启自动更新”需求；
- 范围可控，不会无差别覆盖所有业务组策略。

---

## 4. 详细设计

### 4.1 数据层幂等

在迁移中新增唯一索引（或唯一约束）：

`billing_rules(auth_group_id, user_group_id, provider, model)`。

同时保留现有查询优化索引（例如 `idx_billing_rules_match`）。

> 注意：若历史已存在重复键数据，需在创建唯一索引前先去重（保留最新 `updated_at`，再按最大 `id` 兜底）。

### 4.2 导入层幂等

将 `BatchImport` 的循环逻辑从“先查后写”改为 Upsert：

- 键：`auth_group_id + user_group_id + provider + model`；
- 冲突更新字段：`billing_type`、各价格字段、`is_enabled`、`updated_at`；
- 新建时写入 `created_at/updated_at`。

并在导入前统一规范化：

- `provider = strings.ToLower(strings.TrimSpace(provider))`
- `model = strings.TrimSpace(model)`

避免空白/大小写差异导致逻辑重复。

### 4.3 重启自动导入

在应用启动阶段新增一次性后台任务：

1. 启动后等待 `models` 参考数据可用（短轮询 + 总超时）；
2. 解析默认 `auth_group_id` 与默认 `user_group_id`；
3. 对默认组执行一次与 `batch-import` 等价的导入（`BillingTypePerToken`）。

失败策略：

- 记录 warning/error，不阻塞主服务启动；
- 默认组缺失时跳过并记录日志。

---

## 5. 验收标准

1. 连续多次导入同一批映射，不新增重复行；
2. 并发导入同一批映射，不产生重复行；
3. 重启后（模型参考已同步）默认组规则可自动创建/更新；
4. 管理员手工 CRUD 与现有计费计算行为不回归。

---

## 6. 风险与应对

### 风险 1：历史重复数据阻塞唯一索引创建

**应对：**迁移先执行去重 SQL，再建唯一索引。

### 风险 2：自动导入覆盖默认组人工价格

**应对：**当前需求明确“重启自动更新”，采用覆盖策略；如后续需要可增加配置开关。

### 风险 3：启动阶段模型参考尚未就绪

**应对：**短轮询等待，超时仅记录日志，不阻塞启动。
