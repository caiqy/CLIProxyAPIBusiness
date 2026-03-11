# 认证文件代理运行时对齐 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 修复认证文件 `proxy_url` 在管理员面板、数据库、watcher 与运行时 `auth.ProxyURL` 之间的不一致，让认证文件专属代理真正按 `auth.ProxyURL > 全局 proxy-url > 直连` 生效，并对 auth-files 入口统一执行严格代理校验。

**Architecture:** 以 `models.Auth.ProxyURL` 作为认证文件代理的权威来源。watcher 重载 auth 时优先读取数据库列 `proxy_url`，仅在该列为空时兼容回退 `content.proxy_url`。同时复用 `normalizeProxyURL(...)`，对 auth-files 的 create / update / import / provider import 全部收紧为统一的严格校验。

**Tech Stack:** Go, Gin, GORM, sqlite test DB, Go testing (`go test`), cpab watcher/runtime integration.

---

### Task 1: 先写失败测试，锁定“DB 列优先、JSON 回退、非法代理拒绝”行为

**Files:**
- Create: `internal/http/api/admin/handlers/auth_files_proxy_test.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`
- Modify: `internal/watcher/watcher_auth_whitelist_test.go`

**Step 1: 在 watcher_auth_whitelist_test.go 增加 DB 列优先测试**

新增失败测试：

```go
func TestSynthesizeAuthFromDBRow_PrefersDBProxyURLOverMetadata(t *testing.T) {
    now := time.Now().UTC()
    a := synthesizeAuthFromDBRow(
        "",
        "auth-proxy-a",
        []byte(`{"type":"claude","proxy_url":"http://127.0.0.1:7001"}`),
        "http://127.0.0.1:7002",
        0,
        false,
        now,
        now,
        nil,
    )
    if a == nil || a.ProxyURL != "http://127.0.0.1:7002" {
        t.Fatalf("expected db proxy_url to win, got %#v", a)
    }
}
```

**Step 2: 在 watcher_auth_whitelist_test.go 增加 JSON 回退测试**

```go
func TestSynthesizeAuthFromDBRow_FallsBackToMetadataProxyURL(t *testing.T) {
    now := time.Now().UTC()
    a := synthesizeAuthFromDBRow(
        "",
        "auth-proxy-b",
        []byte(`{"type":"claude","proxy_url":"http://127.0.0.1:7001"}`),
        "",
        0,
        false,
        now,
        now,
        nil,
    )
    if a == nil || a.ProxyURL != "http://127.0.0.1:7001" {
        t.Fatalf("expected metadata proxy_url fallback, got %#v", a)
    }
}
```

**Step 3: 创建 auth_files_proxy_test.go，覆盖 create/update 的严格校验与清空语义**

至少包含：

```go
func TestAuthFiles_Create_RejectsUnsupportedProxyScheme(t *testing.T)
func TestAuthFiles_Update_RejectsUnsupportedProxyScheme(t *testing.T)
func TestAuthFiles_Update_EmptyProxyURLClearsProxy(t *testing.T)
```

示例断言：

```go
if w.Code != http.StatusBadRequest {
    t.Fatalf("expected status 400, got %d body=%s", w.Code, w.Body.String())
}
```

**Step 4: 在 provider import 测试中增加 proxy_url 校验失败场景**

在 `auth_files_provider_import_test.go` 新增：

```go
func TestNormalizeProviderEntry_RejectsUnsupportedProxyScheme(t *testing.T) {
    _, err := normalizeProviderEntry("codex", map[string]any{
        "access_token": "token-123",
        "proxy_url":     "ftp://127.0.0.1:21",
    })
    if err == nil {
        t.Fatalf("expected invalid proxy_url error")
    }
}
```

在 `auth_files_provider_import_handler_test.go` 新增 API 级用例，验证非法 `proxy_url` 导入时进入失败列表。

**Step 5: 运行测试并确认失败**

