# 管理员平台 Provider Catalog（Models & BillingRules）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为管理员平台 Models 与 BillingRules 页面引入统一的后端 Provider Catalog 数据源，补齐 `kiro` 并与运行时可注册 provider 全量对齐。

**Architecture:** 后端新增 `GET /v0/admin/model-mappings/providers`，由单一函数返回固定顺序的全量 provider 集合；前端移除两页硬编码 provider 列表，改为启动时请求该接口并渲染下拉，模型列表继续复用现有 `available-models` 接口。两页共用同一前端 provider 适配逻辑，确保一致性。

**Tech Stack:** Go (Gin/GORM)、React + TypeScript + Vitest、i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

### Task 1: 后端新增 Provider Catalog 核心函数与单测

**Files:**
- Modify: `internal/http/api/admin/handlers/model_mappings.go`
- Create: `internal/http/api/admin/handlers/model_mappings_providers_test.go`

**Step 1: 写失败测试（先定义口径）**

在新测试文件中覆盖：

```go
func TestListProviderCatalog_ReturnsStableOrder(t *testing.T)
func TestListProviderCatalog_IncludesKiroAndKilo(t *testing.T)
func TestListProviderCatalog_NoDuplicates(t *testing.T)
```

断言列表至少包含并按固定顺序输出：

```text
gemini, vertex, gemini-cli, aistudio, antigravity, claude, codex, qwen,
iflow, kimi, github-copilot, kiro, kilo, openai-compatibility
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run ProviderCatalog -count=1 -v`

Expected: FAIL（函数尚不存在）。

**Step 3: 最小实现**

在 `model_mappings.go` 增加：

```go
type providerCatalogItem struct {
    ID             string `json:"id"`
    Label          string `json:"label"`
    Category       string `json:"category"`
    SupportsModels bool   `json:"supports_models"`
}

func listProviderCatalog() []providerCatalogItem {
    return []providerCatalogItem{ ...固定顺序... }
}
```

只做最小字段与固定顺序，不引入动态探测。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run ProviderCatalog -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/model_mappings.go internal/http/api/admin/handlers/model_mappings_providers_test.go
git commit -m "feat(admin): add stable provider catalog for model mappings"
```

---

### Task 2: 新增后端接口、路由与权限（含权限测试）

**Files:**
- Modify: `internal/http/api/admin/handlers/model_mappings.go`
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Create: `internal/http/api/admin/permissions/model_mappings_providers_permissions_test.go`
- Modify: `internal/http/api/admin/handlers/model_mappings_providers_test.go`

**Step 1: 写失败测试**

1) 权限测试：

```go
func TestDefinitionMapIncludesModelMappingsProvidersPermission(t *testing.T)
```

断言 key：`GET /v0/admin/model-mappings/providers`

2) handler 测试（在 `model_mappings_providers_test.go`）：

```go
func TestAvailableProviders_ReturnsCatalogShape(t *testing.T)
```

断言响应：
- HTTP 200
- `providers` 数组存在
- 每项含 `id/label/category/supports_models`

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/permissions -run ModelMappingsProviders -count=1 -v`

Expected: FAIL。

Run: `go test ./internal/http/api/admin/handlers -run AvailableProviders -count=1 -v`

Expected: FAIL。

**Step 3: 最小实现**

1. `model_mappings.go` 新增方法：

```go
func (h *ModelMappingHandler) AvailableProviders(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"providers": listProviderCatalog()})
}
```

2. `admin.go` 增加路由：

```go
authed.GET("/model-mappings/providers", modelMappingHandler.AvailableProviders)
```

3. `permissions.go` 增加权限定义：

```go
newDefinition("GET", "/v0/admin/model-mappings/providers", "List Model Mapping Providers", "Models"),
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/permissions -run ModelMappingsProviders -count=1 -v`

Expected: PASS。

