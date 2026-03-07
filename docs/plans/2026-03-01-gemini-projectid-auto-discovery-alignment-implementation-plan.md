# Gemini 默认 project_id 对齐 AIClient Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 `cpab` 的 Gemini 默认 `project_id` 解析改为 auto-discovery，并仅在默认分支跳过 Cloud API 校验，同时保持显式项目分支兼容。

**Architecture:** 在 `auth_files.go` 内做最小行为调整：默认分支不再依赖 `fetchGCPProjects` 的第一个项目，而是直接复用 `performGeminiCLISetup(..., "")` 的 discovery 结果；`RequestGeminiCLIToken` 中将 Cloud API 校验收敛为“仅显式项目分支执行”。通过新增针对分支策略与默认路径的单元测试锁定行为。

**Tech Stack:** Go, Gin, oauth2, CLIProxyAPIPlus 管理端 handler 测试（`go test`）

---

### Task 1: 先用测试固定目标行为（分支策略）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`
- Create: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_policy_test.go`

**Step 1: 写失败测试（默认分支不校验、显式分支校验）**

```go
package management

import "testing"

func TestShouldVerifyCloudAPIForProjectSelection(t *testing.T) {
	if shouldVerifyCloudAPIForProjectSelection("") {
		t.Fatalf("empty project_id should skip cloud api verification")
	}
	if !shouldVerifyCloudAPIForProjectSelection("my-project") {
		t.Fatalf("explicit project_id should verify cloud api")
	}
}
```

**Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/api/handlers/management -run TestShouldVerifyCloudAPIForProjectSelection -v
```

Expected: FAIL（`shouldVerifyCloudAPIForProjectSelection` 未定义）

**Step 3: 写最小实现（新增纯函数策略）**

在 `auth_files.go` 新增：

```go
func shouldVerifyCloudAPIForProjectSelection(requestedProjectID string) bool {
	return strings.TrimSpace(requestedProjectID) != ""
}
```

**Step 4: 再跑测试确认通过**

Run:

```bash
go test ./internal/api/handlers/management -run TestShouldVerifyCloudAPIForProjectSelection -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_policy_test.go
git commit -m "test(gemini): lock cloud api verification policy by project selection"
```

---

### Task 2: 改默认项目解析为 discovery（去掉 projects[0]）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`（`ensureGeminiProjectAndOnboard`）
- Create/Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_resolution_test.go`

**Step 1: 写失败测试（默认分支应直接走 setup+discovery）**

```go
func TestEnsureGeminiProjectAndOnboard_EmptyProject_UsesDiscovery(t *testing.T) {
	// Arrange: stub setup function to simulate discovered project
	// Act: ensureGeminiProjectAndOnboard(..., "")
	// Assert: no project list fallback; storage.ProjectID == discovered project
}
```

> 实现建议：为测试可控性引入包级函数变量（如 `performGeminiCLISetupFn` / `fetchGCPProjectsFn`），测试中替换为 stub，并在 `t.Cleanup` 恢复。

**Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/api/handlers/management -run TestEnsureGeminiProjectAndOnboard_EmptyProject_UsesDiscovery -v
```

Expected: FAIL（当前默认分支仍依赖 project list）

**Step 3: 写最小实现**

在 `ensureGeminiProjectAndOnboard` 中改为：

- `requestedProject == ""` 时：
  - `storage.Auto = true`
  - 直接调用 `performGeminiCLISetup(..., "")`
  - 使用 `storage.ProjectID` 作为最终项目
- 移除默认路径下的 `fetchGCPProjects -> projects[0]` 逻辑

**Step 4: 跑目标测试确认通过**

Run:

```bash
go test ./internal/api/handlers/management -run TestEnsureGeminiProjectAndOnboard_EmptyProject_UsesDiscovery -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_resolution_test.go
git commit -m "feat(gemini): use onboarding discovery for default project resolution"
```

---

### Task 3: 在 RequestGeminiCLIToken 默认分支跳过 Cloud API 校验

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`（`RequestGeminiCLIToken`）
- Modify/Create: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_policy_test.go`

**Step 1: 写失败测试（空 project_id 不应触发校验）**

```go
func TestShouldVerifyCloudAPIForProjectSelection_EmptyProject_SkipsVerification(t *testing.T) {
	if got := shouldVerifyCloudAPIForProjectSelection(""); got {
		t.Fatalf("expected false, got true")
	}
}
```

**Step 2: 运行测试确认失败（若尚未接入调用点）**

Run:

```bash
go test ./internal/api/handlers/management -run TestShouldVerifyCloudAPIForProjectSelection_EmptyProject_SkipsVerification -v
```

Expected: FAIL（调用点尚未切换）或 PASS（若已切换，可继续下一步）

**Step 3: 写最小实现（接入调用点）**

在 `RequestGeminiCLIToken` 的普通分支中：

```go
if shouldVerifyCloudAPIForProjectSelection(requestedProjectID) {
	// existing checkCloudAPIIsEnabled logic
} else {
	ts.Checked = false
}
```

并保持：
- `ALL` 分支不变
- `GOOGLE_ONE` 分支不变
- 显式 `project_id` 分支保留校验

**Step 4: 跑本任务测试确认通过**

Run:

```bash
go test ./internal/api/handlers/management -run TestShouldVerifyCloudAPIForProjectSelection -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_project_policy_test.go
git commit -m "feat(gemini): skip cloud api verification for default discovery path"
```

---

### Task 4: 回归验证与文档同步

**Files:**
- Modify: `docs/plans/2026-03-01-gemini-projectid-auto-discovery-alignment-design.md`（如需补充“实现后差异/验证记录”）

**Step 1: 运行管理模块测试（聚焦包）**

Run:

```bash
go test ./internal/api/handlers/management -v
```

Expected: PASS

**Step 2: 运行 CLIProxyAPIPlus 关键测试集（可选但建议）**

Run:

```bash
go test ./... 
```

Expected: PASS（若耗时过长，可记录已执行范围与未执行范围）

**Step 3: 人工验收（最小 smoke）**

1. 管理端请求 `/v0/admin/tokens/gemini`（不传 `project_id`）可返回正常授权流程状态。  
2. 显式 `project_id=<id>` 时仍走 Cloud API 校验路径。  
3. 生成 token 记录含 `metadata.project_id`。

**Step 4: 最终 Commit（如前述任务未分批提交则在此合并提交）**

```bash
git add third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_*test.go docs/plans/2026-03-01-gemini-projectid-auto-discovery-alignment-design.md
git commit -m "feat(gemini): align default project discovery with AIClient behavior"
```
