# 查询配额（批量手动刷新）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在配额页新增“查询配额”按钮，按当前筛选条件异步批量触发上游额度查询，展示进度并在完成后自动刷新列表。

**Architecture:** 后端新增“创建任务 + 查询状态”两个管理接口，使用内存任务表追踪运行态；任务执行阶段按筛选条件定位 auth 集合，复用 quota provider 查询逻辑逐条更新数据库。前端新增按钮与轮询状态展示，任务结束后自动调用现有 `fetchQuotas()`。

**Tech Stack:** Go (Gin + GORM + SQLite/Postgres 兼容查询)、React + TypeScript + i18next。

---

> 说明：你已确认本次不使用 worktree，本计划默认在当前分支执行。

### Task 1: 增加权限定义（先写失败测试）

**Files:**
- Create: `internal/http/api/admin/permissions/quota_manual_refresh_permissions_test.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`

**Step 1: 写失败测试（权限 key 必须存在）**

```go
package permissions

import (
    "net/http"
    "testing"
)

func TestDefinitionsIncludeQuotaManualRefresh(t *testing.T) {
    defs := DefinitionMap()

    if _, ok := defs[Key(http.MethodPost, "/v0/admin/quotas/manual-refresh")]; !ok {
        t.Fatalf("missing permission for manual refresh create")
    }
    if _, ok := defs[Key(http.MethodGet, "/v0/admin/quotas/manual-refresh/:task_id")]; !ok {
        t.Fatalf("missing permission for manual refresh status")
    }
}
```

**Step 2: 运行测试并确认失败**

Run: `go test ./internal/http/api/admin/permissions -run TestDefinitionsIncludeQuotaManualRefresh -v`

Expected: FAIL，提示缺少新权限定义。

**Step 3: 只做最小实现让测试通过**

在 `permissions.go` 的 `definitions` 中增加：

```go
newDefinition("POST", "/v0/admin/quotas/manual-refresh", "Manual Refresh Quotas", "Quota"),
newDefinition("GET", "/v0/admin/quotas/manual-refresh/:task_id", "Get Manual Refresh Task", "Quota"),
```

**Step 4: 再跑测试确认通过**

Run: `go test ./internal/http/api/admin/permissions -run TestDefinitionsIncludeQuotaManualRefresh -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/quota_manual_refresh_permissions_test.go
git commit -m "feat(admin): add permissions for quota manual refresh tasks"
```

---

### Task 2: 实现内存任务表（状态机、计数、TTL）

**Files:**
- Create: `internal/http/api/admin/handlers/quota_manual_refresh_task_store.go`
- Create: `internal/http/api/admin/handlers/quota_manual_refresh_task_store_test.go`

**Step 1: 写失败测试（任务生命周期）**

```go
func TestTaskStoreLifecycle(t *testing.T) {
    store := newQuotaManualRefreshTaskStore(30*time.Minute, 200)
    task := store.Create("admin-1", quotaManualRefreshFilter{Key: "abc"}, 3)

    if task.Status != quotaManualRefreshStatusRunning {
        t.Fatalf("expected running")
    }

    store.RecordResult(task.ID, quotaManualRefreshResultSuccess, "")
    store.RecordResult(task.ID, quotaManualRefreshResultFailed, "timeout")
    store.RecordResult(task.ID, quotaManualRefreshResultSkipped, "unsupported")
    store.Finish(task.ID)

    got, ok := store.Get(task.ID)
    if !ok || got.Processed != 3 || got.SuccessCount != 1 || got.FailedCount != 1 || got.SkippedCount != 1 {
        t.Fatalf("unexpected counters: %+v", got)
    }
}
```

**Step 2: 运行测试并确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TaskStoreLifecycle -v`

Expected: FAIL（类型/函数不存在）。

**Step 3: 实现最小任务表**

关键结构建议：

```go
type quotaManualRefreshStatus string

const (
    quotaManualRefreshStatusRunning quotaManualRefreshStatus = "running"
    quotaManualRefreshStatusSuccess quotaManualRefreshStatus = "success"
    quotaManualRefreshStatusFailed  quotaManualRefreshStatus = "failed"
)

