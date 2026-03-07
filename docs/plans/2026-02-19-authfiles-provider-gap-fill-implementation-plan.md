# AuthFiles Provider 缺口补齐 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在管理员 AuthFiles 板块补齐可用 provider 能力：新增认证补 `iflow/kimi/github-copilot/kilo`，导入补 `kimi/github-copilot/kilo`，并为新增导入 provider 提供专有可用字段示例。

**Architecture:** 后端分两条线增量扩展：`/tokens/*` 路由+权限+SDK requester 暴露，以及 `import-by-provider` 规则补齐。前端在 `AuthFiles` 与 Provider Import 组件扩展 provider 列表，并统一复用设备码展示与轮询机制。保持现有页面结构，不新增新页面，不做占位 provider。

**Tech Stack:** Go (Gin/GORM)、React + TypeScript + Vitest、i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

### Task 1: 打通后端 token 请求链路（Kimi/Copilot/Kilo）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/sdk/api/management.go`
- Create: `third_party/CLIProxyAPIPlus/sdk/api/management_github_kilo_test.go`
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Create: `internal/http/api/admin/permissions/token_provider_gapfill_permissions_test.go`

**Step 1: 写失败测试（先定义能力）**

在 `management_github_kilo_test.go` 增加：

```go
func TestManagementTokenRequesterExposesGitHubMethod(t *testing.T)
func TestManagementTokenRequesterExposesKiloMethod(t *testing.T)
```

断言 `managementTokenRequester` 实现：

```go
interface{ RequestGitHubToken(*gin.Context) }
interface{ RequestKiloToken(*gin.Context) }
```

在 `token_provider_gapfill_permissions_test.go` 增加：

```go
func TestDefinitionMapIncludesKimiTokenPermission(t *testing.T)
func TestDefinitionMapIncludesGitHubCopilotTokenPermission(t *testing.T)
func TestDefinitionMapIncludesKiloTokenPermission(t *testing.T)
```

**Step 2: 运行测试确认失败**

Run: `go test ./sdk/api -run "GitHubMethod|KiloMethod" -count=1 -v`（workdir=`third_party/CLIProxyAPIPlus`）

Expected: FAIL（接口未暴露对应方法）。

Run: `go test ./internal/http/api/admin/permissions -run "KimiToken|GitHubCopilotToken|KiloToken" -count=1 -v`

Expected: FAIL（权限 key 尚未定义）。

**Step 3: 最小实现**

1. `management.go`：

```go
RequestGitHubToken(*gin.Context)
RequestKiloToken(*gin.Context)
```

并实现转发：

```go
func (m *managementTokenRequester) RequestGitHubToken(c *gin.Context) { m.handler.RequestGitHubToken(c) }
func (m *managementTokenRequester) RequestKiloToken(c *gin.Context) { m.handler.RequestKiloToken(c) }
```

2. `admin.go` 路由补齐：

```go
authed.POST("/tokens/kimi", tokenRequester.RequestKimiToken)
authed.POST("/tokens/github-copilot", tokenRequester.RequestGitHubToken)
authed.POST("/tokens/kilo", tokenRequester.RequestKiloToken)
```

3. `permissions.go` 补齐：

```go
newDefinition("POST", "/v0/admin/tokens/kimi", "Request Kimi Token", "Auth Tokens"),
newDefinition("POST", "/v0/admin/tokens/github-copilot", "Request GitHub Copilot Token", "Auth Tokens"),
newDefinition("POST", "/v0/admin/tokens/kilo", "Request Kilo Token", "Auth Tokens"),
```

**Step 4: 运行测试确认通过**

Run: `go test ./sdk/api -run "GitHubMethod|KiloMethod" -count=1 -v`（workdir=`third_party/CLIProxyAPIPlus`）

Expected: PASS。

Run: `go test ./internal/http/api/admin/permissions -run "KimiToken|GitHubCopilotToken|KiloToken" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/sdk/api/management.go third_party/CLIProxyAPIPlus/sdk/api/management_github_kilo_test.go internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/token_provider_gapfill_permissions_test.go
git commit -m "feat(admin): add kimi copilot kilo token request endpoints"
```

---

### Task 2: 补齐后端导入规则（Kimi/Copilot/Kilo）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`

**Step 1: 写失败测试（规则先行）**

在 `auth_files_provider_import_test.go` 增加：

```go
func TestNormalizeProviderEntry_Kimi_AccessTokenRequired(t *testing.T)
func TestNormalizeProviderEntry_GitHubCopilot_AccessTokenRequired(t *testing.T)
func TestNormalizeProviderEntry_Kilo_AccessTokenRequired(t *testing.T)
func TestNormalizeProviderEntry_GitHubCopilot_CanonicalType(t *testing.T)
```

在 handler 测试补一条：

```go
func TestImportByProvider_GitHubCopilot_SucceedsWithAccessTokenOnly(t *testing.T)
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run "NormalizeProviderEntry_(Kimi|GitHubCopilot|Kilo)|ImportByProvider_GitHubCopilot" -count=1 -v`

Expected: FAIL（alias/rules 未覆盖）。

**Step 3: 最小实现**

1. `providerAliasToCanonical` 增加：

```go
"kimi": "kimi",
"github-copilot": "github-copilot",
"kilo": "kilo",
```

2. `providerImportRules` 增加三者规则（最小要求 `access_token`）。

