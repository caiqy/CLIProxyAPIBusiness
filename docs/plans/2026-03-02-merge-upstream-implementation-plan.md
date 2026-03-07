# CLIProxyAPIPlus 冲突解决实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 解决 CLIProxyAPIPlus 子模块中的合并冲突，确保动态回调特性与上游架构改进共存。

**Architecture:** 采用“提取-注入-调用”模式。提取本地 Query 参数，注入上游 Context，调用更新后的认证器接口。

**Tech Stack:** Go, Gin, Git

---

### Task 1: 解决 Codex 认证器签名冲突

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/auth/codex/openai_auth.go`

**Step 1: 统一函数签名**
将 `ExchangeCodeForTokensWithRedirect` 修改为上游版本：
```go
func (o *CodexAuth) ExchangeCodeForTokensWithRedirect(ctx context.Context, code, redirectURI string, pkceCodes *PKCECodes) (*CodexAuthBundle, error) {
	if pkceCodes == nil {
		return nil, fmt.Errorf("PKCE codes are required for token exchange")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, fmt.Errorf("redirect URI is required for token exchange")
	}
    // ... 保持后续逻辑
```

**Step 2: 验证编译**
Run: `go build ./third_party/CLIProxyAPIPlus/internal/auth/codex/...`

**Step 3: 提交修改**
Run: `git add third_party/CLIProxyAPIPlus/internal/auth/codex/openai_auth.go && git commit -m "fix(codex): align ExchangeCodeForTokensWithRedirect signature with upstream"`

---

### Task 2: 解决 auth_files.go 中的 OAuth 冲突

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`

**Step 1: 合并 RequestAnthropicToken**
保留 `callbackHost` 逻辑，注入 `PopulateAuthContext`。

**Step 2: 合并 RequestGeminiCLIToken**
同上。

**Step 3: 合并 RequestCodexToken**
调整 `ExchangeCodeForTokensWithRedirect` 调用参数顺序。

**Step 4: 合并 RequestAntigravityToken**
注入 `PopulateAuthContext`。

**Step 5: 验证编译**
Run: `go build ./third_party/CLIProxyAPIPlus/internal/api/handlers/management/...`

**Step 6: 提交修改**
Run: `git add third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go && git commit -m "feat(api): integrate PopulateAuthContext while preserving dynamic callback host"`

---

### Task 3: 解决静态数据冲突

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/registry/model_definitions_static_data.go`

**Step 1: 接受上游定义**
移除冲突标记，保留上游新增的模型。

**Step 2: 验证编译**
Run: `go build ./third_party/CLIProxyAPIPlus/internal/registry/...`

**Step 3: 提交修改**
Run: `git add third_party/CLIProxyAPIPlus/internal/registry/model_definitions_static_data.go && git commit -m "fix(registry): resolve model definitions conflicts with upstream"`

---

### Task 4: 修复测试用例冲突

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/server_test.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/auth/kiro/aws_test.go`

**Step 1: 修复 server_test.go**
调整 Mock 期望值以匹配新的 `PopulateAuthContext` 行为。

**Step 2: 修复 aws_test.go**
解决 AWS 认证测试中的冲突。

**Step 3: 运行测试**
Run: `go test ./third_party/CLIProxyAPIPlus/internal/...`

**Step 4: 提交修改**
Run: `git add . && git commit -m "test: fix unit tests after upstream merge"`

---

### Task 5: 完成子模块合并并更新主仓库

**Step 1: 结束子模块合并**
Run: `cd third_party/CLIProxyAPIPlus && git commit` (如果还有未完成的合并状态)

**Step 2: 更新主仓库引用**
Run: `git add third_party/CLIProxyAPIPlus && git commit -m "chore(submodule): resolve conflicts and update CLIProxyAPIPlus to latest upstream"`
