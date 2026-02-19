# Thinking Variant Adaptation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现“能力优先 + 向下最近级别降级 + 双字段审计（variant_origin/variant）+ 首页可视化展示（origin => real）”的完整适配链路。

**Architecture:** 在 `third_party/CLIProxyAPIPlus/internal/thinking` 增加“决策元数据”输出（origin/real/decision/reason），并把该元数据通过 executor → usage reporter → usage record 传递到业务库 `usages`。业务后端仅在 dashboard recent transactions 接口透传新字段，前端首页两张交易表做展示拼接，不改 logs 二级页。

**Tech Stack:** Go (gin, gorm, testing), React + TypeScript + Vitest。

---

### Task 1: 先用测试锁定 thinking 行为回归边界

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/test/thinking_conversion_test.go`
- Create: `third_party/CLIProxyAPIPlus/internal/thinking/adaptation_meta_test.go`

**Step 1: 写失败测试（xhigh 不应被无条件降级）**

在 `thinking_conversion_test.go` 增加 case：当模型能力支持 `xhigh` 且 toFormat 为 openai 时，期望输出仍为 `xhigh`。

**Step 2: 运行单测确认失败**

Run: `go test ./test -run TestThinkingE2EMatrix`

Expected: FAIL，失败点包含 `xhigh -> high` 或 `minimal -> low`。

**Step 3: 写元数据失败测试**

在 `adaptation_meta_test.go` 增加测试：
- 无显式参数时 `origin='' real=''`
- 支持时 `origin==real`
- 不支持时 `origin!=real`

**Step 4: 运行新测试确认失败**

Run: `go test ./internal/thinking -run AdaptationMeta`

Expected: FAIL（函数/字段尚不存在）。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/test/thinking_conversion_test.go third_party/CLIProxyAPIPlus/internal/thinking/adaptation_meta_test.go
git commit -m "test(thinking): lock variant preservation and adaptation metadata behavior"
```

### Task 2: 在 thinking 层实现“决策元数据 + 条件降级”

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/thinking/adaptation_meta.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/thinking/apply.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/thinking/provider/openai/apply.go`

**Step 1: 写失败测试（先不实现）**

补充 Task1 中未覆盖的 case：`auto`、`minimal`、unknown model fallback、strict 模式错误路径。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/thinking -run AdaptationMeta`

Expected: FAIL。

**Step 3: 写最小实现（但保持架构完整）**

实现要点：
- 新增 `AdaptationMeta`（`VariantOrigin`, `Variant`, `Decision`, `Reason`）
- 新增 `ApplyThinkingWithMeta(...) ([]byte, AdaptationMeta, error)`
- 保留旧 `ApplyThinking(...)` 作为兼容包装
- `openai` 路径禁止无条件 clamp；仅在“目标模型不支持”时按向下最近级别降级

**Step 4: 运行测试验证通过**

Run: `go test ./internal/thinking -run AdaptationMeta && go test ./test -run TestThinkingE2EMatrix`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/thinking/adaptation_meta.go third_party/CLIProxyAPIPlus/internal/thinking/apply.go third_party/CLIProxyAPIPlus/internal/thinking/provider/openai/apply.go third_party/CLIProxyAPIPlus/internal/thinking/adaptation_meta_test.go third_party/CLIProxyAPIPlus/test/thinking_conversion_test.go
git commit -m "feat(thinking): add capability-aware variant adaptation metadata"
```

### Task 3: 把 variant 元数据接入 usage reporter（子仓库）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/sdk/cliproxy/usage/manager.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/usage_helpers.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/*_executor.go`（所有调用 `thinking.ApplyThinking` 的文件）
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/usage_variant_test.go`

**Step 1: 写失败测试（record 应携带 variant 字段）**

在 `usage_variant_test.go` 验证 publish 时 `usage.Record{VariantOrigin, Variant}` 正确透传。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run UsageVariant`

Expected: FAIL（字段缺失或未赋值）。

**Step 3: 实现透传**

实现要点：
- `usage.Record` 新增 `VariantOrigin`、`Variant`
- `usageReporter` 新增对应字段和 setter（例如 `setVariant(origin, real)`）
- 所有 executor 在 thinking 应用后写入 reporter（优先调用 `ApplyThinkingWithMeta`）

**Step 4: 运行测试验证通过**