type quotaManualRefreshTask struct {
    ID          string    `json:"task_id"`
    Status      quotaManualRefreshStatus `json:"status"`
    Total       int       `json:"total"`
    Processed   int       `json:"processed"`
    SuccessCount int      `json:"success_count"`
    FailedCount  int      `json:"failed_count"`
    SkippedCount int      `json:"skipped_count"`
    LastError   string    `json:"last_error"`
    RecentErrors []string `json:"recent_errors"`
    StartedAt   time.Time `json:"started_at"`
    FinishedAt  *time.Time `json:"finished_at"`
}
```

任务表需要：`Create` / `Get` / `RecordResult` / `Finish` / `CleanupExpired`。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TaskStoreLifecycle -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/quota_manual_refresh_task_store.go internal/http/api/admin/handlers/quota_manual_refresh_task_store_test.go
git commit -m "feat(admin): add in-memory store for quota manual refresh tasks"
```

---

### Task 3: 抽取单 auth 刷新能力（供定时轮询与手动任务复用）

**Files:**
- Modify: `internal/quota/poller.go`
- Create: `internal/quota/poller_manual_refresh_test.go`

**Step 1: 写失败测试（不支持 provider 返回明确错误）**

```go
func TestRefreshAuthUnsupportedProvider(t *testing.T) {
    p := &Poller{}
    auth := &coreauth.Auth{ID: "k1", Provider: "unknown-provider"}
    row := authRowInfo{ID: 1, Type: "unknown-provider"}

    err := p.refreshAuth(context.Background(), auth, row)
    if !errors.Is(err, ErrUnsupportedProvider) {
        t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
    }
}
```

**Step 2: 运行测试并确认失败**

Run: `go test ./internal/quota -run TestRefreshAuthUnsupportedProvider -v`

Expected: FAIL。

**Step 3: 实现最小共享逻辑**

在 `poller.go` 增加：

```go
var ErrUnsupportedProvider = errors.New("quota poller: unsupported provider")

func (p *Poller) refreshAuth(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
    provider := strings.ToLower(strings.TrimSpace(auth.Provider))
    if provider == "" {
        provider = strings.ToLower(strings.TrimSpace(row.Type))
    }
    switch provider {
    case "antigravity":
        p.pollAntigravity(ctx, auth, row)
        return nil
    case "codex":
        p.pollCodex(ctx, auth, row)
        return nil
    case "gemini-cli":
        p.pollGeminiCLI(ctx, auth, row)
        return nil
    default:
        return ErrUnsupportedProvider
    }
}
```

并让现有 `poll()` 循环改为调用 `refreshAuth(...)`。

另增导出方法：

```go
func (p *Poller) RefreshByAuthKey(ctx context.Context, authKey string) error
```

流程：`manager.List()` 找到 auth、`loadAuthRows()` 找到 row、调用 `refreshAuth`。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/quota -run TestRefreshAuthUnsupportedProvider -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/quota/poller.go internal/quota/poller_manual_refresh_test.go
git commit -m "refactor(quota): expose single-auth refresh for manual tasks"
```

---

### Task 4: 新增手动刷新接口（POST 创建任务 / GET 查询状态）

**Files:**
- Modify: `internal/http/api/admin/handlers/quotas.go`
- Create: `internal/http/api/admin/handlers/quotas_manual_refresh.go`
- Create: `internal/http/api/admin/handlers/quotas_manual_refresh_test.go`
- Modify: `internal/http/api/admin/admin.go`

**Step 1: 写失败测试（创建任务 + 查询状态）**

```go
func TestQuotaManualRefreshCreateAndGetStatus(t *testing.T) {
    gin.SetMode(gin.TestMode)
    db := setupQuotaHandlerTestDB(t)
    seedQuotaRows(t, db)

    h := NewQuotaHandler(db, nil)
    h.manualRefresher = &fakeManualRefresher{delay: 5 * time.Millisecond}

    // POST /manual-refresh
    // 断言 200 + task_id + status=running
    // GET /manual-refresh/:task_id
    // 轮询直到 status=success
}
```

**Step 2: 运行测试并确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestQuotaManualRefreshCreateAndGetStatus -v`

Expected: FAIL（接口不存在/结构缺失）。

**Step 3: 实现最小 API 与异步执行**

`QuotaHandler` 扩展依赖：

```go
type quotaManualRefresher interface {
    RefreshByAuthKey(ctx context.Context, authKey string) error
}

type QuotaHandler struct {
    db             *gorm.DB
    manualRefresher quotaManualRefresher
    taskStore      *quotaManualRefreshTaskStore
}
```

`POST /manual-refresh`：
- 解析 body：`key/type/auth_group_id`
- 基于 `quota JOIN auths` + 过滤条件查 `DISTINCT auths.key`
- `taskStore.Create(...)`
- goroutine 执行批量刷新（并发复用 `QUOTA_POLL_MAX_CONCURRENCY`，可从 settings 读取）
- 返回 `{task_id,status,total}`

`GET /manual-refresh/:task_id`：
- 查任务，不存在返回 404
- 返回任务快照 JSON

