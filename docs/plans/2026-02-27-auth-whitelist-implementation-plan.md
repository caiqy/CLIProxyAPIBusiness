# 认证文件页模型白名单控制 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在管理员“认证文件”页面支持按认证记录启用模型白名单，并在保存时自动转换为 `excluded_models`，实现“空白名单=禁止所有”。

**Architecture:** 采用“保存时转换”策略：前端提交 `whitelist_enabled + models`，后端在 Create/Update 时拉取 provider 模型全集并计算 `excluded_models = universe - models`。运行时不改认证选择主流程，继续依赖现有 `models/excluded_models` 过滤，保持兼容并降低风险。

**Tech Stack:** Go (Gin/GORM), TypeScript + React, Vitest, SQLite in-memory tests.

---

### Task 1: 后端白名单转换核心函数（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/provider_api_keys.go`
- Test: `internal/http/api/admin/handlers/provider_api_keys_test.go`

**Step 1: 先写失败测试（集合差集 + 空白名单 + 非法模型）**

```go
func TestProviderAPIKeys_BuildExcludedFromWhitelist(t *testing.T) {
	t.Parallel()

	universe := []string{"claude-sonnet-4-6", "claude-opus-4-1", "claude-3-7-sonnet"}

	t.Run("allowlist subset", func(t *testing.T) {
		excluded, err := buildExcludedFromWhitelist(universe, []string{"claude-sonnet-4-6"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := []string{"claude-3-7-sonnet", "claude-opus-4-1"}
		if strings.Join(excluded, ",") != strings.Join(want, ",") {
			t.Fatalf("excluded = %v, want %v", excluded, want)
		}
	})

	t.Run("empty allowlist means block all", func(t *testing.T) {
		excluded, err := buildExcludedFromWhitelist(universe, nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(excluded) != len(universe) {
			t.Fatalf("excluded len = %d, want %d", len(excluded), len(universe))
		}
	})

	t.Run("unknown allowlist model", func(t *testing.T) {
		_, err := buildExcludedFromWhitelist(universe, []string{"not-exists"})
		if err == nil || !strings.Contains(err.Error(), "unknown model") {
			t.Fatalf("want unknown model error, got %v", err)
		}
	})
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_BuildExcludedFromWhitelist -v`

Expected: FAIL，提示 `buildExcludedFromWhitelist` 未定义或行为不符。

**Step 3: 最小实现核心函数**

```go
func buildExcludedFromWhitelist(universe []string, allowlist []string) ([]string, error) {
	normalizedUniverse := normalizeStringSet(universe)
	universeSet := make(map[string]struct{}, len(normalizedUniverse))
	for _, m := range normalizedUniverse {
		universeSet[strings.ToLower(m)] = struct{}{}
	}

	normalizedAllow := normalizeStringSet(allowlist)
	for _, m := range normalizedAllow {
		if _, ok := universeSet[strings.ToLower(m)]; !ok {
			return nil, fmt.Errorf("unknown model: %s", m)
		}
	}

	allowSet := make(map[string]struct{}, len(normalizedAllow))
	for _, m := range normalizedAllow {
		allowSet[strings.ToLower(m)] = struct{}{}
	}

	excluded := make([]string, 0, len(normalizedUniverse))
	for _, m := range normalizedUniverse {
		if _, ok := allowSet[strings.ToLower(m)]; ok {
			continue
		}
		excluded = append(excluded, m)
	}
	return excluded, nil
}
```

