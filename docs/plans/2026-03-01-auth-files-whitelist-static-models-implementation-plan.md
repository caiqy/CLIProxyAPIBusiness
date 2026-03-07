# Auth Files/Provider API Keys 静态模型源切换 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Auth Files 与 Provider API Keys 的白名单模型 universe 统一切换为静态模型源，去除对运行时动态注册表的依赖。

**Architecture:** 在 `CLIProxyAPIPlus` 的 `sdk/cliproxy` 暴露 provider->静态模型列表的公共接口，作为单一真相源；`cpab` 的 `loadProviderUniverse` 统一调用该接口，从而同时覆盖 Auth Files `ListModelPresets` 与保存时 whitelist 差集计算、以及 Provider API Keys 的 whitelist 差集计算。保留现有 reason code 协议，静态列表为空仍返回 `provider_models_unavailable`。

**Tech Stack:** Go 1.26, Gin, GORM, CLIProxyAPIPlus SDK, Go test

---

### Task 1: 在 CLIProxyAPIPlus SDK 暴露静态 universe 接口（TDD）

**Files:**
- Create: `third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe.go`
- Create: `third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe_test.go`

**Step 1: 写失败测试（provider 归一化 + 稳定输出）**

```go
func TestGetStaticProviderModelUniverse_NormalizesAndSorts(t *testing.T) {
	models := GetStaticProviderModelUniverse("  ANTIGRAVITY  ")
	if len(models) == 0 {
		t.Fatal("expected non-empty antigravity static models")
	}
	if !slices.IsSorted(models) {
		t.Fatalf("models should be sorted: %v", models)
	}
	if !slices.Contains(models, "gemini-2.5-flash") {
		t.Fatalf("expected known static model in universe, got %v", models)
	}
}

func TestGetStaticProviderModelUniverse_OpenAIAliasToCodex(t *testing.T) {
	openaiModels := GetStaticProviderModelUniverse("openai-compatibility")
	codexModels := GetStaticProviderModelUniverse("codex")
	if len(openaiModels) == 0 || len(codexModels) == 0 {
		t.Fatal("expected non-empty codex/openai alias static models")
	}
	if !reflect.DeepEqual(openaiModels, codexModels) {
		t.Fatalf("openai alias mismatch: openai=%v codex=%v", openaiModels, codexModels)
	}
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy -run StaticProviderModelUniverse -v`

Expected: FAIL（`GetStaticProviderModelUniverse` 未定义）。

**Step 3: 写最小实现**

```go
func GetStaticProviderModelUniverse(provider string) []string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch key {
	case "openai", "openai-chat-completions", "openai-compatibility", "openai-chat":
		key = "codex"
	}
	infos := registry.GetStaticModelDefinitionsByChannel(key)
	if len(infos) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(infos))
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		id := strings.TrimSpace(info.ID)
		if id == "" {
			continue
		}
		if _, ok := set[id]; ok {
			continue
		}
		set[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
```

**Step 4: 运行测试，确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy -run StaticProviderModelUniverse -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe.go third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe_test.go
git commit -m "feat(cliproxy): expose static provider model universe"
```

---

### Task 2: 将 Business 侧 loadProviderUniverse 切到静态接口（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/provider_api_keys.go`
- Modify: `internal/http/api/admin/handlers/provider_api_keys_test.go`

**Step 1: 写失败测试（默认 loader 来源为静态定义）**

```go
func TestLoadProviderUniverse_UsesStaticUniverse(t *testing.T) {
	models := loadProviderUniverse("antigravity")
	if len(models) == 0 {
		t.Fatal("expected non-empty static universe for antigravity")
	}
	if !slices.Contains(models, "gemini-2.5-flash") {
		t.Fatalf("expected static model gemini-2.5-flash, got %v", models)
	}
}

func TestLoadProviderUniverse_OpenAIAlias(t *testing.T) {
	got := loadProviderUniverse("openai-compatibility")
	if len(got) == 0 {
		t.Fatal("expected non-empty static universe for openai alias")
	}
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestLoadProviderUniverse -v`

Expected: FAIL（当前仍依赖动态注册表，未启动时为空）。

**Step 3: 写最小实现**

将 `loadProviderUniverse` 改为调用 SDK 静态接口：

```go
func loadProviderUniverse(provider string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}
	models := cliproxy.GetStaticProviderModelUniverse(provider)
	return normalizeModelNames(models)
}
```

