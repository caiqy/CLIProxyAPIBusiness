# Provider 驱动认证文件导入弹窗 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 AuthFiles 管理页新增“按 Provider 导入认证文件”弹窗能力，支持文件导入与文本导入，并通过后端 provider 专用接口做强校验与规范化入库。

**Architecture:** 新增后端 `import-by-provider` 专用链路（路由 + 权限 + handler + provider 校验器），前端在 `New` 菜单增加新入口并打开独立导入弹窗。文件导入与文本导入统一转换为 `entries[]` 请求体提交，后端按 provider 仅提取并校验必要字段后 upsert 入库；前端按返回结果展示逐条错误。

**Tech Stack:** Go (Gin/GORM)、React + TypeScript + Vitest、i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

> 执行要求：严格按 @superpowers/test-driven-development 执行（先失败测试，再最小实现，再通过测试）。

### Task 1: 后端新增 Provider 校验与规范化核心（纯函数）

**Files:**
- Create: `internal/http/api/admin/handlers/auth_files_provider_import.go`
- Create: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`

**Step 1: 写失败测试（先定义规则）**

新增测试覆盖以下行为：

```go
func TestNormalizeProviderEntry_Codex_KeepRequiredFieldsOnly(t *testing.T)
func TestNormalizeProviderEntry_Kiro_MissingRequiredField(t *testing.T)
func TestNormalizeProviderEntry_UnknownProvider(t *testing.T)
func TestNormalizeProviderEntry_IgnoreExtraFields(t *testing.T)
```

示例断言（Codex）：

```go
raw := map[string]any{
  "key": "codex-main",
  "type": "codex",
  "access_token": "xxx",
  "base_url": "https://api.openai.com",
  "unused": "will-be-dropped",
}
normalized, err := normalizeProviderEntry("codex", raw)
require.NoError(t, err)
assert.Equal(t, "codex-main", normalized["key"])
assert.Equal(t, "codex", normalized["type"])
assert.Equal(t, "xxx", normalized["access_token"])
_, exists := normalized["unused"]
assert.False(t, exists)
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run NormalizeProviderEntry -count=1 -v`

Expected: FAIL（函数/规则尚不存在）。

**Step 3: 最小实现**

在新文件实现：

1. provider 枚举与字段白名单（例如 `codex`、`anthropic`、`gemini-cli`、`antigravity`、`qwen`、`kiro`、`iflow-cookie`）。
2. `normalizeProviderEntry(provider string, raw map[string]any) (map[string]any, error)`：
   - 仅提取该 provider 必需/允许字段；
   - 校验必填字段与类型；
   - 丢弃无关字段；
   - `type` 字段标准化为 provider（若缺失则自动补齐）。

最小实现模板：

```go
type providerRule struct {
    Required []string
    Optional []string
}