3. 保持既有策略不变：
- 自动 `type`
- 自动 `key`
- 冗余字段忽略

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run "NormalizeProviderEntry_(Kimi|GitHubCopilot|Kilo)|ImportByProvider_GitHubCopilot" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files_provider_import.go internal/http/api/admin/handlers/auth_files_provider_import_test.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go
git commit -m "feat(admin): support kimi copilot kilo in provider import"
```

---

### Task 3: 前端新增认证入口补齐（iflow/kimi/copilot/kilo）

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Modify: `web/src/pages/admin/authFilesAuthFlow.ts`
- Modify: `web/src/pages/admin/authFilesKiroFlow.test.ts`
- Create: `web/src/pages/admin/authFilesDeviceCodeStart.test.ts`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 写失败测试**

新增 `authFilesDeviceCodeStart.test.ts`：

```ts
it('normalizes token start response with verification_uri and user_code', ...)
it('supports github-copilot device code payload', ...)
it('supports kilo device code payload', ...)
```

并在 `authFilesKiroFlow.test.ts` 补一条：

```ts
it('supports providers that only return state and url', ...)
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- authFilesDeviceCodeStart.test.ts authFilesKiroFlow.test.ts`（workdir=`web`）

Expected: FAIL（start response 尚未解析 device code 字段）。

**Step 3: 最小实现**

1. `AUTH_TYPES` 增加：

```ts
{ key: 'iflow', label: 'iFlow OAuth', endpoint: '/v0/admin/tokens/iflow' }
{ key: 'kimi', label: 'Kimi', endpoint: '/v0/admin/tokens/kimi' }
{ key: 'github-copilot', label: 'GitHub Copilot', endpoint: '/v0/admin/tokens/github-copilot' }
{ key: 'kilo', label: 'Kilo', endpoint: '/v0/admin/tokens/kilo' }
```

2. `authFilesAuthFlow.ts` 的 `TokenStartResponse` 扩展：

```ts
verification_url?: string;
verification_uri?: string;
user_code?: string;
```

3. `AuthFiles.tsx` 在 `handleNewAuthType` 读取 start response 的设备码字段，设置 `deviceVerificationUrl/deviceUserCode`，并对 `kiro/github-copilot/kilo` 自动开始轮询。

4. 设备码 UI 区块对非 Kiro provider 也可渲染（条件改为“有设备码数据时显示”）。

**Step 4: 运行测试确认通过**

Run: `npm run test -- authFilesDeviceCodeStart.test.ts authFilesKiroFlow.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesAuthFlow.ts web/src/pages/admin/authFilesKiroFlow.test.ts web/src/pages/admin/authFilesDeviceCodeStart.test.ts web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "feat(web): add iflow kimi copilot kilo auth entries in auth files"
```

---

### Task 4: 前端导入入口与示例补齐（kimi/copilot/kilo）

**Files:**
- Modify: `web/src/pages/admin/providerImportTemplates.ts`
- Modify: `web/src/pages/admin/providerImportSchema.ts`
- Modify: `web/src/pages/admin/providerImportSchema.test.ts`
- Modify: `web/src/pages/admin/AuthFilesProviderImportModal.tsx`

**Step 1: 写失败测试**

在 `providerImportSchema.test.ts` 增加：

```ts
it('supports kimi import entry with access_token', ...)
it('supports github-copilot import entry with access_token', ...)
it('supports kilo import entry with access_token', ...)
it('provider option list includes kimi github-copilot kilo', ...)
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: FAIL（ProviderImportKey/模板尚未包含新增 provider）。

**Step 3: 最小实现**

1. `ProviderImportKey` 扩展：

```ts
| 'kimi' | 'github-copilot' | 'kilo'
```

2. `PROVIDER_IMPORT_OPTIONS` 与 `PROVIDER_IMPORT_TEMPLATES` 增加三者专有示例（仅可用字段，示例可复制可填充）。

3. `providerImportSchema.ts` 的 `PROVIDER_ALLOWED_FIELDS` 覆盖新增 provider。

4. `AuthFilesProviderImportModal.tsx` 保持现有示例交互（复制/填充），无需额外流程改造。

**Step 4: 运行测试确认通过**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/providerImportTemplates.ts web/src/pages/admin/providerImportSchema.ts web/src/pages/admin/providerImportSchema.test.ts web/src/pages/admin/AuthFilesProviderImportModal.tsx
git commit -m "feat(web): add kimi copilot kilo provider import examples"
```

---

### Task 5: 回归验证与需求核对

**Files:**
- Modify (if needed): `docs/plans/2026-02-19-authfiles-provider-gap-fill-design.md`

**Step 1: 后端回归**

Run: `go test ./internal/http/api/admin/permissions ./internal/http/api/admin/handlers -v`

Expected: PASS。

Run: `go test ./sdk/api -v`（workdir=`third_party/CLIProxyAPIPlus`）

Expected: PASS。

**Step 2: 前端回归**

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

Run: `npm run lint`（workdir=`web`）

Expected: 允许历史基线问题；本次新增文件不引入新 lint 报错。

**Step 3: 全仓 Go 回归**

Run: `go test ./...`

Expected: PASS（如有历史失败需单独标注与本改动无关）。

**Step 4: 需求核对清单**

- [ ] New 菜单出现 `iflow`、`kimi`、`github-copilot`、`kilo`
- [ ] Import 支持 `kimi`、`github-copilot`、`kilo`
- [ ] 新增导入 provider 具备专有可用字段示例（可复制+可填充）
- [ ] `key/type` 仍由系统自动生成
- [ ] 设备码 provider（copilot/kilo）在弹窗可见 `verification_url + user_code`
- [ ] 既有 provider 流程无回归

**Step 5: 最终提交（若前面未分批）**

```bash
git add -A
git commit -m "feat(admin): fill authfiles provider gaps for new and import flows"
```
