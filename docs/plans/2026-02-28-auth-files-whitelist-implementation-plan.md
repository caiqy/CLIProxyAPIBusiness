# Auth Files 模型白名单（预置模型）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Auth Files 管理页实现端到端模型白名单：按 `content.type` 自动识别 provider、自动加载预置模型列表、保存时转换并在运行时选路生效。

**Architecture:** 在 `auths` 表增加 `whitelist_enabled/allowed_models/excluded_models` 三列；Create/Update 时按预置模型全集计算 `excluded_models`。新增 `GET /v0/admin/auth-files/model-presets` 接口供前端勾选模型。watcher 将 `excluded_models` 注入 runtime auth attributes，复用现有 `ClientSupportsModel` 过滤链路。

**Tech Stack:** Go (Gin + GORM), SQLite/Postgres migration, React + TypeScript + Vitest, CLIProxy SDK model registry.

---

### Task 1: 扩展 Auth 数据模型与数据库迁移（TDD）

**Files:**
- Modify: `internal/models/auth.go`
- Modify: `internal/db/migrate.go`
- Create: `internal/db/migrate_auth_whitelist_test.go`

**Step 1: Write the failing test**

```go
func TestMigrate_EnsuresAuthWhitelistColumnsSQLite(t *testing.T) {
	db := openSQLiteForTest(t)
	if err := MigrateSQLite(db); err != nil { t.Fatal(err) }

	m := db.Migrator()
	if !m.HasColumn(&models.Auth{}, "whitelist_enabled") { t.Fatal("missing whitelist_enabled") }
	if !m.HasColumn(&models.Auth{}, "allowed_models") { t.Fatal("missing allowed_models") }
	if !m.HasColumn(&models.Auth{}, "excluded_models") { t.Fatal("missing excluded_models") }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/db -run TestMigrate_EnsuresAuthWhitelistColumnsSQLite -v`

Expected: FAIL（列不存在）。

**Step 3: Write minimal implementation**

```go
// internal/models/auth.go
WhitelistEnabled bool           `gorm:"type:boolean;not null;default:false"`
AllowedModels    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
ExcludedModels   datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`

// internal/db/migrate.go
func ensureAuthModelWhitelistColumnsPostgres(conn *gorm.DB) error { /* ALTER TABLE ... ADD COLUMN IF NOT EXISTS */ }
func ensureAuthModelWhitelistColumnsSQLite(conn *gorm.DB, migrator gorm.Migrator) error { /* HasColumn + ALTER */ }

// 在 PostgreSQL/SQLite 迁移主流程中调用以上函数
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/db -run TestMigrate_EnsuresAuthWhitelistColumnsSQLite -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/models/auth.go internal/db/migrate.go internal/db/migrate_auth_whitelist_test.go
git commit -m "feat(db): add auth whitelist model columns"
```

---

### Task 2: Auth Files Create/Update 白名单转换与接口回传（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Create: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: Write the failing test**

```go
func TestAuthFiles_Create_WhitelistEmptyMeansBlockAll(t *testing.T) {
	// POST /v0/admin/auth-files
	// body: content.type=claude, whitelist_enabled=true, allowed_models=[]
	// assert: 201 + row.excluded_models == universe
}

func TestAuthFiles_Update_WhitelistUnknownModelReturns400(t *testing.T) {
	// PUT /v0/admin/auth-files/:id with allowed_models=["not-exists"]
	// assert: 400 + "unknown model"
}

