# Admin API Keys Vertex/Aistudio Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在管理员 API Keys 中支持 `vertex`（配置落地到 `vertex-api-key`），并将 `gemini` UI 文案调整为「AI Studio（Gemini API Key）」。

**Architecture:** 后端沿用现有 `/v0/admin/provider-api-keys` CRUD，在 provider 规范化、校验与 `syncSDKConfig` 中补 `vertex` 分支；前端在 `ApiKeys.tsx` 增加 `vertex` 类型与文案调整，并补最小单测覆盖映射与校验规则。保持 DB canonical provider 为 `vertex`，避免和 Models/Billing provider 口径漂移。

**Tech Stack:** Go + Gin + GORM + SQLite（测试），React + TypeScript + Vitest。

---

### Task 1: 后端 provider 归一化与规则测试（TDD）

**Files:**
- Create: `internal/http/api/admin/handlers/provider_api_keys_test.go`
- Modify: `internal/http/api/admin/handlers/provider_api_keys.go`

**Step 1: 写失败测试（provider 归一化 + vertex 校验）**

- 增加用例：
  - `normalizeProvider("vertex") == "vertex"`
  - `normalizeProvider("vertex-api-key") == "vertex"`
  - `validateProviderRow` 对 `provider=vertex` 缺少 `api_key` 报错。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_NormalizeAndValidateVertex -count=1`

Expected: FAIL（当前代码不支持 vertex）。

**Step 3: 最小实现使测试通过**

- 在 `provider_api_keys.go` 增加 `providerVertex` 常量。
- 扩展 `providerAliases` 与 `validateProviderRow` 的 `vertex` 分支。

**Step 4: 再跑测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_NormalizeAndValidateVertex -count=1`

Expected: PASS。

### Task 2: 后端字段规范化与配置同步（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/provider_api_keys_test.go`
- Modify: `internal/http/api/admin/handlers/provider_api_keys.go`

**Step 1: 写失败测试（字段清理 + syncSDKConfig 映射）**

- 增加用例：
  - `normalizeProviderFields` 在 `provider=vertex` 时清空 `api_key_entries` 与 `excluded_models`。
  - `syncSDKConfig` 将 vertex 记录写入 `cfg.VertexCompatAPIKey`（校验 base_url/api_key/models）。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_(NormalizeFields|SyncSDKConfig)_Vertex -count=1`

Expected: FAIL。

**Step 3: 最小实现使测试通过**

- 在 `normalizeProviderFields` 增加 vertex 清理逻辑。
- 在 `syncSDKConfig` switch 增加 vertex -> `sdkconfig.VertexCompatKey` 映射与 `cfg.VertexCompatAPIKey` 赋值。

**Step 4: 再跑测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_(NormalizeFields|SyncSDKConfig)_Vertex -count=1`

Expected: PASS。

### Task 3: 前端 provider 文案/规则测试（TDD）

**Files:**
- Create: `web/src/pages/admin/apiKeysProviderConfig.test.ts`
- Modify: `web/src/pages/admin/ApiKeys.tsx`

**Step 1: 写失败测试（provider 列表/文案/规则）**

- 覆盖：
  - provider 选项包含 `vertex`；
  - `gemini` 标签为 `AI Studio（Gemini API Key）`；
  - vertex 需要 `base_url`（与 codex/openai-compat 不同分支逻辑区分）。

**Step 2: 运行测试确认失败**

Run: `npm --prefix web run test -- src/pages/admin/apiKeysProviderConfig.test.ts`

Expected: FAIL。

**Step 3: 最小实现使测试通过**

- 在 `ApiKeys.tsx` 补 `vertex` provider option 与 style。
- 提炼/导出轻量 helper（如 provider 标签与 base_url 需求判断）以便单测。
- `handleSubmit` 增加 vertex 的 base_url 前置校验。

**Step 4: 再跑测试确认通过**

Run: `npm --prefix web run test -- src/pages/admin/apiKeysProviderConfig.test.ts`

Expected: PASS。

### Task 4: 端到端回归验证

**Files:**
- Modify (if needed): `web/src/pages/admin/BillingRules.tsx`（仅在联调发现 label 回退不理想时最小修复）

**Step 1: 运行后端相关测试集**

Run: `go test ./internal/http/api/admin/handlers -count=1`

Expected: PASS。

**Step 2: 运行前端测试集（最小相关）**

Run: `npm --prefix web run test -- src/pages/admin/apiKeysProviderConfig.test.ts src/pages/admin/providerCatalog.test.ts`

Expected: PASS。

**Step 3: 构建校验**

Run: `npm --prefix web run build`

Expected: Build 成功。