**Step 4: 运行测试，确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestLoadProviderUniverse -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/provider_api_keys.go internal/http/api/admin/handlers/provider_api_keys_test.go
git commit -m "refactor(admin): source provider universe from static model definitions"
```

---

### Task 3: 验证 Auth Files `ListModelPresets` 与保存差集语义不变（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_whitelist_test.go`

**Step 1: 写失败测试（不 stub 动态，直接验证静态可用）**

```go
func TestAuthFiles_ModelPresets_Antigravity_StaticUniverse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAuthFilesWhitelistDB(t)
	h := NewAuthFileHandler(db)
	router := gin.New()
	router.GET("/v0/admin/auth-files/model-presets", h.ListModelPresets)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/auth-files/model-presets?type=antigravity", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Supported  bool     `json:"supported"`
		ReasonCode string   `json:"reason_code"`
		Models     []string `json:"models"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Supported || resp.ReasonCode != "" || len(resp.Models) == 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ModelPresets_Antigravity_StaticUniverse -v`

Expected: FAIL（切换前依赖动态注册，结果为空或 unsupported）。

**Step 3: 复用 Task 2 改造结果（不额外改业务逻辑）**

说明：`auth_files.go` 通过同包 `providerUniverseLoader` 间接调用 `loadProviderUniverse`；Task 2 完成后此链路自动切到静态源。

**Step 4: 运行测试，确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_ModelPresets_Antigravity_StaticUniverse -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/auth_files_whitelist_test.go
git commit -m "test(auth-files): verify static presets for antigravity"
```

---

### Task 4: 覆盖 Provider API Keys 白名单差集回归（TDD）

**Files:**
- Modify: `internal/http/api/admin/handlers/provider_api_keys_test.go`

**Step 1: 写失败测试（静态 universe 下 buildExcluded 正常）**

```go
func TestBuildExcludedFromCreateWhitelist_StaticUniverseAvailable(t *testing.T) {
	models := []modelAlias{{Name: "claude-sonnet-4-6"}}
	excluded, err := buildExcludedFromCreateWhitelist("claude", models)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(excluded) == 0 {
		t.Fatal("expected excluded models computed from static universe")
	}
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestBuildExcludedFromCreateWhitelist_StaticUniverseAvailable -v`

Expected: 在切换前（动态空注册）容易 FAIL；切换后应稳定 PASS。

**Step 3: 若需要，最小补充兼容映射**

如果测试暴露某 provider alias 为空，补充 `GetStaticProviderModelUniverse` 的 alias 归一化分支（只加必要映射，避免过度设计）。

**Step 4: 运行测试，确认通过**

Run: `go test ./internal/http/api/admin/handlers -run "TestBuildExcludedFromCreateWhitelist_StaticUniverseAvailable|TestLoadProviderUniverse" -v`

Expected: PASS。

**Step 5: 提交**

```bash
git add internal/http/api/admin/handlers/provider_api_keys_test.go third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe.go third_party/CLIProxyAPIPlus/sdk/cliproxy/static_model_universe_test.go
git commit -m "test(admin): lock static whitelist universe behavior"
```

---

### Task 5: 全量回归与文档同步

**Files:**
- Modify: `docs/plans/2026-03-01-auth-files-whitelist-static-models-design.md`（如需补“已落地实现细节”）

**Step 1: 跑受影响测试集**

Run:

```bash
go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy -v && go test ./internal/http/api/admin/handlers -run "AuthFiles_ModelPresets|BuildExcludedFromCreateWhitelist|ProviderAPIKeys|LoadProviderUniverse" -v
```

Expected: PASS。

**Step 2: 做一次手工验证（可选但建议）**

1. 打开 Auth Files 编辑弹窗，选择 antigravity，确认可看到静态模型列表；
2. 在无运行时模型注册场景下再次验证列表仍可展示；
3. 保存白名单后，确认 `excluded_models` 被正确计算。

**Step 3: 提交最终收口**

```bash
git add docs/plans/2026-03-01-auth-files-whitelist-static-models-design.md
git commit -m "docs: align static whitelist universe design notes"
```

---

### 统一验收清单（完成定义）

1. `loadProviderUniverse` 不再读取运行时 `GlobalModelRegistry().GetAvailableModelsByProvider`。
2. Auth Files `ListModelPresets` 在 antigravity 等支持 provider 上可稳定返回静态模型。
3. Provider API Keys 的 whitelist 差集计算使用同一静态 universe 语义。
4. 静态列表为空时 reason code 仍为 `provider_models_unavailable`。
5. 相关测试均可稳定通过。