func normalizeProviderEntry(provider string, raw map[string]any) (map[string]any, error) {
    // 1) load rule by provider
    // 2) validate required fields exist and are non-empty string
    // 3) copy only required+optional
    // 4) set normalized["type"] = provider
    // 5) return normalized
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run NormalizeProviderEntry -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files_provider_import.go internal/http/api/admin/handlers/auth_files_provider_import_test.go
git commit -m "feat(admin): add provider import normalization rules"
```

---

### Task 2: 接入新导入接口（路由/权限/handler）

**Files:**
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Create: `internal/http/api/admin/permissions/auth_files_provider_import_permissions_test.go`
- Create: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`

**Step 1: 写失败测试（权限 + handler 请求校验）**

1. 权限测试：

```go
func TestDefinitionMapIncludesAuthFilesImportByProviderPermission(t *testing.T)
```

断言 key：`POST /v0/admin/auth-files/import-by-provider`

2. handler 测试（建议 `httptest`）：

```go
func TestImportByProvider_RejectsInvalidPayload(t *testing.T)
func TestImportByProvider_AllowsPartialSuccess(t *testing.T)
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/permissions -run ImportByProvider -count=1 -v`

Expected: FAIL（权限未定义）。

Run: `go test ./internal/http/api/admin/handlers -run ImportByProvider -count=1 -v`

Expected: FAIL（路由/handler 未实现）。

**Step 3: 最小实现**

1. `admin.go` 新增：

```go
authed.POST("/auth-files/import-by-provider", authFileHandler.ImportByProvider)
```

2. `permissions.go` 新增：

```go
newDefinition("POST", "/v0/admin/auth-files/import-by-provider", "Import Auth Files By Provider", "Auth Files"),
```

3. `auth_files.go` 新增请求体与 handler：

```go
type importByProviderRequest struct {
    Provider    string              `json:"provider"`
    Source      string              `json:"source"`
    AuthGroupID models.AuthGroupIDs `json:"auth_group_id"`
    Entries     []map[string]any    `json:"entries"`
}
```

`ImportByProvider` 逻辑：
- 校验 provider/source/entries；
- default auth group 兜底；
- 遍历 entries 调用 `normalizeProviderEntry`；
- 仅用规范化结果构造 `models.Auth.Content`；
- 复用现有 upsert 冲突策略；
- 返回 `imported + failed[]`（建议 `failed` 增加 `index` 字段）。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/permissions -run ImportByProvider -count=1 -v`

Expected: PASS。

Run: `go test ./internal/http/api/admin/handlers -run ImportByProvider -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/permissions/auth_files_provider_import_permissions_test.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go
git commit -m "feat(admin): add provider-driven auth file import endpoint"
```

---

### Task 3: 前端新增 Provider 导入弹窗入口与模板

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Create: `web/src/pages/admin/AuthFilesProviderImportModal.tsx`
- Create: `web/src/pages/admin/providerImportTemplates.ts`
- Create: `web/src/pages/admin/providerImportSchema.ts`
- Create: `web/src/pages/admin/providerImportSchema.test.ts`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 写失败测试（前端纯函数）**

在 `providerImportSchema.test.ts` 覆盖：

```ts
it('builds payload from text json object/array', ...)
it('rejects invalid json text', ...)
it('normalizes provider before submit', ...)
it('keeps only required fields for provider payload', ...)
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: FAIL（函数未实现）。

**Step 3: 最小实现**

1. `AuthFiles.tsx`：
   - `New` 菜单新增项：`Import Auth Files (Provider)`；
   - 新增弹窗状态（`providerImportModalOpen`）。

2. `AuthFilesProviderImportModal.tsx`：
   - 顶部固定：Provider + Auth Group；
   - Tabs：文件导入 / 文本导入 / Provider 示例；
   - `Import` 按钮调用新接口 `/v0/admin/auth-files/import-by-provider`。

3. `providerImportTemplates.ts`：
   - 每个 provider 的真实最小 JSON 示例。

4. `providerImportSchema.ts`：
   - 解析文本 JSON；
   - 文件读取后转 entries；
   - 构造统一 payload。

5. `en.ts` / `zh-CN.ts`：
   - 新增导入弹窗文案 key。

**Step 4: 运行测试确认通过**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS（包含既有测试）。

**Step 5: Commit**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/AuthFilesProviderImportModal.tsx web/src/pages/admin/providerImportTemplates.ts web/src/pages/admin/providerImportSchema.ts web/src/pages/admin/providerImportSchema.test.ts web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "feat(web): add provider-driven auth file import modal"
```

---

### Task 4: 联调与错误反馈完善（部分成功/逐条失败）

**Files:**
- Modify: `web/src/pages/admin/AuthFilesProviderImportModal.tsx`
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`

**Step 1: 写失败测试（返回结构与前端渲染）**

后端新增断言：

```go
func TestImportByProvider_ReturnsIndexedFailures(t *testing.T)
```

前端新增断言（若采用纯函数可测试）：

```ts
it('maps failed items to result list UI model', ...)
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run IndexedFailures -count=1 -v`

Expected: FAIL。

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: FAIL（新增断言未满足）。

**Step 3: 最小实现**

1. 后端失败项返回 `index/error`（必要时附 `key`）。
2. 前端结果区展示：
   - `Imported {{count}}`
   - 失败列表（第 N 条 + 错误原因）
3. 成功后支持：
   - 关闭并刷新列表
   - 留在弹窗继续导入

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run ImportByProvider -count=1 -v`

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go web/src/pages/admin/AuthFilesProviderImportModal.tsx web/src/pages/admin/providerImportSchema.test.ts
git commit -m "feat: improve provider import validation feedback"
```

---

### Task 5: 全量验证与收口

**Files:**
- Modify (if needed): `docs/plans/2026-02-18-provider-driven-auth-import-design.md`

**Step 1: 后端回归**

Run: `go test ./internal/http/api/admin/permissions ./internal/http/api/admin/handlers -v`

Expected: PASS。

**Step 2: 前端回归**

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

Run: `npm run lint`（workdir=`web`）

Expected: 允许存在仓库历史基线问题，但本次新增文件不引入新的 lint 报错。

**Step 3: 全仓 Go 回归**

Run: `go test ./...`

Expected: PASS（如有历史失败，需明确与本改动无关）。

**Step 4: 需求核对清单**

- [ ] `New` 菜单出现新的 Provider 导入入口
- [ ] 可选 provider 并展示对应真实示例
- [ ] 同时支持文件导入与文本导入
- [ ] 导入走 `/v0/admin/auth-files/import-by-provider`
- [ ] 仅取用并校验必要字段，冗余字段不阻断导入
- [ ] 结果区可展示部分成功与逐条失败

**Step 5: 最终提交（若前面未分批）**

```bash
git add -A
git commit -m "feat(admin): support provider-driven auth file import modal"
```
