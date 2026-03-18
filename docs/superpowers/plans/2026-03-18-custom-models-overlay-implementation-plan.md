# Custom Models Overlay Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `CLIProxyAPIPlus` 增加 `<config dir>/custom_models.json` overlay 机制，使其在启动加载、startup refresh、periodic refresh 时都能对 `models.json` 基础 catalog 做按 `id` 的覆盖与增补。

**Architecture:** 在 `internal/registry` 内部引入“base catalog + final catalog”模型：base 保存最近一次成功装载的基础目录，final 保存套用 `custom_models.json` 后的可见目录。通过新增 custom 解析/merge 辅助代码和路径注入入口，统一 startup load、startup refresh、periodic refresh 三条链路；主程序在启动 updater 之前先注入 custom 路径并立即重算，避免服务早期暴露未 overlay 的目录。

**Tech Stack:** Go, Gin, embedded JSON catalog, `net/http`, `encoding/json`,现有 registry/updater/watcher/server 测试框架。

---

## 目标文件结构

### 预计新增文件

- `third_party/CLIProxyAPIPlus/internal/registry/custom_models.go`
  - 负责 `custom_models.json` 路径管理、逐段解析、容错清洗、overlay merge、摘要统计。
- `third_party/CLIProxyAPIPlus/internal/registry/custom_models_test.go`
  - 覆盖 custom 文件解析与 merge 规则单测。
- `third_party/CLIProxyAPIPlus/internal/registry/model_updater_test.go`
  - 覆盖 startup reload、refresh overlay、changed providers 语义。
- `third_party/CLIProxyAPIPlus/cmd/server/main_test.go`
  - 覆盖启动前 custom 路径接线 helper。

### 预计修改文件

- `third_party/CLIProxyAPIPlus/internal/registry/model_updater.go`
  - 把 store 从单一 final catalog 扩展为 base/final 两层；统一 load/refresh 重算；保持 codex 回调聚合语义。
- `third_party/CLIProxyAPIPlus/cmd/server/main.go`
  - 在调用 `registry.StartModelsUpdater(...)` 前设置 custom 路径并立即重算。
- `third_party/CLIProxyAPIPlus/internal/api/debug_model_route_memory.go`
  - 暴露 custom overlay 诊断信息。
- `third_party/CLIProxyAPIPlus/internal/api/server_test.go`
  - 补充 debug 接口 overlay 信息测试。
- `third_party/CLIProxyAPIPlus/internal/registry/debug_snapshot.go`
  - 或等价导出的 registry debug accessor，供 API 层读取 custom overlay 状态。

**参考规格：** `docs/superpowers/specs/2026-03-18-custom-models-overlay-design.md`

## Chunk 1: Registry overlay 基础能力

### Task 1: 建立 custom 文件解析与 merge 帮助函数

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/registry/custom_models.go`
- Test: `third_party/CLIProxyAPIPlus/internal/registry/custom_models_test.go`

- [ ] **Step 1: 写失败测试，锁定 merge 规则**

覆盖这些用例：

```go
func TestOverlayModels_ReplacesByIDKeepsPosition(t *testing.T) {}
func TestOverlayModels_AppendsNewIDsToTail(t *testing.T) {}
func TestParseCustomModels_IgnoresUnknownProviderAndBadItems(t *testing.T) {}
func TestParseCustomModels_AllowsPartialSections(t *testing.T) {}
func TestParseCustomModels_FileNotFoundFallsBack(t *testing.T) {}
func TestParseCustomModels_InvalidJSONFallsBack(t *testing.T) {}
func TestParseCustomModels_SectionTypeMismatchIgnored(t *testing.T) {}
func TestOverlayModels_AddsWholeProviderSectionWhenBaseMissing(t *testing.T) {}
```

- [ ] **Step 2: 运行测试，确认当前缺实现失败**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/registry -run "TestOverlayModels|TestParseCustomModels" -count=1`

Expected: FAIL，提示缺少 parser / overlay 实现。

- [ ] **Step 3: 实现 custom parser 与 overlay helper**

实现要点：

```go
type customOverlaySummary struct {
    Overridden int
    Added      int
}

func parseCustomModelsFile(path string) (*staticModelsJSON, map[string]customOverlaySummary, error) {}
func overlayModels(base, custom *staticModelsJSON) (*staticModelsJSON, map[string]customOverlaySummary) {}
func overlaySection(base, custom []*ModelInfo) ([]*ModelInfo, customOverlaySummary) {}
```

约束：
- custom 逐段解析，不能复用 `validateModelsCatalog()`
- 未知 provider 段只 warn，不报 fatal
- 无 `id` 项跳过
- 同 `id` 整条替换，新增项追加末尾

