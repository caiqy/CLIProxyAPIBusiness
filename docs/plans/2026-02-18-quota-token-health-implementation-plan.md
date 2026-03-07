# 配额页更新时间与 Token 健康状态 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让配额页展示账号级最后更新时间与 token 健康状态，并在 token 401/403 失效时自动排除普通业务调用、保留配额刷新探测以自动恢复。

**Architecture:** 在 `auths` 表新增健康字段（`token_invalid`/`last_auth_check_at`/`last_auth_error`），并在 quota poller（定时+手动共用）统一写入健康状态。普通业务选路由 selector 基于健康字段排除失效 token；配额页面通过 `quota JOIN auths` 返回展示字段，前端底部两行状态 + “查看错误信息”弹窗。

**Tech Stack:** Go (Gin/GORM/SQLite/Postgres)、React + TypeScript + i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

### Task 1: 扩展 Auth 模型与数据库字段（健康状态）

**Files:**
- Modify: `internal/models/auth.go`
- Modify: `internal/db/migrate.go`
- Test: `internal/db/migrate_test.go`（若无则新增）

**Step 1: 写失败测试（字段存在并有默认值）**

```go
func TestAuthHasTokenHealthColumns(t *testing.T) {
    // migrate sqlite/postgres test db
    // assert columns: token_invalid, last_auth_check_at, last_auth_error
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/db -run TokenHealthColumns -count=1 -v`

Expected: FAIL（缺少字段）。

**Step 3: 最小实现**

在 `models.Auth` 增加：

```go
TokenInvalid    bool       `gorm:"type:boolean;not null;default:false"`
LastAuthCheckAt *time.Time `gorm:"type:timestamptz"`
LastAuthError   string     `gorm:"type:text"`
```

在 `migrate.go` 为 postgres/sqlite 增加兜底 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`（保证老库升级稳定）。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/db -run TokenHealthColumns -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/models/auth.go internal/db/migrate.go internal/db/migrate_test.go
git commit -m "feat(db): add auth token health columns"
```

---

### Task 2: Poller 统一写 token 健康状态（401/403 失效、成功恢复）

**Files:**
- Modify: `internal/quota/poller.go`
- Modify: `internal/quota/poller_manual_refresh_test.go`

**Step 1: 写失败测试（失效判定与恢复）**

```go
func TestRefreshAuth401MarksTokenInvalid(t *testing.T) {}
func TestRefreshAuthSuccessClearsTokenInvalid(t *testing.T) {}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/quota -run "MarksTokenInvalid|ClearsTokenInvalid" -count=1 -v`

Expected: FAIL。

**Step 3: 最小实现**

1. 增加可判定状态码的错误类型：

```go
type quotaHTTPError struct { StatusCode int; Message string }
```

2. provider 请求非 2xx 时返回 `quotaHTTPError`；
3. 在 `refreshAuth` 末尾统一落库：
   - `401/403` => `token_invalid=true`, `last_auth_check_at=now`, `last_auth_error=full error`;
   - 成功 => `token_invalid=false`, `last_auth_check_at=now`, `last_auth_error=''`;
   - 其它失败 => 仅更新 `last_auth_check_at`（不置失效）。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/quota -run "MarksTokenInvalid|ClearsTokenInvalid" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/quota/poller.go internal/quota/poller_manual_refresh_test.go
git commit -m "feat(quota): persist auth token health on refresh results"
```

---

### Task 3: 普通业务选路排除失效 token（与配额探测分离）

**Files:**
- Modify: `internal/watcher/watcher.go`
- Modify: `internal/auth/selector.go`
- Create: `internal/auth/selector_token_health_test.go`

**Step 1: 写失败测试（selector 排除 token_invalid）**

```go
func TestSelectorSkipsTokenInvalidAuth(t *testing.T) {}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/auth -run TokenInvalid -count=1 -v`

Expected: FAIL。

**Step 3: 最小实现**

1. `watcher` 读取 auth 行时把健康字段注入到 `coreauth.Auth.Metadata`（保留 `is_available=true` 才下发的现状）；
2. `selector` 在 `isAuthBlockedForModel` 或可用集合筛选阶段识别元数据 `_sys_token_invalid=true` 并排除；
3. 不改 poller `RefreshByAuthKey` 的 manager 查询逻辑，使其仍可探测并自动恢复。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/auth -run TokenInvalid -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/watcher/watcher.go internal/auth/selector.go internal/auth/selector_token_health_test.go
git commit -m "feat(auth): exclude token-invalid auths from request selection"
```

