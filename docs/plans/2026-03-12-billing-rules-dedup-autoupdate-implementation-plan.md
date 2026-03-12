# Billing Rules 防重复与重启自动导入 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 `billing_rules` 增加库层防重复与导入幂等能力，并在服务重启后自动将默认组计费规则同步到最新模型参考价格。

**Architecture:** 通过迁移新增 `billing_rules(auth_group_id,user_group_id,provider,model)` 唯一索引，并在迁移前执行重复数据清理。将批量导入逻辑抽离为可复用的 billing importer，统一做 provider/model 规范化并使用 Upsert。应用启动后新增一次性后台任务，等待 `models` 参考数据可用后，对默认 auth/user group 自动执行一次 per-token 导入。

**Tech Stack:** Go, Gorm, SQLite/PostgreSQL migration SQL, Gin handlers, go test

---

### Task 1: 先用测试锁定“billing_rules 不允许重复键”

**Files:**
- Modify: `internal/db/migrate_test.go`
- Test: `internal/db/migrate_test.go`

**Step 1: 写失败测试（迁移后重复键应被拒绝）**

在 `internal/db/migrate_test.go` 新增测试（示例）：

```go
func TestMigrateSQLiteBillingRulesUniqueKey(t *testing.T) {
    conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if errOpen != nil {
        t.Fatalf("open sqlite: %v", errOpen)
    }
    if errMigrate := Migrate(conn); errMigrate != nil {
        t.Fatalf("migrate: %v", errMigrate)
    }

    if err := conn.Exec(`
        INSERT INTO billing_rules (auth_group_id, user_group_id, provider, model, billing_type, is_enabled, created_at, updated_at)
        VALUES (1, 1, 'openai', 'gpt-5.3-codex', 2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    `).Error; err != nil {
        t.Fatalf("insert first row: %v", err)
    }

    errDup := conn.Exec(`
        INSERT INTO billing_rules (auth_group_id, user_group_id, provider, model, billing_type, is_enabled, created_at, updated_at)
        VALUES (1, 1, 'openai', 'gpt-5.3-codex', 2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    `).Error
    if errDup == nil {
        t.Fatal("expected duplicate billing rule insert to fail")
    }
}
```

**Step 2: 运行测试，确认当前失败**

Run: `go test ./internal/db -run TestMigrateSQLiteBillingRulesUniqueKey -v`

Expected: FAIL（当前缺少唯一约束，重复插入不会报错）。

**Step 3: Commit（测试先行）**

```bash
git add internal/db/migrate_test.go
git commit -m "test(db): require unique billing rule key"
```

---

### Task 2: 迁移实现去重 + 唯一索引

**Files:**
- Modify: `internal/db/migrate.go`
- Test: `internal/db/migrate_test.go`

**Step 1: 在迁移中补去重 SQL（SQLite + PostgreSQL）**

在 `migrateSQLite` / `migratePostgres` 的索引创建前，新增重复清理语句：

```sql
-- 逻辑：同键保留 updated_at 最新；若并列保留 id 最大
DELETE FROM billing_rules
WHERE id IN (
  SELECT id FROM (
    SELECT id,
           ROW_NUMBER() OVER (
             PARTITION BY auth_group_id, user_group_id, provider, model
             ORDER BY updated_at DESC, id DESC
           ) AS rn
    FROM billing_rules
  ) t
  WHERE t.rn > 1
);
```

**Step 2: 新增唯一索引**

在 `internal/db/migrate.go` 的索引列表中补充：

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_rules_unique_key
ON billing_rules (auth_group_id, user_group_id, provider, model)
```

**Step 3: 运行 Task 1 测试，确认转绿**

Run: `go test ./internal/db -run TestMigrateSQLiteBillingRulesUniqueKey -v`

Expected: PASS。

**Step 4: 运行 db 包回归测试**

Run: `go test ./internal/db -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/db/migrate.go internal/db/migrate_test.go
git commit -m "feat(db): deduplicate and enforce unique billing rule key"
```

---

### Task 3: 抽离可复用 importer，使用 Upsert + 规范化

**Files:**
- Create: `internal/billing/importer.go`
- Create: `internal/billing/importer_test.go`
- Modify: `internal/billing/billing_rules.go`（如需复用默认组解析函数）
- Test: `internal/billing/importer_test.go`

**Step 1: 写失败测试（大小写 provider 不应生成重复）**

新增测试用例：

```go
func TestImportFromModelMappings_NormalizesProviderAndUpserts(t *testing.T) {
    // seed: model_mappings(provider="OpenAI", new_model_name="gpt-5.3-codex")
    // seed: existing billing_rule(provider="openai", model="gpt-5.3-codex")
    // act: ImportFromModelMappings(...)
    // assert: rows count == 1, row provider == "openai", price fields updated
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/billing -run TestImportFromModelMappings_NormalizesProviderAndUpserts -v`

Expected: FAIL（当前尚无 importer 或仍可能走大小写不一致路径）。

**Step 3: 写最小实现（Upsert + 规范化）**

在 `internal/billing/importer.go` 增加：

```go
type ImportResult struct { Created int; Updated int }

func ImportFromModelMappings(ctx context.Context, db *gorm.DB, authGroupID, userGroupID uint64, billingType models.BillingType) (ImportResult, error)
```

核心实现：

```go
provider := strings.ToLower(strings.TrimSpace(mapping.Provider))
model := strings.TrimSpace(mapping.NewModelName)

// 使用 clause.OnConflict 做 upsert
tx.Clauses(clause.OnConflict{
  Columns: []clause.Column{{Name:"auth_group_id"}, {Name:"user_group_id"}, {Name:"provider"}, {Name:"model"}},
  DoUpdates: clause.AssignmentColumns([]string{
    "billing_type",
    "price_per_request",
    "price_input_token",
    "price_output_token",
    "price_cache_create_token",
    "price_cache_read_token",
    "is_enabled",
    "updated_at",
  }),
}).Create(&rule)
```

**Step 4: 运行 billing 包测试**

Run: `go test ./internal/billing -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/billing/importer.go internal/billing/importer_test.go internal/billing/billing_rules.go
git commit -m "feat(billing): upsert import rules from model mappings"
```

---

### Task 4: Handler 改用 importer（保持 API 语义）

**Files:**
- Modify: `internal/http/api/admin/handlers/billing_rules.go`
- Create: `internal/http/api/admin/handlers/billing_rules_batch_import_test.go`
- Test: `internal/http/api/admin/handlers/billing_rules_batch_import_test.go`

**Step 1: 写失败测试（重复调用 batch-import 结果幂等）**

新增测试：

```go
func TestBillingRulesBatchImport_IdempotentAcrossRepeatedCalls(t *testing.T) {
    // 准备默认分组 + model_mappings + model_reference
    // 连续调用 handler.BatchImport 两次
    // 断言 billing_rules 对应 provider/model 仅 1 条
}
```

**Step 2: 运行测试，确认失败或红灯**

Run: `go test ./internal/http/api/admin/handlers -run TestBillingRulesBatchImport_IdempotentAcrossRepeatedCalls -v`

Expected: FAIL（旧逻辑未复用统一 importer，且未保证规范化路径）。

**Step 3: 以最小改动替换 BatchImport 主体**

在 `BatchImport` 中改为调用：

```go
result, err := billing.ImportFromModelMappings(ctx, h.db, body.AuthGroupID, body.UserGroupID, billingType)
if err != nil { ... }
c.JSON(http.StatusOK, gin.H{"created": result.Created, "updated": result.Updated})
```

**Step 4: 运行 handlers 回归测试**

Run: `go test ./internal/http/api/admin/handlers -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/billing_rules.go internal/http/api/admin/handlers/billing_rules_batch_import_test.go
git commit -m "refactor(admin): use billing importer for batch import"
```

---

### Task 5: 启动后自动导入默认组（一次性）

**Files:**
- Create: `internal/billing/startup_import.go`
- Create: `internal/billing/startup_import_test.go`
- Modify: `internal/app/app.go`
- Test: `internal/billing/startup_import_test.go`

**Step 1: 写失败测试（models 就绪后自动导入默认组）**

新增测试：

```go
func TestAutoImportDefaultGroupOnce_ImportsWhenModelReferencesReady(t *testing.T) {
    // seed: default auth_group + default user_group + enabled model_mappings + model_reference
    // act: AutoImportDefaultGroupOnce(ctx, db, pollInterval, timeout)
    // assert: billing_rules 有对应记录且价格来源于 model_reference
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/billing -run TestAutoImportDefaultGroupOnce_ImportsWhenModelReferencesReady -v`

Expected: FAIL（函数未实现）。

**Step 3: 实现一次性自动导入函数**

在 `internal/billing/startup_import.go` 增加：

```go
func AutoImportDefaultGroupOnce(ctx context.Context, db *gorm.DB, waitTimeout, pollInterval time.Duration) error
```

行为：

1. 等待 `models` 表有记录（超时返回可观测错误）；
2. 调用 `ResolveDefaultAuthGroupID/ResolveDefaultUserGroupID`；
3. 调用 `ImportFromModelMappings(..., models.BillingTypePerToken)`。

**Step 4: 在服务启动接入后台调用**

在 `internal/app/app.go` 的 `modelSyncer.Start(ctx)` 后追加：

```go
go func() {
    if err := billing.AutoImportDefaultGroupOnce(ctx, conn, 60*time.Second, 2*time.Second); err != nil {
        log.WithError(err).Warn("billing rules auto import on startup failed")
    }
}()
```

**Step 5: 运行目标测试与核心回归**

Run:

```bash
go test ./internal/billing -v
go test ./internal/http/api/admin/handlers -v
go test ./internal/db -v
```

Expected: PASS。

**Step 6: Commit**

```bash
git add internal/billing/startup_import.go internal/billing/startup_import_test.go internal/app/app.go
git commit -m "feat(startup): auto import default billing rules after model sync"
```

---

## 验证清单（完成前必须执行）

1. `go test ./internal/db -v`
2. `go test ./internal/billing -v`
3. `go test ./internal/http/api/admin/handlers -v`
4. 人工检查：重启后默认组 `billing_rules` 对应 codex 模型价格已更新，且无重复键记录。

---

## 实施技能约束

- 实施时按 `@superpowers:executing-plans` 逐任务执行。
- 每个任务结束前执行 `@superpowers:verification-before-completion`。
- 若测试失败，先按 `@superpowers:systematic-debugging` 定位根因再修复。