Run: `go test ./internal/runtime/executor -run UsageVariant`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/sdk/cliproxy/usage/manager.go third_party/CLIProxyAPIPlus/internal/runtime/executor/usage_helpers.go third_party/CLIProxyAPIPlus/internal/runtime/executor/usage_variant_test.go third_party/CLIProxyAPIPlus/internal/runtime/executor/*_executor.go
git commit -m "feat(usage): propagate thinking variant metadata from executors"
```

### Task 4: 业务库落地 `variant_origin/variant` 字段

**Files:**
- Modify: `internal/models/usage.go`
- Modify: `internal/usage/usage.go`
- Modify: `internal/db/migrate.go`
- Modify: `internal/db/migrate_test.go`

**Step 1: 写失败测试（迁移后字段存在）**

在 `migrate_test.go` 增加断言：`usages` 表存在 `variant_origin`、`variant`。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/db -run Migrate`

Expected: FAIL（列不存在）。

**Step 3: 实现模型与迁移、写入**

实现要点：
- `models.Usage` 增加 `VariantOrigin`、`Variant`
- 迁移用 `HasColumn` 增量添加
- `internal/usage/usage.go` 从 `record` 写入双字段

**Step 4: 运行测试验证通过**

Run: `go test ./internal/db -run Migrate && go test ./internal/usage -run .`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/models/usage.go internal/usage/usage.go internal/db/migrate.go internal/db/migrate_test.go
git commit -m "feat(db): persist variant_origin and variant in usages"
```

### Task 5: 首页 dashboard transactions 接口透传新字段

**Files:**
- Modify: `internal/http/api/front/handlers/dashboard.go`
- Modify: `internal/http/api/admin/handlers/dashboard.go`
- Create: `internal/http/api/front/handlers/dashboard_transactions_variant_test.go`
- Create: `internal/http/api/admin/handlers/dashboard_transactions_variant_test.go`

**Step 1: 写失败测试（接口返回包含新字段）**

为 front/admin 两个 transactions handler 增加用例，断言 JSON 中存在 `variant_origin` 与 `variant`。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/front/handlers -run DashboardTransactionsVariant && go test ./internal/http/api/admin/handlers -run DashboardTransactionsVariant`

Expected: FAIL。

**Step 3: 实现后端返回字段**

实现要点：
- `transactionItem` 新增字段
- 构建 transactions 列表时填充 `u.VariantOrigin`、`u.Variant`
- 仅改 dashboard transactions，不改 logs handlers

**Step 4: 运行测试验证通过**

Run: `go test ./internal/http/api/front/handlers -run DashboardTransactionsVariant && go test ./internal/http/api/admin/handlers -run DashboardTransactionsVariant`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/front/handlers/dashboard.go internal/http/api/front/handlers/dashboard_transactions_variant_test.go internal/http/api/admin/handlers/dashboard.go internal/http/api/admin/handlers/dashboard_transactions_variant_test.go
git commit -m "feat(api): expose variant fields in dashboard transactions"
```

### Task 6: 前端首页两张表新增 variant 展示

**Files:**
- Modify: `web/src/components/TransactionsTable.tsx`
- Modify: `web/src/components/admin/AdminTransactionsTable.tsx`
- Create: `web/src/components/TransactionsTable.variant.test.tsx`
- Create: `web/src/components/admin/AdminTransactionsTable.variant.test.tsx`

**Step 1: 写失败测试（3种展示态）**

用例覆盖：
- `origin='' real=''` 显示 `-`
- `origin==real` 显示单值
- `origin!=real` 显示 `origin => real`

**Step 2: 运行测试确认失败**

Run: `npm --prefix web test -- TransactionsTable.variant AdminTransactionsTable.variant`

Expected: FAIL。

**Step 3: 实现前端展示**

实现要点：
- 类型定义新增 `variant_origin`、`variant`
- 在 Model 文本右侧增加 badge
- 保持 table 列数不变（不新增列，只在模型单元格内展示）

**Step 4: 运行测试验证通过**

Run: `npm --prefix web test -- TransactionsTable.variant AdminTransactionsTable.variant`

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/components/TransactionsTable.tsx web/src/components/admin/AdminTransactionsTable.tsx web/src/components/TransactionsTable.variant.test.tsx web/src/components/admin/AdminTransactionsTable.variant.test.tsx
git commit -m "feat(web): show thinking variant in dashboard recent request tables"
```

### Task 7: 全量回归与交付校验

**Files:**
- Modify: `docs/plans/2026-02-19-thinking-variant-adaptation-design.md`（如需补充实现偏差）

**Step 1: 运行子仓库关键测试**

Run: `go test ./internal/thinking ./internal/runtime/executor ./test`

Expected: PASS。

**Step 2: 运行主仓库后端测试**

Run: `go test ./internal/db ./internal/usage ./internal/http/api/front/handlers ./internal/http/api/admin/handlers`

Expected: PASS。

**Step 3: 运行前端测试与构建**

Run: `npm --prefix web test && npm --prefix web build`

Expected: PASS。

**Step 4: 手工验证首页展示**

检查管理员首页与用户首页 recent 表格：
- 无 thinking 参数显示 `-`
- 不降级显示单值
- 降级显示 `origin => real`

**Step 5: Commit（若有最后修订）**

```bash
git add -A
git commit -m "chore: finalize thinking variant adaptation validation"
```

---

## 执行备注

- 你已明确要求：**不使用 worktree**，本计划默认在当前分支执行。
- 如需保持提交整洁，建议每个 Task 一个提交，避免跨层混改。
