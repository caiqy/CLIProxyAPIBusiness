# Provider 导入自动 key/type Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 provider 导入改为“用户无需提供 key/type”，由系统自动生成；并把字段校验收敛到真实运行时最小依赖（含 iFlow 三模式）。

**Architecture:** 在后端统一完成 key/type 生成与最小必需字段校验：`type` 由 provider canonical 映射生成，`key` 优先 `provider+email`，无 email 时回退 `provider+凭据哈希`。前端仅输入 provider 相关凭据，不再提交 key/type，并更新示例与提示文案。通过后端单测 + 前端 schema 测试确保规则稳定。

**Tech Stack:** Go (Gin/GORM)、TypeScript + React + Vitest、i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

> 执行要求：严格按 @superpowers/test-driven-development（先失败测试，再最小实现，再验证通过）。

### Task 1: 后端补齐自动 key/type 与最小依赖规则（纯函数层）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`

**Step 1: 写失败测试**

新增测试：

```go
func TestGenerateImportKey_UsesProviderAndEmail(t *testing.T)
func TestGenerateImportKey_FallbackToCredentialHashWhenNoEmail(t *testing.T)
func TestNormalizeProviderEntry_DoesNotRequireUserKeyOrType(t *testing.T)
func TestNormalizeProviderEntry_IFlow_ThreeModes(t *testing.T)
func TestNormalizeProviderEntry_Gemini_AllowsTokenAccessToken(t *testing.T)
```

关键断言：
- 输入无 `key/type` 时，normalize 结果含自动 `key/type`。
- `key` 优先 `provider+normalizedEmail`。
- 无 email 时稳定回退哈希（同输入同 key）。
- iFlow 三模式：`api_key` 或 `cookie+email` 或 `refresh_token` 任一满足即通过。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run "GenerateImportKey|NormalizeProviderEntry" -count=1 -v`

Expected: FAIL（现有实现仍要求 key，且校验口径不完全一致）。

**Step 3: 最小实现**

在 `auth_files_provider_import.go` 增加/调整：

1. `canonicalizeImportProvider()` 保持现有映射。
2. 新增 `generateImportKey(provider string, normalized map[string]any) string`：
   - 先取 `email`（trim+lower）=> `provider-email`。
   - 无 email 回退 `provider-h-<sha256[:12]>`（hash 输入为 provider + 核心凭据串）。
3. `normalizeProviderEntry()`：
   - 忽略用户传入 `key/type`；
   - 仅提取允许字段并做最小依赖校验；
   - 写入自动 `type`、自动 `key`。
4. provider 最小必需规则对齐：
   - codex/claude/qwen/antigravity/kiro：`access_token`
   - gemini：`access_token` 或 `token.access_token`
   - iflow：三模式（A `api_key` / B `cookie+email` / C `refresh_token`）

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run "GenerateImportKey|NormalizeProviderEntry" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files_provider_import.go internal/http/api/admin/handlers/auth_files_provider_import_test.go
git commit -m "feat(admin): auto-generate import key/type and align provider rules"
```

---

### Task 2: Handler 层对齐（去除用户 key/type 依赖）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go`
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import.go`

**Step 1: 写失败测试**

新增/更新测试：

```go
func TestImportByProvider_AutoGeneratesKeyAndType(t *testing.T)
func TestImportByProvider_UsesProviderEmailKeyWhenEmailPresent(t *testing.T)
func TestImportByProvider_UsesFallbackKeyWhenEmailMissing(t *testing.T)
```

关键断言：
- 请求 entry 不带 key/type 也可导入成功。
- 返回/落库内容含自动 key/type。
- 同 provider+email 重复导入应命中同 key（upsert 更新）。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run "ImportByProvider_(AutoGenerates|UsesProviderEmail|UsesFallback)" -count=1 -v`

Expected: FAIL。

**Step 3: 最小实现**

在 `ImportByProvider` 里：
- 不再从原始输入读取 key；统一读取 normalize 后 key。
- 错误信息从 “missing key” 转为更具体字段缺失信息。
- 保持 `failed[index,error]` 返回结构不变。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run "ImportByProvider" -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files_provider_import.go internal/http/api/admin/handlers/auth_files_provider_import_handler_test.go
git commit -m "feat(admin): remove user key/type dependency in provider import"
```

---

### Task 3: 前端示例与提交 payload 对齐（不再要求 key/type）

**Files:**
- Modify: `web/src/pages/admin/providerImportTemplates.ts`
- Modify: `web/src/pages/admin/providerImportSchema.ts`
- Modify: `web/src/pages/admin/providerImportSchema.test.ts`
- Modify: `web/src/pages/admin/AuthFilesProviderImportModal.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 写失败测试**

更新/新增测试：

```ts
it('does not require key/type in template payload', ...)
it('sanitizes payload without user-provided key/type', ...)
it('supports iflow three-mode inputs', ...)
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: FAIL（当前模板仍含 key/type）。

**Step 3: 最小实现**

1. `providerImportTemplates.ts`：去掉示例中的 `key/type`。
2. `providerImportSchema.ts`：
   - 不从用户输入保留 `key/type`；
   - payload 仅提交 provider 相关字段。
3. `AuthFilesProviderImportModal.tsx`：
   - 增加提示：`key` 与 `type` 将由系统自动生成；
   - iFlow 提示三模式输入示例。
4. i18n 新增对应文案。

**Step 4: 运行测试确认通过**

Run: `npm run test -- providerImportSchema.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run test`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/providerImportTemplates.ts web/src/pages/admin/providerImportSchema.ts web/src/pages/admin/providerImportSchema.test.ts web/src/pages/admin/AuthFilesProviderImportModal.tsx web/src/locales/en.ts web/src/locales/zh-CN.ts
git commit -m "feat(web): align provider import ui with auto key/type rules"
```

---

### Task 4: 全量回归与需求核对

**Files:**
- Modify (if needed): `docs/plans/2026-02-18-provider-driven-auth-import-design.md`

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

Expected: 允许历史基线问题；本次新增/修改文件不引入新 lint 报错。

**Step 3: 需求核对清单**

- [ ] 用户不需要提交 `key/type`
- [ ] 系统自动生成 `type`
- [ ] `key` 优先 `provider+email`，无 email 回退凭据哈希
- [ ] email 全局可选
- [ ] iFlow 三模式校验生效
- [ ] provider 最小必需字段与真实运行时代码一致

**Step 4: 最终提交（若前面未分批）**

```bash
git add -A
git commit -m "feat(admin): finalize auto key/type provider import rules"
```
