# Copilot 认证文件配额查询 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在管理员配额页面接入 GitHub Copilot 配额查询，支持“本地缓存展示 + 手动任务刷新 + 首次无 quota 历史账号可查询”，并在 `premium_interactions` 展示“高级请求”与“剩余/总量”。

**Architecture:** 后端复用现有 quota poller 与手动刷新任务机制，只新增 `github-copilot` provider 刷新分支和手动筛选补集逻辑；前端继续使用 Quotas 卡片，在 `extractQuotaItems` 增加 Copilot 快照解析分支。全流程遵循 TDD：先写失败测试，再最小实现，再回归。

**Tech Stack:** Go (Gin/GORM), SQLite in-memory tests, React + TypeScript + Vitest, i18next locales.

---

### Task 1: 后端测试先行（Copilot 刷新与首次覆盖）

**Files:**
- Modify: `internal/quota/poller_manual_refresh_test.go`
- Modify: `internal/http/api/admin/handlers/quotas_manual_refresh_test.go`

**Step 1: 写失败测试（Poller Copilot 成功落库）**

在 `poller_manual_refresh_test.go` 新增用例（先失败）：

```go
func TestRefreshAuthCopilotSavesQuotaSnapshots(t *testing.T) {
    // 构造 github-copilot auth，metadata 含 access_token
    // mock executor HttpRequest 返回 200 + quota_snapshots JSON
    // 调用 refreshAuth
    // 断言 quota 表写入 type=github-copilot 且 data 包含 premium_interactions
}
```

**Step 2: 运行单测确认失败**

Run: `go test ./internal/quota -run TestRefreshAuthCopilotSavesQuotaSnapshots -v`  
Expected: FAIL（提示 provider 未支持或无数据写入）。

**Step 3: 写失败测试（首次无 quota 仍可入任务）**

在 `quotas_manual_refresh_test.go` 新增用例（先失败）：

```go
func TestListManualRefreshAuthKeysIncludesCopilotWithoutQuotaRow(t *testing.T) {
    // 仅创建 auths 记录：type=github-copilot, is_available=true
    // 不创建 quota 行
    // 调用 listManualRefreshAuthKeys(type=github-copilot)
    // 断言返回包含该 key
}
```

**Step 4: 运行单测确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestListManualRefreshAuthKeysIncludesCopilotWithoutQuotaRow -v`  
Expected: FAIL（当前只查 quota JOIN，拿不到首次账号）。

**Step 5: Commit（测试骨架）**

```bash
git add internal/quota/poller_manual_refresh_test.go internal/http/api/admin/handlers/quotas_manual_refresh_test.go
git commit -m "test(quota): add failing tests for copilot refresh and first-time manual selection"
```

---

### Task 2: 实现 Copilot provider 刷新（最小实现）

**Files:**
- Modify: `internal/quota/poller.go`
- Test: `internal/quota/poller_manual_refresh_test.go`

**Step 1: 最小实现 provider 分发**

在 `poller.go` 两处加 `github-copilot`：

```go
// poll() provider allow-list
if provider != "antigravity" && provider != "codex" && provider != "gemini-cli" && provider != "github-copilot" {
    continue
}

// refreshAuth() switch
case "github-copilot":
    errRefresh = p.pollCopilot(ctx, auth, row)
```

**Step 2: 添加 `pollCopilot` 实现**

新增函数（最小可用）：

```go
const copilotUserEndpoint = "https://api.github.com/copilot_internal/user"