Run: `go test ./internal/watcher -run ProxyURL -v`（workdir: 项目根）  
Expected: FAIL（`synthesizeAuthFromDBRow` 还未支持 DB 列参数/优先级）。

Run: `go test ./internal/http/api/admin/handlers -run "AuthFiles_.*Proxy|NormalizeProviderEntry_RejectsUnsupportedProxyScheme|ImportByProvider" -v`（workdir: 项目根）  
Expected: FAIL（auth-files 入口尚未严格校验）。

**Step 6: Commit（失败测试先行）**

```bash
git add internal/watcher/watcher_auth_whitelist_test.go internal/http/api/admin/handlers/auth_files_proxy_test.go internal/http/api/admin/handlers/auth_files_provider_import_test.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go
git commit -m "test(auth): add failing tests for auth file proxy runtime alignment"
```

---

### Task 2: 收紧 auth-files create / update / import 的 proxy_url 校验

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Test: `internal/http/api/admin/handlers/auth_files_proxy_test.go`
- Test: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: 在 Create 中统一规范化 proxy_url**

把当前直接 `strings.TrimSpace(*body.ProxyURL)` 的逻辑收敛为：

```go
proxyURL := ""
if body.ProxyURL != nil {
    trimmed := strings.TrimSpace(*body.ProxyURL)
    if trimmed != "" {
        normalized, err := normalizeProxyURL(trimmed)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy_url"})
            return
        }
        proxyURL = normalized
    }
}
```

**Step 2: 在 Update 中允许空字符串清空代理，但非空必须合法**

实现语义：

```go
if body.ProxyURL != nil {
    trimmed := strings.TrimSpace(*body.ProxyURL)
    if trimmed == "" {
        updates["proxy_url"] = ""
    } else {
        normalized, err := normalizeProxyURL(trimmed)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy_url"})
            return
        }
        updates["proxy_url"] = normalized
    }
}
```

**Step 3: 在 multipart Import 中对 payload 里的 proxy_url 执行同样校验**

只要 `payload["proxy_url"]` 非空且不合法，就记录该文件失败：

```go
if proxyValue, ok := payload["proxy_url"].(string); ok {
    trimmed := strings.TrimSpace(proxyValue)
    if trimmed != "" {
        normalized, err := normalizeProxyURL(trimmed)
        if err != nil { /* append failure and continue */ }
        proxyURL = normalized
    }
}
```

**Step 4: 运行 auth-files 相关测试**

Run: `go test ./internal/http/api/admin/handlers -run "AuthFiles_.*Proxy|AuthFiles_Create_|AuthFiles_Update_" -v`（workdir: 项目根）  
Expected: PASS

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_proxy_test.go internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "fix(auth): validate auth file proxy urls in create update and import"
```

---

### Task 3: 收紧 provider import 的 proxy_url 校验，保持导入入口一致

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import.go`
- Test: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`
- Test: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`

**Step 1: 在 normalizeProviderEntry 中规范化 proxy_url**

在当前仅 `TrimSpace` 的位置改为：

```go
if proxyURL, ok := normalized["proxy_url"].(string); ok {
    trimmed := strings.TrimSpace(proxyURL)
    if trimmed == "" {
        delete(normalized, "proxy_url")
    } else {
        normalizedProxyURL, err := normalizeProxyURL(trimmed)
        if err != nil {
            return nil, fmt.Errorf("invalid proxy_url")
        }
        normalized["proxy_url"] = normalizedProxyURL
    }
}
```

**Step 2: 保持 ImportByProvider 的入库值与规范化结果一致**

确认 `auth.ProxyURL` 直接来自已规范化后的 `normalized["proxy_url"]`，不再出现宽松路径。

**Step 3: 跑 provider import 测试**

Run: `go test ./internal/http/api/admin/handlers -run "NormalizeProviderEntry|ImportByProvider" -v`（workdir: 项目根）  
Expected: PASS