Run: `go test ./internal/http/api/admin/handlers -run "ProviderCatalog|AvailableProviders" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/model_mappings.go internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/model_mappings_providers_permissions_test.go internal/http/api/admin/handlers/model_mappings_providers_test.go
git commit -m "feat(admin): expose provider catalog endpoint for model pages"
```

---

### Task 3: 前端抽取共享 Provider Catalog 适配逻辑（先测再改）

**Files:**
- Create: `web/src/pages/admin/providerCatalog.ts`
- Create: `web/src/pages/admin/providerCatalog.test.ts`

**Step 1: 写失败测试**

测试覆盖：

```ts
it('maps backend catalog to dropdown options in order', ...)
it('filters empty provider ids', ...)
it('falls back label to id when label is empty', ...)
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- providerCatalog.test.ts`（workdir=`web`）

Expected: FAIL。

**Step 3: 最小实现**

在 `providerCatalog.ts` 实现：

```ts
export interface AdminProviderItem { id: string; label: string; category: string; supports_models: boolean }
export interface DropdownOption { label: string; value: string }
export function toProviderDropdownOptions(input: AdminProviderItem[]): DropdownOption[] { ... }
```

保证顺序按接口原序，避免前端二次排序。

**Step 4: 运行测试确认通过**

Run: `npm run test -- providerCatalog.test.ts`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/providerCatalog.ts web/src/pages/admin/providerCatalog.test.ts
git commit -m "feat(web): add shared provider catalog adapter for admin pages"
```

---

### Task 4: Models 与 BillingRules 页面改为后端 provider 源

**Files:**
- Modify: `web/src/pages/admin/Models.tsx`
- Modify: `web/src/pages/admin/BillingRules.tsx`

**Step 1: 写失败测试（以适配层为主）**

新增适配层用例（若需要可补充到 `providerCatalog.test.ts`）：

```ts
it('contains kiro in dropdown options when backend returns it', ...)
```

并先跑现有相关测试（若无页面测试，至少保证适配测试失败-转绿）。

**Step 2: 运行测试确认失败**

Run: `npm run test -- providerCatalog.test.ts`（workdir=`web`）

Expected: FAIL（新增断言尚未满足）。

**Step 3: 最小实现**

1. 在两个页面移除本地硬编码 `PROVIDER_OPTIONS` 依赖。
2. 页面加载时请求：`/v0/admin/model-mappings/providers`。
3. 使用 `toProviderDropdownOptions` 构造下拉数据。
4. 保持模型加载逻辑不变（仍请求 `available-models`）。
5. provider 接口失败时：
   - 显示错误提示（可重试）；
   - 不使用本地回退常量。

**Step 4: 运行测试与构建确认通过**

Run: `npm run test -- providerCatalog.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/Models.tsx web/src/pages/admin/BillingRules.tsx web/src/pages/admin/providerCatalog.ts web/src/pages/admin/providerCatalog.test.ts
git commit -m "feat(web): load model and billing providers from admin catalog api"
```

---

### Task 5: 全量回归与验收

**Files:**
- Modify (if needed): `docs/plans/2026-02-19-admin-provider-catalog-for-models-and-billing-design.md`

**Step 1: 后端回归**

Run: `go test ./internal/http/api/admin/handlers ./internal/http/api/admin/permissions -v`

Expected: PASS。

Run: `go test ./...`

Expected: PASS。

**Step 2: 前端回归**

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

Run: `npm run lint`（workdir=`web`）

Expected: 允许仓库历史基线问题；本次新增文件不引入新的 lint 报错。

**Step 3: 需求核对清单**

- [ ] Models 与 BillingRules 都不再依赖硬编码 provider 列表
- [ ] 两页 provider 来自 `GET /v0/admin/model-mappings/providers`
- [ ] provider 列表包含 `kiro`
- [ ] provider 列表按后端固定顺序一致展示
- [ ] provider 接口失败时可感知且可重试（无前端回退常量）

**Step 4: 最终提交（若前面未分批）**

```bash
git add -A
git commit -m "feat(admin): use provider catalog api for models and billing rules"
```