**Step 4: 重新运行测试确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestProviderAPIKeys_BuildExcludedFromWhitelist -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/provider_api_keys.go internal/http/api/admin/handlers/provider_api_keys_test.go
git commit -m "test(auth): lock whitelist-to-excluded set conversion behavior"
```

---

### Task 2: 接入 Create/Update（`whitelist_enabled`）与 provider 模型全集解析

**Files:**
- Modify: `internal/http/api/admin/handlers/provider_api_keys.go`
- Test: `internal/http/api/admin/handlers/provider_api_keys_test.go`

**Step 1: 先写失败测试（HTTP Create/Update）**

```go
func TestProviderAPIKeys_Create_WithWhitelistEnabled_AutoGeneratesExcluded(t *testing.T) {
	// 1) 创建 gin router + handler
	// 2) POST body: whitelist_enabled=true, models=[{"name":"claude-sonnet-4-6"}]
	// 3) 读取入库 row.ExcludedModels，断言包含 universe-allowlist
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_EmptyModels_BlocksAll(t *testing.T) {
	// whitelist_enabled=true, models=[] => excluded_models == universe
}

func TestProviderAPIKeys_Create_WithWhitelistEnabled_UnknownModel_Returns400(t *testing.T) {
	// models=[{"name":"not-exists"}] => status 400
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/handlers -run "TestProviderAPIKeys_Create_WithWhitelistEnabled|TestProviderAPIKeys_Update_WithWhitelistEnabled" -v`

Expected: FAIL（字段未接入或状态码不符）。

**Step 3: 最小实现 Create/Update 接入**

```go
type createProviderAPIKeyRequest struct {
	// ...existing fields...
	WhitelistEnabled *bool `json:"whitelist_enabled"`
}

type updateProviderAPIKeyRequest struct {
	// ...existing fields...
	WhitelistEnabled *bool `json:"whitelist_enabled"`
}

func applyWhitelistIfEnabled(provider string, whitelistEnabled *bool, models []modelAlias, excluded []string) ([]string, error) {
	if whitelistEnabled == nil || !*whitelistEnabled {
		return excluded, nil
	}
	universe := loadProviderUniverse(provider) // 从 cliproxy.GlobalModelRegistry 获取
	allowlist := make([]string, 0, len(models))
	for _, m := range models {
		name := strings.TrimSpace(m.Name)
		if name != "" {
			allowlist = append(allowlist, name)
		}
	}
	return buildExcludedFromWhitelist(universe, allowlist)
}
```

关键实现要求：
- `loadProviderUniverse(provider)` 返回去重/排序后的模型列表。
- `whitelist_enabled=true` 时忽略请求中的 `excluded_models`，以后端计算结果为准。
- `unknown model` 返回 `400`（`invalid models` 的细化错误信息）。

**Step 4: 跑后端测试集确认通过**

Run: `go test ./internal/http/api/admin/handlers -run ProviderAPIKeys -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/provider_api_keys.go internal/http/api/admin/handlers/provider_api_keys_test.go
git commit -m "feat(admin): enforce per-auth whitelist by deriving excluded models on save"
```

---

### Task 3: 管理员页面接入白名单模式

**Files:**
- Modify: `web/src/pages/admin/ApiKeys.tsx`
- Modify: `web/src/locales/zh-CN.ts`
- Modify: `web/src/locales/en.ts`
- Test: `web/src/pages/admin/apiKeysProviderConfig.test.ts`

**Step 1: 先写失败测试（纯函数/配置行为）**

```ts
it('builds payload with whitelist_enabled=true', () => {
  const payload = buildApiKeyPayload({
    provider: 'claude',
    whitelistEnabled: true,
    models: [{ name: 'claude-sonnet-4-6', alias: '' }],
    excludedModels: ['legacy-value-should-be-ignored'],
  })
  expect(payload.whitelist_enabled).toBe(true)
  expect(payload.models).toHaveLength(1)
})
```

**Step 2: 运行前端测试确认失败**

Run: `npm --prefix web run test -- apiKeysProviderConfig.test.ts`

Expected: FAIL（helper 未导出或 payload 字段不存在）。

**Step 3: 最小实现 UI 和提交字段**

```ts
const [whitelistEnabled, setWhitelistEnabled] = useState<boolean>(false)

const payload = {
  provider,
  name: normalizedName,
  // ...existing fields...
  models: normalizedModels,
  whitelist_enabled: whitelistEnabled,
  excluded_models: whitelistEnabled ? undefined : excludedModels,
}
```

UI 行为：
- 新增“Whitelist mode”开关。
- 开启时隐藏/禁用“Excluded Models”编辑区，并展示提示：由系统自动生成排除列表。
- 关闭时恢复原有手工编辑 `excluded_models`。

**Step 4: 运行前端测试和静态检查**

Run: `npm --prefix web run test -- apiKeysProviderConfig.test.ts && npm --prefix web run lint`

Expected: PASS。

**Step 5: 提交**

```bash
git add web/src/pages/admin/ApiKeys.tsx web/src/locales/zh-CN.ts web/src/locales/en.ts web/src/pages/admin/apiKeysProviderConfig.test.ts
git commit -m "feat(web): add whitelist mode toggle for auth-file model control"
```

---

### Task 4: 回归验证与端到端行为确认

**Files:**
- Modify (if needed): `internal/http/api/admin/handlers/provider_api_keys_test.go`
- Modify (if needed): `web/src/pages/admin/apiKeysProviderConfig.test.ts`

**Step 1: 增加回归测试（防止语义回退）**

```go
func TestProviderAPIKeys_WhitelistEmptyMeansBlockAll(t *testing.T) {
	// 覆盖“空白名单=全禁”不回退
}
```

```ts
it('keeps excluded editor hidden when whitelist mode on', () => {
  // 确认 UI 行为稳定
})
```

**Step 2: 运行完整相关测试**

Run: `go test ./internal/http/api/admin/handlers -v`

Run: `npm --prefix web run test -- apiKeysProviderConfig.test.ts`

Expected: 全部 PASS。

**Step 3: 手工验收（管理员面板）**

1. 创建 `claude` 认证，开启白名单，`models=[]`，保存成功。
2. 用该认证调用任意模型，预期不可用（最终 `model_not_found`）。
3. 编辑为 `models=[claude-sonnet-4-6]`，保存后仅该模型可用。

**Step 4: 最终提交**

```bash
git add internal/http/api/admin/handlers/provider_api_keys*.go web/src/pages/admin/ApiKeys.tsx web/src/locales/*.ts web/src/pages/admin/apiKeysProviderConfig.test.ts
git commit -m "fix(auth): align admin whitelist semantics with model availability outcomes"
```

---

## 备注（执行约束）

- 严格按 TDD：先失败测试，再最小实现，再通过测试。
- 每个任务独立提交，避免大包提交。
- 若测试失败超过两轮，先回到根因分析，不连续叠加修复。
