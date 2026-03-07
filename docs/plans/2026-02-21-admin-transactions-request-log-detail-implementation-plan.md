# Admin Transactions Request Log Detail Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为管理员“最近交易”增加日志图标入口，支持按交易查看单次上游请求输入/输出原文（成功与失败请求均可查）。

**Architecture:** 通过在 `usages` 持久化 `request_id` 建立交易与 request-log 的强关联。后端新增交易日志详情接口，按 `usage.id -> request_id -> *-{request_id}.log` 读取并返回 Request/Response 原文。前端在交易表格末列增加日志图标按钮并使用弹窗分栏展示，日志缺失时显示错误信息。

**Tech Stack:** Go (Gin + GORM), SQLite/PostgreSQL migration, React + TypeScript + Vitest。

---

### Task 1: 为 usages 增加 request_id 并完成持久化（TDD）

**Files:**
- Modify: `internal/models/usage.go`
- Modify: `internal/db/migrate.go`
- Modify: `internal/usage/usage.go`
- Test: `internal/usage/usage_request_id_test.go` (new)

**Step 1: 写失败测试（usage 会写入 request_id）**

```go
func TestUsageRequestIDCaptured(t *testing.T) {
    gin.SetMode(gin.TestMode)
    conn, _ := db.Open(":memory:")
    _ = db.Migrate(conn)

    rec := httptest.NewRecorder()
    ginCtx, _ := gin.CreateTestContext(rec)
    logging.SetGinRequestID(ginCtx, "req-abc123")

    ctx := context.WithValue(context.Background(), "gin", ginCtx)
    plugin := NewGormUsagePlugin(conn)
    plugin.HandleUsage(ctx, coreusage.Record{Provider: "openai", Model: "gpt-5", RequestedAt: time.Now().UTC()})

    var row struct{ RequestID string `gorm:"column:request_id"` }
    _ = conn.Table("usages").Select("request_id").Order("id DESC").Take(&row)
    if row.RequestID != "req-abc123" { t.Fatalf("want request_id persisted") }
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/usage -run TestUsageRequestIDCaptured -v`

Expected: FAIL（`request_id` 列不存在或返回空值）。

**Step 3: 最小实现让测试通过**

实现点：
1. `models.Usage` 增加 `RequestID string 'gorm:"type:text;index"'`
2. `migrate.go`（Postgres + SQLite）补 `ALTER TABLE usages ADD COLUMN ... request_id`
3. `usage.go` 在落库前从 gin/context 读取 request id 并写入 `row.RequestID`

```go
func extractRequestID(ctx context.Context) string {
    ginCtx, _ := ctx.Value("gin").(*gin.Context)
    if ginCtx == nil { return "" }
    return strings.TrimSpace(logging.GetGinRequestID(ginCtx))
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/usage -run TestUsageRequestIDCaptured -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/models/usage.go internal/db/migrate.go internal/usage/usage.go internal/usage/usage_request_id_test.go
git commit -m "feat(usage): persist request_id for transaction-log linking"
```

### Task 2: 扩展最近交易返回 id/request_id（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/dashboard.go`
- Test: `internal/http/api/admin/handlers/dashboard_transactions_variant_test.go`

**Step 1: 写失败测试（响应包含 id/request_id）**

在现有 `TestAdminDashboardTransactionsReturnsVariantFields` 中扩展断言：

```go
Transactions []struct {
    ID        uint64 `json:"id"`
    RequestID string `json:"request_id"`
    VariantOrigin string `json:"variant_origin"`
    Variant string `json:"variant"`
} `json:"transactions"`
```

并断言：

```go
if resp.Transactions[0].ID == 0 { t.Fatalf("expected id") }
if resp.Transactions[0].RequestID != "req-abc123" { t.Fatalf("expected request_id") }
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionsReturnsVariantFields -v`

Expected: FAIL（JSON 字段缺失或为空）。

**Step 3: 最小实现**

1. `transactionItem` 增加 `ID`、`RequestID`
2. `RecentTransactions` 组装响应时填充 `u.ID`、`strings.TrimSpace(u.RequestID)`

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionsReturnsVariantFields -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/dashboard.go internal/http/api/admin/handlers/dashboard_transactions_variant_test.go
git commit -m "feat(admin): expose transaction id and request_id"
```

### Task 3: 新增交易日志详情接口（成功/失败日志都可读）（TDD）

**Files:**
- Create: `internal/http/api/admin/handlers/dashboard_request_log.go`
- Test: `internal/http/api/admin/handlers/dashboard_request_log_test.go` (new)

**Step 1: 写失败测试**

测试覆盖：
1. `request_id` 存在且日志文件存在 → 200，返回 request/response 原文
2. 匹配 `error-...-{request_id}.log`（失败请求日志）也可返回 200
3. usage 无 `request_id` → 409
4. 找不到文件 → 404

示例测试片段：

```go
resp := httptest.NewRecorder()
c, _ := gin.CreateTestContext(resp)
c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
c.Params = gin.Params{{Key: "id", Value: "1"}}

