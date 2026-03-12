# Copilot 模型映射统一到框架默认机制 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 移除 GitHub Copilot 默认 alias 注入，改为仅依赖管理员面板配置的通用模型映射机制。

**Architecture:** 在 `SanitizeOAuthModelAlias` 层删除 `github-copilot` 默认注入分支，使运行时仅消费 `model_mappings -> OAuthModelAlias`。保留现有请求阶段 alias->upstream 转换逻辑，不修改白名单逻辑。通过 TDD 先锁定“Copilot 不再自动注入、手工映射仍可生效”的行为，再做最小实现。

**Tech Stack:** Go, Gin, CLIProxyAPIPlus SDK, go test

---

### Task 1: 先锁定配置层目标行为（不再自动注入 Copilot 默认 alias）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/oauth_model_alias_test.go`

**Step 1: 写失败测试（Copilot 默认不注入）**

将现有 `TestSanitizeOAuthModelAlias_InjectsDefaultGitHubCopilotAliases` 改为“**不注入**”断言，例如：

```go
func TestSanitizeOAuthModelAlias_DoesNotInjectDefaultGitHubCopilotAliases(t *testing.T) {
    cfg := &Config{
        OAuthModelAlias: map[string][]OAuthModelAlias{
            "codex": {{Name: "gpt-5", Alias: "g5"}},
        },
    }

    cfg.SanitizeOAuthModelAlias()

    if _, exists := cfg.OAuthModelAlias["github-copilot"]; exists {
        t.Fatal("expected github-copilot defaults to NOT be injected")
    }
    if len(cfg.OAuthModelAlias["codex"]) != 1 {
        t.Fatal("expected codex aliases to be preserved")
    }
}
```

**Step 2: 运行测试，确认失败**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/config -run TestSanitizeOAuthModelAlias_DoesNotInjectDefaultGitHubCopilotAliases -v`

Expected: FAIL（当前实现会自动注入 `github-copilot` 默认 alias）。

**Step 3: 提交前检查同文件相关断言**

确认以下测试语义仍成立（不改或按新语义微调）：
- `TestSanitizeOAuthModelAlias_DoesNotOverrideUserGitHubCopilotAliases`
- `TestSanitizeOAuthModelAlias_GitHubCopilotDoesNotReinjectAfterExplicitDeletion`

---

### Task 2: 做最小实现（删除 Copilot 默认注入分支）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/config.go`

**Step 1: 修改实现**

在 `SanitizeOAuthModelAlias()` 中删除以下分支：

```go
if !hasChannel("github-copilot") {
    cfg.OAuthModelAlias["github-copilot"] = defaultGitHubCopilotAliases()
}
```

保留 `kiro` 默认注入逻辑不变。

**Step 2: 运行 Task 1 测试，确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/config -run TestSanitizeOAuthModelAlias_DoesNotInjectDefaultGitHubCopilotAliases -v`

Expected: PASS。

**Step 3: 运行配置包相关测试，确认无回归**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/config -run OAuthModelAlias -v`

Expected: PASS。

---

### Task 3: 清理仅用于默认注入的 Copilot 默认别名定义

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/oauth_model_alias_defaults.go`

**Step 1: 删除未使用函数（或改为私有保留并注明未启用）**

删除：

```go
func defaultGitHubCopilotAliases() []OAuthModelAlias { ... }
```

保留 `GitHubCopilotAliasesFromModels`（若当前/未来有独立用途）或一并删除（若仓库内已确认无引用且无产品需求）。

**Step 2: 运行编译与单测，确认无死引用**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/config -v`

Expected: PASS。

---

### Task 4: 增加运行时正向保障测试（手工映射可驱动 Copilot alias）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/sdk/cliproxy/auth/oauth_model_alias_test.go`

**Step 1: 写失败测试（github-copilot alias -> upstream）**

新增测试：

```go
func TestApplyOAuthModelAlias_GitHubCopilotManualAlias(t *testing.T) {
    aliases := map[string][]internalconfig.OAuthModelAlias{
        "github-copilot": {
            {Name: "claude-opus-4.6", Alias: "claude-opus-4-6", Fork: true},
        },
    }

    mgr := NewManager(nil, nil, nil)
    mgr.SetConfig(&internalconfig.Config{})
    mgr.SetOAuthModelAlias(aliases)

    auth := &Auth{ID: "test-auth", Provider: "github-copilot"}
    got := mgr.applyOAuthModelAlias(auth, "claude-opus-4-6")
    if got != "claude-opus-4.6" {
        t.Fatalf("applyOAuthModelAlias() = %q, want %q", got, "claude-opus-4.6")
    }
}
```

**Step 2: 运行测试，确认失败或编译红（若缺少导入/细节）**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy/auth -run TestApplyOAuthModelAlias_GitHubCopilotManualAlias -v`

Expected: FAIL（先红后绿）。

**Step 3: 补齐最小实现差异（若需要）并复测**

通常无需改实现；若测试因小问题失败（构造 auth 细节、channel 判定），只做最小修正。

**Step 4: 跑 auth 包 alias 相关测试**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy/auth -run OAuthModelAlias -v`

Expected: PASS。

---

### Task 5: 端到端回归验证（仅模型映射，不涉及白名单改动）

**Files:**
- No file changes (verification only)

**Step 1: 运行关键测试集合**

Run:

```bash
go test ./third_party/CLIProxyAPIPlus/internal/config -v
go test ./third_party/CLIProxyAPIPlus/sdk/cliproxy/auth -v
```

Expected: PASS。

**Step 2: 手工验证场景（本地）**

1. 不配置 Copilot 映射：请求 `claude-opus-4-6`，预期失败。  
2. 管理员面板配置映射 `claude-opus-4.6 -> claude-opus-4-6`：同请求预期成功。  
3. 观察上游请求模型名为 `claude-opus-4.6`。

**Step 3: 更新变更说明（如果项目有 changelog 规范）**

记录“Copilot 默认 alias 注入已移除，需通过管理员面板显式配置映射”。

---

### Task 6: 提交（按小步提交）

**Step 1: 提交配置行为变更**

```bash
git add third_party/CLIProxyAPIPlus/internal/config/config.go third_party/CLIProxyAPIPlus/internal/config/oauth_model_alias_test.go third_party/CLIProxyAPIPlus/internal/config/oauth_model_alias_defaults.go
git commit -m "refactor(copilot): remove default oauth model alias injection"
```

**Step 2: 提交运行时保障测试**

```bash
git add third_party/CLIProxyAPIPlus/sdk/cliproxy/auth/oauth_model_alias_test.go
git commit -m "test(copilot): cover manual github-copilot alias resolution"
```

---

## 完成定义（DoD）

1. Copilot 默认 alias 不再自动注入。  
2. 手工配置映射后，Copilot alias 请求可成功并回写原始上游模型名。  
3. 白名单逻辑无改动且无回归。