---

### Task 4: 配额 API 返回账号状态与错误信息，并限制手动探测仅启用账号

**Files:**
- Modify: `internal/http/api/admin/handlers/quotas.go`
- Modify: `internal/http/api/admin/handlers/quotas_manual_refresh.go`
- Create: `internal/http/api/admin/handlers/quotas_list_test.go`
- Modify: `internal/http/api/admin/handlers/quotas_manual_refresh_test.go`

**Step 1: 写失败测试**

```go
func TestQuotaListIncludesAuthTokenHealthFields(t *testing.T) {}
func TestManualRefreshSkipsDisabledAuths(t *testing.T) {}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run "QuotaListIncludesAuthTokenHealthFields|ManualRefreshSkipsDisabledAuths" -count=1 -v`

Expected: FAIL。

**Step 3: 最小实现**

1. `quotas.go` 查询字段扩展：
   - `auths.is_available`
   - `auths.token_invalid`
   - `auths.last_auth_check_at`
   - `auths.last_auth_error`
2. API 输出新增对应 JSON 字段；
3. `listManualRefreshAuthKeys` 增加 `auths.is_available = true` 条件，符合“禁用不探测”。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run "QuotaListIncludesAuthTokenHealthFields|ManualRefreshSkipsDisabledAuths" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/quotas.go internal/http/api/admin/handlers/quotas_manual_refresh.go internal/http/api/admin/handlers/quotas_list_test.go internal/http/api/admin/handlers/quotas_manual_refresh_test.go
git commit -m "feat(admin): expose token health fields in quota list"
```

---

### Task 5: 前端卡片底部状态 + 查看错误弹窗 + 最后更新时间

**Files:**
- Modify: `web/src/pages/admin/Quotas.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 写最小失败用例（若现有无前端测试框架则以类型/构建失败代替）**

优先若有测试框架：新增组件行为测试；否则先改类型定义让 `tsc` 报错，再补实现。

**Step 2: 实现最小 UI 变更**

1. 扩展 `QuotaRecord` 字段：`is_available`、`token_invalid`、`last_auth_check_at`、`last_auth_error`；
2. 卡片底部新增：
   - 行 1：启用状态（Enabled/Disabled）
   - 行 2：Token 状态（Normal/Invalid）
   - 行 3：最后更新时间（相对时间；`title` 显示绝对时间）
3. `token_invalid && last_auth_error` 时显示按钮 `View Error` / `查看错误信息`；
4. 点击按钮弹窗显示完整错误原文 + 最近检查时间 + 复制按钮。

**Step 3: i18n 词条补齐**

新增 keys（中英文）：
- `Token Status`
- `Token Invalid`
- `Token Normal`
- `Last Updated`
- `View Error`
- `Token Error Details`
- `Copy Error`

**Step 4: 运行前端验证**

Run: `npm run build`（workdir=`web`）

Expected: PASS。

Run: `npm run lint`（workdir=`web`）

Expected: 若失败，确认是否为仓库既有基线问题；本次文件无新增 lint 报错。

**Step 5: Commit**

```bash
git add web/src/pages/admin/Quotas.tsx web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "feat(web): show token health and quota update time on quota cards"
```

---

### Task 6: 全量验证与收口

**Files:**
- Modify (if needed): `docs/plans/2026-02-18-quota-token-health-and-ui-design.md`

**Step 1: 后端回归**

Run: `go test ./internal/db ./internal/quota ./internal/auth ./internal/http/api/admin/handlers -v`

Expected: PASS。

**Step 2: 全仓 Go 回归**

Run: `go test ./...`

Expected: PASS。

**Step 3: 前端回归**

Run: `npm run build`（workdir=`web`）

Expected: PASS。

Run: `npm run lint`（workdir=`web`）

Expected: 仅历史基线问题（如有），无本次新增问题。

**Step 4: 需求核对清单**

- [ ] 配额卡片底部显示账号级最后更新时间
- [ ] token 401/403 会记录失效状态
- [ ] 普通业务调用排除失效 token
- [ ] 配额定时/手动刷新可继续探测并自动恢复
- [ ] 启用/禁用与 token 状态分离展示
- [ ] 失效时有“查看错误信息”按钮与弹窗（完整原文）

**Step 5: 最终提交（若前面未分批提交）**

```bash
git add -A
git commit -m "feat: add token health lifecycle and quota card status UX"
```