在 `admin.go` 注册：

```go
authed.POST("/quotas/manual-refresh", quotaHandler.CreateManualRefreshTask)
authed.GET("/quotas/manual-refresh/:task_id", quotaHandler.GetManualRefreshTask)
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestQuotaManualRefreshCreateAndGetStatus -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/quotas.go internal/http/api/admin/handlers/quotas_manual_refresh.go internal/http/api/admin/handlers/quotas_manual_refresh_test.go internal/http/api/admin/admin.go
git commit -m "feat(admin): add async quota manual refresh task endpoints"
```

---

### Task 5: 前端接入“查询配额”按钮、进度展示、自动刷新

**Files:**
- Modify: `web/src/pages/admin/Quotas.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 先写最小前端状态结构（会导致 lint/build 暂时失败）**

```ts
interface ManualRefreshTaskStatus {
  task_id: string;
  status: 'running' | 'success' | 'failed';
  total: number;
  processed: number;
  success_count: number;
  failed_count: number;
  skipped_count: number;
  last_error?: string;
}
```

**Step 2: 完成按钮与轮询逻辑实现**

关键逻辑：

```ts
const canManualRefresh = hasPermission(buildAdminPermissionKey('POST', '/v0/admin/quotas/manual-refresh'));
const canGetTask = hasPermission(buildAdminPermissionKey('GET', '/v0/admin/quotas/manual-refresh/:task_id'));

const triggerManualRefresh = async () => {
  const res = await apiFetchAdmin<{ task_id: string; status: string; total: number }>(
    '/v0/admin/quotas/manual-refresh',
    { method: 'POST', body: JSON.stringify({ key: search.trim() || undefined, type: typeFilter || undefined, auth_group_id: authGroupFilter || undefined }) }
  );
  startPolling(res.task_id);
};
```

轮询完成条件：`status !== 'running'`，完成后 `fetchQuotas()`。

按钮文案使用：`t('Query Quota')`（中文翻译为“查询配额”）。

**Step 3: 增加 i18n 词条**

`en.ts`：

```ts
"Query Quota": "Query Quota",
"Quota query in progress": "Quota query in progress",
"Quota query completed": "Quota query completed",
```

`zh-CN.ts`：

```ts
"Query Quota": "查询配额",
"Quota query in progress": "正在查询配额",
"Quota query completed": "配额查询完成",
```

**Step 4: 运行前端校验**

Run: `npm run lint`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 5: 提交**

```bash
git add web/src/pages/admin/Quotas.tsx web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "feat(web): add query quota button with async progress polling"
```

---

### Task 6: 全量回归验证与收尾

**Files:**
- Modify (if needed): `docs/plans/2026-02-18-quota-bulk-manual-refresh-design.md`

**Step 1: 跑后端相关测试集**

Run: `go test ./internal/http/api/admin/permissions ./internal/quota ./internal/http/api/admin/handlers -v`

Expected: PASS。

**Step 2: 跑仓库关键验证**

Run: `go test ./...`

Expected: PASS（或仅历史已知失败，需单独记录）。

**Step 3: 复核需求清单（逐条打勾）**

- [ ] 按钮名称为“查询配额”
- [ ] 范围为当前筛选结果
- [ ] 异步 + 进度可见
- [ ] 不支持类型计失败但不中断
- [ ] 任务完成后自动刷新列表
- [ ] 新增独立权限点
- [ ] 并发复用 `QUOTA_POLL_MAX_CONCURRENCY`

**Step 4: 最终提交（若 Task 1~5 未分提交则在此补齐）**

```bash
git add -A
git commit -m "feat(admin): support async manual quota query for filtered records"
```

**Step 5: 产出变更说明（PR 描述草稿）**

```md
## Summary
- 新增配额页“查询配额”按钮，支持按当前筛选条件批量异步刷新
- 新增任务状态接口与内存任务表，前端轮询展示进度
- 新增权限点并复用现有并发配置，完成后自动刷新列表
```

---

## 风险与应对

1. **任务执行中服务重启**：内存任务丢失（已接受范围）。
2. **筛选结果过大导致等待长**：通过并发上限控制与进度展示缓解。
3. **上游瞬时波动导致失败率高**：失败计数与错误摘要可观测，支持再次触发。

## 验收口径（实现完成时）

- 管理员在 Quotas 页面可见“查询配额”按钮（有权限时）。
- 点击后 2 秒内看到任务进度更新。
- 任务结束后卡片自动刷新并显示最新数据。
- 不支持 provider 的记录被计入失败，不阻断整批。