**Step 4: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files_provider_import.go internal/http/api/admin/handlers/auth_files_provider_import_test.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go
git commit -m "fix(auth): enforce strict proxy validation in provider import"
```

---

### Task 4: 修复 watcher 重载链路，让运行时优先使用 DB 列 proxy_url

**Files:**
- Modify: `internal/watcher/watcher.go`
- Test: `internal/watcher/watcher_auth_whitelist_test.go`
- Test: `internal/watcher/watcher_startup_sync_test.go`

**Step 1: 在 pollAuth 查询中把 proxy_url 列查出来**

把当前查询：

```go
Select("key", "content", "priority", "token_invalid", "created_at", "updated_at", "excluded_models")
```

改为包含：

```go
Select("key", "proxy_url", "content", "priority", "token_invalid", "created_at", "updated_at", "excluded_models")
```

并确保 `rows` 中可取到 `row.ProxyURL`。

**Step 2: 调整 synthesizeAuthFromDBRow 函数签名**

修改为：

```go
func synthesizeAuthFromDBRow(authDir string, key string, payload []byte, dbProxyURL string, priority int, tokenInvalid bool, createdAt, updatedAt time.Time, excludedModels []string) *coreauth.Auth
```

同步修正全部调用点与测试。

**Step 3: 在 synthesizeAuthFromDBRow 中实现“DB 优先，JSON 回退”**

核心逻辑：

```go
proxyURL := strings.TrimSpace(dbProxyURL)
if proxyURL == "" {
    if v, ok := metadata["proxy_url"].(string); ok {
        proxyURL = strings.TrimSpace(v)
    }
}
```

并将该值赋给：

```go
ProxyURL: proxyURL,
```

**Step 4: 增加 watcher 热加载回归断言**

在 `watcher_startup_sync_test.go` 或新测试中，插入一条带 `ProxyURL` 的 `models.Auth`，执行 `pollAuth()` 后确认 `SnapshotAuths()[0].ProxyURL` 与 DB 列一致。

**Step 5: 运行 watcher 测试**

Run: `go test ./internal/watcher -v`（workdir: 项目根）  
Expected: PASS

**Step 6: Commit**

```bash
git add internal/watcher/watcher.go internal/watcher/watcher_auth_whitelist_test.go internal/watcher/watcher_startup_sync_test.go
git commit -m "fix(watcher): load auth proxy url from db with metadata fallback"
```

---

### Task 5: 回归验证 auth 代理优先级与关键链路

**Files:**
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go`
- Test: `internal/http/api/admin/handlers/auth_files_proxy_test.go`
- Test: `internal/watcher/watcher_auth_whitelist_test.go`

**Step 1: 检查 proxy_helpers 优先级测试是否已覆盖 auth 优先于全局**

如果缺失，补一个最小测试：

```go
func TestNewProxyAwareHTTPClient_PrefersAuthProxyURLOverGlobalProxy(t *testing.T)
```

确保本次修复不会改变 executor 既有优先级，只改变 `auth.ProxyURL` 是否被正确装载。

**Step 2: 运行关键包测试**

Run: `go test ./internal/http/api/admin/handlers ./internal/watcher -v`（workdir: 项目根）  
Expected: PASS

Run: `go test ./internal/runtime/executor -run "ProxyAwareHTTPClient|BuildProxyTransport" -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 3: 运行主仓库全量测试**

Run: `go test ./...`（workdir: 项目根）  
Expected: PASS

**Step 4: 运行子模块全量测试（如时间允许，至少关键包）**

Run: `go test ./...`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 5: 最终提交（若前序未分批提交）**

```bash
git add -A
git commit -m "fix: align auth file proxy persistence with runtime loading"
```

**Step 6: 评审与收尾**

- 使用 `@superpowers:requesting-code-review` 做实现终审。
- 使用 `@superpowers:verification-before-completion` 复核“证据先于结论”。