func TestAuthFiles_Get_ReturnsWhitelistFields(t *testing.T) {
	// GET /v0/admin/auth-files/:id
	// assert: has whitelist_enabled/allowed_models/excluded_models
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ -v`

Expected: FAIL（字段/逻辑未实现）。

**Step 3: Write minimal implementation**

```go
type createAuthFileRequest struct {
	// ...existing
	WhitelistEnabled *bool    `json:"whitelist_enabled"`
	AllowedModels    []string `json:"allowed_models"`
}
type updateAuthFileRequest struct {
	// ...existing
	WhitelistEnabled *bool     `json:"whitelist_enabled"`
	AllowedModels    *[]string `json:"allowed_models"`
}

// 保存前：
provider, err := resolveProviderFromAuthContent(content) // from content.type
universe, supported, reason := loadAuthTypePresetModels(provider)
if whitelistEnabled && !supported { return 400 }
excluded, err := buildExcludedFromWhitelist(universe, allowedModels)

auth.WhitelistEnabled = whitelistEnabled
auth.AllowedModels = marshalJSON(normalizeModelNames(allowedModels))
auth.ExcludedModels = marshalJSON(excluded)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "feat(auth-files): persist whitelist and derive excluded models on save"
```

---

### Task 3: 新增预置模型接口与权限注册（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files.go`
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Create: `internal/http/api/admin/permissions/auth_files_model_presets_permissions_test.go`
- Extend Test: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: Write the failing test**

```go
func TestAuthFiles_ModelPresets_ByType(t *testing.T) {
	// GET /v0/admin/auth-files/model-presets?type=claude
	// assert: 200 + supported=true + models not empty
}

func TestAuthFiles_ModelPresets_UnsupportedType(t *testing.T) {
	// GET ...?type=unknown
	// assert: 200 + supported=false + reason not empty
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ModelPresets -v`

Expected: FAIL（路由/处理器不存在）。

**Step 3: Write minimal implementation**

```go
// auth_files.go
func (h *AuthFileHandler) ListModelPresets(c *gin.Context) {
	typeKey := strings.TrimSpace(c.Query("type"))
	provider, err := canonicalizeAuthTypeProvider(typeKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"supported": false, "reason": "unsupported auth type", "models": []string{}})
		return
	}
	models := loadProviderUniverse(provider)
	c.JSON(http.StatusOK, gin.H{"provider": provider, "supported": len(models) > 0, "models": models})
}

// admin.go
authed.GET("/auth-files/model-presets", authFileHandler.ListModelPresets)

// permissions.go
newDefinition("GET", "/v0/admin/auth-files/model-presets", "List Auth File Model Presets", "Auth Files")
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ModelPresets -v && go test ./internal/http/api/admin/permissions -run AuthFilesModelPresets -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/auth_files_model_presets_permissions_test.go internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "feat(auth-files): add model presets endpoint and admin permission"
```

---

### Task 4: watcher 注入 excluded_models 到 runtime auth（TDD）

**Files:**
- Modify: `internal/watcher/watcher.go`
- Create: `internal/watcher/watcher_auth_whitelist_test.go`

**Step 1: Write the failing test**

```go
func TestSynthesizeAuthFromDBRow_IncludesExcludedModelsAttribute(t *testing.T) {
	payload := []byte(`{"type":"claude","email":"x@y.com"}`)
	excluded := []string{"claude-opus-4-1", "claude-3-7-sonnet"}
	a := synthesizeAuthFromDBRow("", "auth-a", payload, 0, false, time.Now(), time.Now(), excluded)
	if got := a.Attributes["excluded_models"]; got != "claude-3-7-sonnet,claude-opus-4-1" {
		t.Fatalf("unexpected excluded_models: %q", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/watcher -run TestSynthesizeAuthFromDBRow_IncludesExcludedModelsAttribute -v`

Expected: FAIL（函数签名/属性未包含）。

**Step 3: Write minimal implementation**

```go
// pollAuth Select 增加 excluded_models 列
Select("key", "content", "priority", "token_invalid", "created_at", "updated_at", "excluded_models")

// 解析 excluded_models JSON -> []string
excludedModels := decodeModelNames(row.ExcludedModels)

// synthesizeAuthFromDBRow 新增参数，并注入 attributes
if len(excludedModels) > 0 {
	attrs["excluded_models"] = strings.Join(normalizeModelNames(excludedModels), ",")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/watcher -run TestSynthesizeAuthFromDBRow_IncludesExcludedModelsAttribute -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/watcher/watcher.go internal/watcher/watcher_auth_whitelist_test.go
git commit -m "feat(watcher): propagate auth excluded models into runtime attributes"
```

---

### Task 5: Auth Files 页面白名单勾选与自动加载预置模型（TDD）

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`
- Create: `web/src/pages/admin/authFilesWhitelistConfig.test.ts`

**Step 1: Write the failing test**

```ts
import { buildAuthFileUpdatePayload, deriveWhitelistCapability } from './AuthFiles'

it('builds whitelist payload with allowed_models', () => {
  const payload = buildAuthFileUpdatePayload({
    name: 'a', key: 'k', proxyUrl: '', rateLimit: 0, priority: 0,
    whitelistEnabled: true,
    allowedModels: ['claude-sonnet-4-6'],
  })
  expect(payload.whitelist_enabled).toBe(true)
  expect(payload.allowed_models).toEqual(['claude-sonnet-4-6'])
})

it('disables whitelist when presets unsupported', () => {
  expect(deriveWhitelistCapability({ supported: false, reason: 'unsupported' }).disabled).toBe(true)
})
```

**Step 2: Run test to verify it fails**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts`

Expected: FAIL（helper 未导出或行为不符）。

**Step 3: Write minimal implementation**

```ts
// 新增 model presets 调用
await apiFetchAdmin(`/v0/admin/auth-files/model-presets?type=${encodeURIComponent(type)}`)

// 编辑弹窗新增状态
const [whitelistEnabled, setWhitelistEnabled] = useState(file.whitelist_enabled ?? false)
const [allowedModels, setAllowedModels] = useState<string[]>(file.allowed_models ?? [])

// 保存 payload
const payload = {
  name, key, proxy_url, rate_limit, priority,
  whitelist_enabled: whitelistEnabled,
  allowed_models: whitelistEnabled ? allowedModels : [],
}
```

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts`

Expected: PASS。

**Step 5: Commit**

```bash
git -C web add src/pages/admin/AuthFiles.tsx src/pages/admin/authFilesWhitelistConfig.test.ts src/locales/en.ts src/locales/zh-CN.ts
git -C web commit -m "feat(auth-files): add whitelist UI with preset model loading"
```

---

### Task 6: 回归验证与合并前检查

**Files:**
- Modify (if needed): `internal/http/api/admin/handlers/auth_files_whitelist_test.go`
- Modify (if needed): `web/src/pages/admin/authFilesWhitelistConfig.test.ts`

**Step 1: Run backend suite for impacted modules**

Run: `go test ./internal/db ./internal/http/api/admin/handlers ./internal/http/api/admin/permissions ./internal/watcher -v`

Expected: PASS。

**Step 2: Run frontend targeted tests + build**

Run: `npm --prefix web run test -- authFilesWhitelistConfig.test.ts && npm --prefix web run build`

Expected: PASS。

**Step 3: Manual acceptance checklist**

1. 打开 Auth Files 编辑弹窗，`type=claude` 自动加载预置模型。
2. 开启白名单仅勾选一个模型，保存后仅该模型可用。
3. 清空勾选并保存，认证全禁。
4. 切换到不支持类型，开关禁用并显示原因。

**Step 4: Final commit (main repo for backend + submodule pointer)**

```bash
git add internal/models/auth.go internal/db/migrate.go internal/http/api/admin/handlers/auth_files.go internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/watcher/watcher.go web
git commit -m "feat(auth-files): enforce per-auth whitelist with preset model comparison"
```

---

## 执行备注

- 严格按 `@superpowers/test-driven-development`：每个行为先红后绿。
- 不在未支持 provider 上做隐式降级；统一返回可解释错误或禁用提示。
- 每个任务独立提交，避免大包提交与回归定位困难。