func (p *Poller) pollCopilot(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
    token := resolveCopilotAccessToken(auth.Metadata)
    if token == "" {
        return errors.New("quota poller: copilot missing access token")
    }

    headers := http.Header{}
    headers.Set("Accept", "application/json")
    headers.Set("Authorization", "Bearer "+token)
    headers.Set("User-Agent", "GitHubCopilotChat/0.38.2")

    status, payload, errReq := p.doRequest(ctx, auth, http.MethodGet, copilotUserEndpoint, nil, headers)
    if errReq != nil { return errReq }
    if status < 200 || status >= 300 {
        return &providerRequestError{provider: "github-copilot", statusCode: status, err: fmt.Errorf("quota poller: copilot non-2xx status=%d", status)}
    }
    return p.saveQuota(ctx, row.ID, row.Type, payload)
}
```

并补一个 helper：

```go
func resolveCopilotAccessToken(metadata map[string]any) string {
    if metadata == nil { return "" }
    if v := normalizeString(metadata["access_token"]); v != "" { return v }
    if m := mapFromAny(metadata["metadata"]); m != nil {
        return normalizeString(m["access_token"])
    }
    return ""
}
```

**Step 3: 运行 Task 1 + Task 2 相关测试**

Run: `go test ./internal/quota -run TestRefreshAuthCopilotSavesQuotaSnapshots -v`  
Expected: PASS

Run: `go test ./internal/quota -run TestRefreshAuth.* -v`  
Expected: PASS（现有 token health 相关用例不回归）。

**Step 4: Commit（Copilot 刷新实现）**

```bash
git add internal/quota/poller.go internal/quota/poller_manual_refresh_test.go
git commit -m "feat(quota): add github-copilot poller refresh support"
```

---

### Task 3: 实现手动刷新筛选补集（首次 Copilot 可查询）

**Files:**
- Modify: `internal/http/api/admin/handlers/quotas_manual_refresh.go`
- Test: `internal/http/api/admin/handlers/quotas_manual_refresh_test.go`

**Step 1: 最小实现补集查询 + 去重**

在 `listManualRefreshAuthKeys` 中保留原有 `quota JOIN auths` 查询，然后新增 Copilot 补集查询并去重：

```go
// 1) 先取 quota JOIN auths keys（原逻辑）
// 2) 再查 auths keys where is_available=true and content.type='github-copilot'
// 3) 若 req.Type 非空且 != github-copilot，则跳过补集
// 4) 合并去重后按 key 排序返回
```

建议使用现有 `dbutil.JSONExtractTextExpr(h.db, "auths.content", "type")` 保持多数据库兼容。

**Step 2: 运行相关测试**

Run: `go test ./internal/http/api/admin/handlers -run TestListManualRefreshAuthKeysIncludesCopilotWithoutQuotaRow -v`  
Expected: PASS

Run: `go test ./internal/http/api/admin/handlers -run TestManualRefresh -v`  
Expected: PASS（旧逻辑不回归）。

**Step 3: Commit（手动筛选补集）**

```bash
git add internal/http/api/admin/handlers/quotas_manual_refresh.go internal/http/api/admin/handlers/quotas_manual_refresh_test.go
git commit -m "feat(admin): include first-time copilot auths in manual quota refresh"
```

---

### Task 4: 前端 Copilot 展示与“高级请求”数量

**Files:**
- Modify: `web/src/pages/admin/Quotas.tsx`
- Modify: `web/src/locales/zh-CN.ts`
- Modify: `web/src/locales/en.ts`
- Create: `web/src/pages/admin/quotasCopilotDisplay.test.ts`

**Step 1: 写失败测试（先定义输出）**

新增测试覆盖：

```ts
it('maps copilot premium_interactions to 高级请求 with remaining/entitlement', () => {
  // 输入 quota_snapshots JSON
  // 断言输出 items 含 name='高级请求'、percent=77.06、amount='231 / 300'
})
```

> 需要先从 `Quotas.tsx` 导出一个可测试 helper，例如 `extractCopilotItemsForTest`。

**Step 2: 运行前端单测确认失败**

Run: `npm run test -- quotasCopilotDisplay.test.ts`（workdir: `web`）  
Expected: FAIL（helper 不存在或解析逻辑未实现）。

**Step 3: 最小实现 Copilot 解析与文案**

在 `Quotas.tsx`：

```ts
type QuotaItem = {
  name: string;
  percent: number | null;
  percentDisplay?: string;
  updatedAt: string | null;
  amountDisplay?: string; // 新增：用于“231 / 300”
};

// extractCopilotItems(payload, locale, t)
// - 解析 quota_snapshots
// - premium_interactions -> t('Premium Interactions') (zh 为“高级请求”)
// - amountDisplay = `${remaining} / ${entitlement}`
```

渲染层在现有 item 区域增加一行（仅当 `amountDisplay` 存在时显示）。

在 locale 文件新增：

```ts
"Premium Interactions": "高级请求"
```

**Step 4: 跑前端测试**

Run: `npm run test -- quotasCopilotDisplay.test.ts quotasAuthDisplay.test.ts`（workdir: `web`）  
Expected: PASS

**Step 5: Commit（前端展示）**

```bash
git add web/src/pages/admin/Quotas.tsx web/src/locales/zh-CN.ts web/src/locales/en.ts web/src/pages/admin/quotasCopilotDisplay.test.ts
git commit -m "feat(web): render copilot quota snapshots and premium interactions amount"
```

---

### Task 5: 全量验证与收尾

**Files:**
- Modify: `docs/plans/2026-03-09-copilot-auth-file-quota-query-implementation-plan.md`（仅在验证命令需补充时）

**Step 1: 运行后端关键回归**

Run: `go test ./internal/quota ./internal/http/api/admin/handlers -v`  
Expected: PASS

**Step 2: 运行前端关键回归**

Run: `npm run test`（workdir: `web`）  
Expected: PASS

**Step 3: 运行构建检查**

Run: `go test ./...`  
Expected: PASS

Run: `npm run build`（workdir: `web`）  
Expected: PASS

**Step 4: 最终提交（若前序任务未分批提交）**

```bash
git add -A
git commit -m "feat: support copilot auth-file quota query in admin quota workflow"
```

**Step 5: 请求评审**

- 使用 `@superpowers:requesting-code-review` 进行提交前自检。
- 合并前使用 `@superpowers:verification-before-completion` 复核测试证据。
