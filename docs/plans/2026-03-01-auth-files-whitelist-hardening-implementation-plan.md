# Auth Files Whitelist Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 修复 Auth Files 白名单在 provider 判定、Import 覆盖一致性、前端竞态与 i18n 映射上的关键质量问题，确保行为稳定可回归。

**Architecture:** 以后端“能力收口 helper”作为单一真相源，统一服务 Create/Update/ListModelPresets/Import 覆盖路径；前端引入“仅最新请求生效”的异步控制防止状态串写；接口层新增 `reason_code` 并保留 `reason` 兼容，形成渐进式升级。

**Tech Stack:** Go (Gin/GORM), TypeScript + React, Vitest, npm, SQLite test DB

---

### Task 1: 后端 provider 能力判定收口与 reason_code 输出

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Test: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: Write the failing test**

在 `auth_files_whitelist_test.go` 新增/扩展用例：
- `TestAuthFiles_ModelPresets_SupportedProviderMatrix`
- `TestAuthFiles_ModelPresets_ReturnsReasonCode`

断言要点：
- `type=kiro`（或 `qwen`）返回 `supported=true`（stub universe 非空）；
- 不支持/缺参场景返回稳定 `reason_code`（而不仅是 reason 文案）。

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/api/admin/handlers -run "TestAuthFiles_ModelPresets_SupportedProviderMatrix|TestAuthFiles_ModelPresets_ReturnsReasonCode" -v`

Expected: FAIL（当前实现对部分 provider 误判 unsupported，且未输出 `reason_code`）。

**Step 3: Write minimal implementation**

在 `auth_files.go` 中实现：
- 统一 provider 判定函数（不依赖 `normalizeProvider`）；
- `ListModelPresets` 响应新增 `reason_code`，保留 `reason`。

最小代码方向：

```go
func normalizeAuthFileProvider(value string) string {
    return strings.ToLower(strings.TrimSpace(value))
}

func supportsAuthFileWhitelistProvider(provider string) bool {
    switch normalizeAuthFileProvider(provider) {
    case "gemini", "codex", "claude", "antigravity", "qwen", "kiro", "kimi", "github-copilot", "kilo", "iflow":
        return true
    default:
        return false
    }
}
```

并在 `ListModelPresets` 分支中补充 `reason_code`（例如 `AUTH_TYPE_REQUIRED` / `UNSUPPORTED_AUTH_TYPE` / `WHITELIST_UNSUPPORTED_PROVIDER` / `MODELS_UNAVAILABLE`）。

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/api/admin/handlers -run "TestAuthFiles_ModelPresets_SupportedProviderMatrix|TestAuthFiles_ModelPresets_ReturnsReasonCode" -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "fix: unify auth-file whitelist provider capability and reason codes"
```

### Task 2: Import 覆盖路径白名单一致性（自动重算并兜底）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Test: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: Write the failing test**

新增用例：
- `TestAuthFiles_ImportConflict_RecomputeWhitelist_WithIntersection`
- `TestAuthFiles_ImportConflict_RecomputeWhitelist_FallbackDisableWhenEmptyIntersection`
- `TestAuthFiles_ImportConflict_RecomputeWhitelist_FallbackDisableWhenUniverseUnavailable`

断言：
- 有交集 -> whitelist 保持开启，allowed/excluded 重算；
- 无交集或 universe 不可用 -> whitelist 自动关闭且 allowed/excluded 清空。

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/api/admin/handlers -run "TestAuthFiles_ImportConflict_RecomputeWhitelist_" -v`

Expected: FAIL（当前 OnConflict 仅更新 content/proxy/group/updated_at）。

**Step 3: Write minimal implementation**

在 Import 循环中：
- 对同 key 先查旧记录（仅需 whitelist/allowed/content 等字段）；
- 用新 content 解析 provider + universe；
- 计算新 allowed/excluded 或兜底关闭；
- 将 `whitelist_enabled/allowed_models/excluded_models` 纳入 `DoUpdates`。

建议新增小函数：

```go
func reconcileWhitelistOnImportConflict(old models.Auth, newContent map[string]any) (enabled bool, allowed datatypes.JSON, excluded datatypes.JSON, err error)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/api/admin/handlers -run "TestAuthFiles_ImportConflict_RecomputeWhitelist_" -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "fix: keep auth-file whitelist consistent on import conflicts"
```

### Task 3: 前端编辑弹窗竞态修复 + reason_code 本地化映射

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`
- Test: `web/src/pages/admin/authFilesWhitelistConfig.test.ts`

**Step 1: Write the failing test**

在 `authFilesWhitelistConfig.test.ts` 新增 helper 测试（必要时提取纯函数）：
- 仅最新请求响应可应用（序号比较）；
- reason_code 映射优先，reason 兜底；
- authType 为空时 loading 复位。

**Step 2: Run test to verify it fails**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts`

Expected: FAIL（当前无竞态门控与 reason_code 映射）。

**Step 3: Write minimal implementation**

在 `AuthFiles.tsx`：
- 添加 `editPresetRequestSeqRef`（或同等机制）并在请求前递增；
- 响应时仅在 `seq === latestSeq` 时 setState；
- 空 authType 分支与关闭弹窗路径显式关闭 loading；
- 新增 `reason_code` 到 UI 解释映射，文案补充到中英文 locales。

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts`

Expected: PASS

**Step 5: Commit**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesWhitelistConfig.test.ts web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "fix: guard auth-file whitelist preset loading against stale responses"
```

### Task 4: 全量回归与发布前验证

**Files:**
- Verify only: `internal/http/api/admin/handlers/*`, `internal/http/api/admin/permissions/*`, `internal/watcher/*`, `web/*`

**Step 1: Run backend affected tests**

Run: `go test ./internal/db ./internal/http/api/admin/handlers ./internal/http/api/admin/permissions ./internal/watcher -v`

Expected: PASS

**Step 2: Run frontend tests and build**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts apiKeysProviderConfig.test.ts && npm --prefix web run build`

Expected: PASS

**Step 3: Run quick git review**

Run: `git status --short && git diff --stat`

Expected: 仅包含预期变更文件。

**Step 4: Final commit (if batching)**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_whitelist_test.go web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesWhitelistConfig.test.ts web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "fix: harden auth-file whitelist consistency and preset loading"
```