- [ ] **Step 4: 运行测试，确认 parser/merge 通过**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/registry -run "TestOverlayModels|TestParseCustomModels" -count=1`

Expected: PASS

- [ ] **Step 5: 若本次执行已获得用户明确允许提交，则提交本任务**

```bash
git add third_party/CLIProxyAPIPlus/internal/registry/custom_models.go third_party/CLIProxyAPIPlus/internal/registry/custom_models_test.go
git commit -m "feat: add custom models overlay helpers"
```

### Task 2: 将 updater 改造成 base/final 双目录重算链路

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/registry/model_updater.go`
- Test: `third_party/CLIProxyAPIPlus/internal/registry/model_updater_test.go`

- [ ] **Step 1: 写失败测试，锁定 updater 行为**

至少覆盖：

```go
func TestSetCustomModelsPath_ReloadsCurrentEmbeddedCatalog(t *testing.T) {}
func TestTryRefreshModels_AppliesOverlayAfterRemoteSuccess(t *testing.T) {}
func TestTryRefreshModels_ReappliesCustomOnRemoteFailureUsingLastBase(t *testing.T) {}
func TestDetectChangedProviders_UsesFinalCatalogAndKeepsCodexGrouping(t *testing.T) {}
func TestSetCustomModelsPath_RepeatedCallsReplacePreviousState(t *testing.T) {}
```

- [ ] **Step 2: 运行测试，确认旧链路失败**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/registry -run "TestSetCustomModelsPath|TestTryRefreshModels|TestDetectChangedProviders" -count=1`

Expected: FAIL，提示无 custom path/reload/base-final 逻辑。

- [ ] **Step 3: 先改 store 结构为 base/final 双层**

```go
type modelStore struct {
    mu    sync.RWMutex
    base  *staticModelsJSON
    final *staticModelsJSON
}
```

- [ ] **Step 4: 调整 init 装载 embedded 的写入语义**

要求：
- embedded 成功后初始化 base
- 立即基于当前 custom 路径（若有）生成 final
- `getModels()` 只返回 final

- [ ] **Step 5: 增加 custom 路径注入与立即重算入口**

关键修改：

```go
func SetCustomModelsPath(path string) error {}
func reloadFinalCatalogFromCurrentBase(reason string) ([]string, error) {}
```

实现要求：
- `SetCustomModelsPath()` 要立即基于当前 base 重算 final
- 重复调用要覆盖旧路径，且状态可预测

- [ ] **Step 6: 改造 startup/periodic refresh 都走统一重算流程**

```go
func tryRefreshModels(ctx context.Context, label string) {}
func getModels() *staticModelsJSON { return modelsCatalogStore.final }
```

实现要求：
- startup refresh / periodic refresh 都走“更新 base -> 读取 custom -> 生成 final -> 比较 changed providers”
- 远端失败时保留旧 base，但仍重读 custom 并重算 final
- `codex-free/team/plus/pro` 仍聚合为 `codex`

- [ ] **Step 7: 增加/验证 warning 与 overlay summary 日志**

至少覆盖：
- file not found warning
- parse failed warning
- overlay applied summary

- [ ] **Step 8: 运行 registry 全量测试**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/registry/... -count=1`

Expected: PASS

- [ ] **Step 9: 若本次执行已获得用户明确允许提交，则提交本任务**

```bash
git add third_party/CLIProxyAPIPlus/internal/registry/model_updater.go third_party/CLIProxyAPIPlus/internal/registry/model_updater_test.go third_party/CLIProxyAPIPlus/internal/registry/custom_models.go third_party/CLIProxyAPIPlus/internal/registry/custom_models_test.go
git commit -m "feat: overlay custom models onto registry catalog"
```

## Chunk 2: 启动接线与运行时可见性

### Task 3: 在服务启动前注入 custom 路径并避免早期未 overlay 暴露

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/cmd/server/main.go`
- Create: `third_party/CLIProxyAPIPlus/cmd/server/main_test.go`
- Optional reference: `third_party/CLIProxyAPIPlus/internal/config/config.go`

- [ ] **Step 1: 写失败测试或最小回归验证脚本**

先提取小函数并测试，不要直接在巨大的 `main()` 中堆逻辑：

```go
func configureCustomModelsOverlay(configFilePath string) error {}
```

并为其写测试：

```go
func TestConfigureCustomModelsOverlay_UsesConfigDir(t *testing.T) {}
```

- [ ] **Step 2: 运行测试，确认当前无法在 updater 前设置路径**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./cmd/server -run "TestConfigureCustomModelsOverlay" -count=1`

Expected: FAIL 或无此 helper。

- [ ] **Step 3: 实现启动接线**

仅在实际启动 proxy service 的路径中接线，不影响 login/import 等非 server 分支。

在 `cmd/server/main.go` 的两处启动路径中都确保顺序为：

1. 已解析 `configFilePath`
2. `registry.SetCustomModelsPath(filepath.Join(filepath.Dir(configFilePath), "custom_models.json"))`
3. `registry.StartModelsUpdater(...)`
4. 启动服务

涉及位置：
- TUI standalone 分支
- 正常 server 分支