h.GetTransactionRequestLog(c)
if resp.Code != http.StatusOK { t.Fatalf("want 200") }
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionRequestLog -v`

Expected: FAIL（handler 不存在或返回不符）。

**Step 3: 最小实现**

实现 `GetTransactionRequestLog`：
1. 解析 `:id`，读取 usage（拿 `request_id`）
2. `request_id` 为空返回 409
3. 定位日志目录（与 request logger 一致策略）
4. 匹配后缀 `-{request_id}.log`，多命中取最新修改时间
5. 读取文件并分段提取：`api_request_raw`、`api_response_raw`
6. 返回 JSON：`request_id`、`api_request_raw`、`api_response_raw`、`source_file`

分段提取建议最小规则：

```go
// 从全文中提取以 "=== API REQUEST" 开始到下一个大段标记前的文本
// 从全文中提取以 "=== API RESPONSE" 开始到 "=== RESPONSE ===" 前的文本
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionRequestLog -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/dashboard_request_log.go internal/http/api/admin/handlers/dashboard_request_log_test.go
git commit -m "feat(admin): add transaction request-log detail endpoint"
```

### Task 4: 路由与权限注册（TDD）

**Files:**
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Test: `internal/http/api/admin/permissions/dashboard_transaction_request_log_permissions_test.go` (new)

**Step 1: 写失败测试（权限定义存在）**

```go
func TestDefinitionMapIncludesDashboardTransactionRequestLogPermission(t *testing.T) {
    key := "GET /v0/admin/dashboard/transactions/:id/request-log"
    if _, ok := DefinitionMap()[key]; !ok {
        t.Fatalf("missing permission key %q", key)
    }
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/permissions -run TestDefinitionMapIncludesDashboardTransactionRequestLogPermission -v`

Expected: FAIL。

**Step 3: 最小实现**

1. `admin.go` 注册：

```go
authed.GET("/dashboard/transactions/:id/request-log", dashboardHandler.GetTransactionRequestLog)
```

2. `permissions.go` 增加定义：

```go
newDefinition("GET", "/v0/admin/dashboard/transactions/:id/request-log", "View Transaction Request Log", "Dashboard")
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/permissions -run TestDefinitionMapIncludesDashboardTransactionRequestLogPermission -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/dashboard_transaction_request_log_permissions_test.go
git commit -m "feat(admin): wire dashboard transaction request-log route and permission"
```

### Task 5: 前端新增日志图标与弹窗展示（先写测试）

**Files:**
- Modify: `web/src/components/admin/AdminTransactionsTable.tsx`
- Create: `web/src/components/admin/AdminTransactionsTable.request-log.test.tsx`

**Step 1: 写失败测试（图标列 + 错误提示）**

覆盖点：
1. 每行显示日志图标按钮（无文字）
2. 点击按钮后请求详情接口
3. 接口报错时弹窗显示错误信息

示例片段：

```tsx
expect(screen.getAllByRole('button', { name: /request log/i })).toHaveLength(1)
await user.click(screen.getByRole('button', { name: /request log/i }))
expect(await screen.findByText(/log file not found/i)).toBeInTheDocument()
```

**Step 2: 运行测试确认失败**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: FAIL（按钮/弹窗不存在）。

**Step 3: 最小实现**

1. `Transaction` 类型增加 `id`、`request_id`
2. 表格新增末列图标按钮（例如 `Icon name="description"`）
3. 点击后请求：`/v0/admin/dashboard/transactions/${id}/request-log`
4. 新增弹窗状态：loading / success / error
5. 弹窗分栏展示 `api_request_raw` 与 `api_response_raw`
6. 报错时展示后端返回错误文本

**Step 4: 运行测试确认通过**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: PASS。

**Step 5: 提交**

```bash
git add web/src/components/admin/AdminTransactionsTable.tsx web/src/components/admin/AdminTransactionsTable.request-log.test.tsx
git commit -m "feat(admin-ui): add request-log icon modal for recent transactions"
```

### Task 6: 全量回归验证

**Files:**
- Verify only (no file change required)

**Step 1: 运行后端目标测试**

Run: `go test ./internal/usage ./internal/http/api/admin/handlers ./internal/http/api/admin/permissions -v`

Expected: PASS。

**Step 2: 运行前端目标测试**

Run: `npm --prefix web run test -- AdminTransactionsTable.variant.test.tsx AdminTransactionsTable.request-log.test.tsx`

Expected: PASS。

**Step 3: 运行前端构建检查**

Run: `npm --prefix web run build`

Expected: BUILD SUCCESS。

**Step 4: 手工联调清单**

1. 成功交易：图标可打开并显示 request/response 原文。
2. 失败交易：图标可打开并显示失败请求日志。
3. 清理日志后：图标可点击，弹窗显示“日志不存在”。
4. 历史无 request_id 数据：弹窗显示“该交易无 request_id”。

**Step 5: 提交（如有修复）**

```bash
git add <fixed-files>
git commit -m "fix: address regressions from transaction request-log feature"
```