- [ ] **Step 4: 运行相关测试**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./cmd/server ./internal/registry -count=1`

Expected: PASS

- [ ] **Step 5: 若本次执行已获得用户明确允许提交，则提交本任务**

```bash
git add third_party/CLIProxyAPIPlus/cmd/server/main.go third_party/CLIProxyAPIPlus/cmd/server/main_test.go
git commit -m "feat: load custom model overlay before updater startup"
```

### Task 4: 扩展 debug 接口，暴露 custom overlay 诊断信息

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/registry/debug_snapshot.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/api/debug_model_route_memory.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/api/server_test.go`

- [ ] **Step 1: 写失败测试，锁定 debug 返回字段**

建议新增断言：

```go
type debugConfig struct {
    CustomModels map[string]any `json:"custom_models"`
}
```

至少校验：
- custom 文件路径
- 是否检测到 custom 文件
- 是否启用了 overlay
- 指定 model 是否来自 custom overlay
- provider 级 overlay 摘要

- [ ] **Step 2: 运行测试，确认新字段当前不存在**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/api -run TestDebugModelRouteMemoryEndpoint_NoAuthAndContainsModelDiagnostics -count=1`

Expected: FAIL，缺少 custom overlay 诊断字段。

- [ ] **Step 3: 最小实现 debug 字段**

先在 registry 侧提供统一 accessor，再由 API 层消费，避免 debug 接口自行猜测文件状态。

在 `/debug/model-route-memory` 的 `config` 或新增 `catalog` 诊断块中暴露：

```go
"custom_models": gin.H{
    "path": customPath,
    "exists": exists,
    "overlay_enabled": overlayEnabled,
    "provider_summaries": summaries,
}
```

并在 `model_diagnostics[*]` 中追加类似字段：

```go
"from_custom_overlay": true,
```

如已有 per-provider 摘要，优先追加非侵入式字段，不重构现有主体返回结构。

- [ ] **Step 4: 运行 API 测试**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/api -run TestDebugModelRouteMemoryEndpoint_NoAuthAndContainsModelDiagnostics -count=1`

Expected: PASS

- [ ] **Step 5: 若本次执行已获得用户明确允许提交，则提交本任务**

```bash
git add third_party/CLIProxyAPIPlus/internal/registry/debug_snapshot.go third_party/CLIProxyAPIPlus/internal/api/debug_model_route_memory.go third_party/CLIProxyAPIPlus/internal/api/server_test.go
git commit -m "feat: expose custom model overlay diagnostics"
```

## Chunk 3: 交叉验证与收尾

### Task 5: 跑全量回归并核对关键场景

**Files:**
- Modify if needed: `third_party/CLIProxyAPIPlus/internal/registry/model_updater_test.go`
- Modify if needed: `third_party/CLIProxyAPIPlus/internal/api/server_test.go`

- [ ] **Step 1: 运行定向测试组**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./internal/registry/... ./internal/api -count=1`

Expected: PASS

- [ ] **Step 2: 运行更大范围回归**

在 `third_party/CLIProxyAPIPlus` 目录执行：

Run: `go test ./... -count=1`

Expected: PASS；如过慢，至少确认 registry、api、cmd/server 相关包全绿。

- [ ] **Step 3: 手工验证场景**

1. 准备 `config.yaml` 同目录 `custom_models.json`
2. 启动服务
3. 请求：`GET /debug/model-route-memory?models=<custom-id>`
4. 确认：
   - 模型在最终 catalog 中可见
   - debug 中可见 custom 路径/exists/overlay 状态
   - 同 `id` 覆盖时 owned_by/display_name 已替换为 custom 值

- [ ] **Step 4: 更新计划执行备注（如果实现中有偏差）**

在本计划文件或对应执行记录中补记：
- 实际落点是否与计划一致
- 是否放弃了某个可选测试
- 是否发现新的后续工作

- [ ] **Step 5: 若本次执行已获得用户明确允许提交，则提交最终集成结果**

```bash
git add third_party/CLIProxyAPIPlus/internal/registry third_party/CLIProxyAPIPlus/internal/api third_party/CLIProxyAPIPlus/cmd/server/main.go
git commit -m "feat: support config-local custom model overlays"
```

## 执行注意事项

- 不要复用 `validateModelsCatalog()` 去校验 custom 文件；那是完整 catalog 的校验器，不适合 partial overlay。
- `getModels()` 必须始终返回 final catalog，而不是 base catalog。
- `SetCustomModelsPath()` 不能只是保存路径，必须立即重算 final catalog。
- debug 接口不能直接 `os.Stat(custom_models.json)` 自行判断状态，必须消费 registry 暴露的统一调试视图。
- refresh 失败时不能丢失最近一次成功 base catalog。
- callback 的 provider 命名必须保持兼容，尤其是 codex 聚合语义。
- 仅 server/standalone 启动路径需要接线，不要污染 login/import 分支。
- 未获得用户明确授权前，不要实际执行 git commit；计划中的 commit 步骤只是执行检查点。

Plan complete and saved to `docs/superpowers/plans/2026-03-18-custom-models-overlay-implementation-plan.md`. Ready to execute?
