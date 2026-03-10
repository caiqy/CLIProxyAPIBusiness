# Copilot Claude Thinking 参数传递修复计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 修复 Copilot Claude 路径（`/v1/messages`）中推理思考参数未能完整传递给上游的 6 个问题。

**Architecture:** 所有修改集中在子模块 `third_party/CLIProxyAPIPlus/` 内，主要涉及 `github_copilot_executor.go`（executor 行为）和 `model_definitions.go`（模型元数据）两个文件，以及对应测试文件。

**Tech Stack:** Go 1.26，`tidwall/gjson`/`sjson` 操作 JSON body，`net/http` header 操作

---

## 背景：6 个问题汇总

| # | 层面 | 问题 |
|---|------|------|
| ①② | Header | `betas` body→header 提取缺失；默认 `Anthropic-Beta` 缺 thinking beta |
| ③ | Header | 用户传 `Anthropic-Beta` 时直接覆盖而非合并 |
| ④ | 请求体 | `disableThinkingIfToolChoiceForced` 缺失 |
| ⑤ | 模型元数据 | claude-4.5/4 系列无 thinking 声明，参数被主动剥除 |
| ⑥ | 模型元数据 | Claude 4.6 在 Copilot 路径只有 level，无 budget，精度丢失 |

**所有修改路径前缀：** `third_party/CLIProxyAPIPlus/`（下文省略此前缀）

**测试运行命令：**
```bash
# 在子模块目录运行
go test ./internal/runtime/executor/... -v -run TestApplyHeaders
go test ./internal/runtime/executor/... -v
go test ./internal/registry/... -v
```

---

## Task 1：更新 `applyHeaders` 签名，统一 beta 合并逻辑（Fix ①②③）

**涉及问题：**
- ① `betas` 从 body 提取后需要传给 `applyHeaders`（接口层，实际提取在 Task 2）
- ② 默认 `Anthropic-Beta` 从 `"advanced-tool-use-2025-11-20"` 扩展为包含 `interleaved-thinking-2025-05-14`
- ③ 用户传 `Anthropic-Beta` 时合并而非覆盖

**Files:**
- Modify: `internal/runtime/executor/github_copilot_executor.go`
- Modify: `internal/runtime/executor/github_copilot_executor_test.go`

---

### Step 1.1：写失败测试 —— 默认 beta 包含 thinking beta

```go
// 在 github_copilot_executor_test.go 中，替换现有的 TestApplyHeaders_MessagesAddsAnthropicBeta
func TestApplyHeaders_MessagesAddsAnthropicBeta(t *testing.T) {
    t.Parallel()
    e := &GitHubCopilotExecutor{}
    req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
    e.applyHeaders(req, "token", nil, false, true, nil)
    got := req.Header.Get("Anthropic-Beta")
    if !strings.Contains(got, "advanced-tool-use-2025-11-20") {
        t.Fatalf("Anthropic-Beta = %q, want to contain advanced-tool-use-2025-11-20", got)
    }
    if !strings.Contains(got, "interleaved-thinking-2025-05-14") {
        t.Fatalf("Anthropic-Beta = %q, want to contain interleaved-thinking-2025-05-14", got)
    }
}
```

### Step 1.2：写失败测试 —— 用户 beta 被合并而非覆盖

```go
func TestApplyHeaders_MessagesUserBetaMergedNotOverridden(t *testing.T) {
    t.Parallel()
    gin.SetMode(gin.TestMode)

    incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
    incomingReq.Header.Set("Anthropic-Beta", "my-custom-beta-2025-01-01")

    w := httptest.NewRecorder()
    ginCtx, _ := gin.CreateTestContext(w)
    ginCtx.Request = incomingReq

    outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
    outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

    e := &GitHubCopilotExecutor{}
    e.applyHeaders(outReq, "token", nil, false, true, nil)

    got := outReq.Header.Get("Anthropic-Beta")
    // 默认 beta 不能丢失
    if !strings.Contains(got, "advanced-tool-use-2025-11-20") {
        t.Fatalf("Anthropic-Beta = %q, want to retain advanced-tool-use-2025-11-20", got)
    }
    // 用户自定义 beta 必须存在
    if !strings.Contains(got, "my-custom-beta-2025-01-01") {
        t.Fatalf("Anthropic-Beta = %q, want to contain my-custom-beta-2025-01-01", got)
    }
}
```

### Step 1.3：写失败测试 —— extraBetas 被合并到 header

```go
func TestApplyHeaders_MessagesExtraBetasFromBodyMerged(t *testing.T) {
    t.Parallel()
    e := &GitHubCopilotExecutor{}
    req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
    extraBetas := []string{"files-api-2025-04-14", "advanced-tool-use-2025-11-20"} // 后者重复，应去重
    e.applyHeaders(req, "token", nil, false, true, extraBetas)
    got := req.Header.Get("Anthropic-Beta")
    // files-api beta 必须出现
    if !strings.Contains(got, "files-api-2025-04-14") {
        t.Fatalf("Anthropic-Beta = %q, want to contain files-api-2025-04-14", got)
    }
    // 重复 beta 不能导致重复值（advanced-tool-use 应只出现一次）
    count := strings.Count(got, "advanced-tool-use-2025-11-20")
    if count != 1 {
        t.Fatalf("Anthropic-Beta = %q, advanced-tool-use-2025-11-20 appears %d times, want 1", got, count)
    }
}
```

### Step 1.4：运行测试，确认失败

```bash
go test ./internal/runtime/executor/... -v -run "TestApplyHeaders_MessagesAddsAnthropicBeta|TestApplyHeaders_MessagesUserBetaMergedNotOverridden|TestApplyHeaders_MessagesExtraBetasFromBodyMerged"
```

期望：编译失败（`applyHeaders` 参数不匹配）或测试失败。

### Step 1.5：实现 —— 更新常量和 `applyHeaders` 签名与逻辑

在 `github_copilot_executor.go` 中：

**1. 更新常量（约第 47 行）：**

将：
```go
copilotAnthropicBeta = "advanced-tool-use-2025-11-20"
```
改为：
```go
copilotAnthropicBeta = "advanced-tool-use-2025-11-20,interleaved-thinking-2025-05-14"
```

**2. 更新 `applyHeaders` 签名（第 537 行）：**

将：
```go
func (e *GitHubCopilotExecutor) applyHeaders(r *http.Request, apiToken string, body []byte, stream bool, useMessages bool) {
```
改为：
```go
func (e *GitHubCopilotExecutor) applyHeaders(r *http.Request, apiToken string, body []byte, stream bool, useMessages bool, extraBetas []string) {
```

**3. 替换 `applyHeaders` 内的 beta 设置逻辑（第 557-588 行的 useMessages beta 相关代码）：**

将：
```go
	if useMessages {
		r.Header.Set("Anthropic-Beta", copilotAnthropicBeta)
	} else {
		r.Header.Del("Anthropic-Beta")
	}
```
以及后面（第 584-588 行）的：
```go
		if useMessages {
			if v := strings.TrimSpace(ginHeaders.Get("Anthropic-Beta")); v != "" {
				r.Header.Set("Anthropic-Beta", v)
			}
		}
```

整体替换为（将第二段也一并去掉，并在 `forwardHeaders` 循环之后添加统一的 beta 合并逻辑）：

```go
	if useMessages {
		baseBeta := copilotAnthropicBeta
		// Build deduplicated beta set: defaults + user header + body betas.
		betaSet := make(map[string]bool)
		for _, b := range strings.Split(baseBeta, ",") {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				betaSet[trimmed] = true
			}
		}
		if ginHeaders != nil {
			if v := strings.TrimSpace(ginHeaders.Get("Anthropic-Beta")); v != "" {
				for _, b := range strings.Split(v, ",") {
					if trimmed := strings.TrimSpace(b); trimmed != "" && !betaSet[trimmed] {
						betaSet[trimmed] = true
						baseBeta += "," + trimmed
					}
				}
			}
		}
		for _, b := range extraBetas {
			if trimmed := strings.TrimSpace(b); trimmed != "" && !betaSet[trimmed] {
				betaSet[trimmed] = true
				baseBeta += "," + trimmed
			}
		}
		r.Header.Set("Anthropic-Beta", baseBeta)
	} else {
		r.Header.Del("Anthropic-Beta")
	}
```

注意：同时删除原来 `ginHeaders != nil` 块内 `Anthropic-Beta` 覆盖的那段代码（第 584-588 行），因为已经整合进上面的合并逻辑了。

**4. 更新 `applyHeaders` 的 3 个调用点（同文件）：**

- 第 104 行（`PrepareRequest`）：`e.applyHeaders(req, apiToken, nil, false, useMessages)` → `e.applyHeaders(req, apiToken, nil, false, useMessages, nil)`
- 第 198 行（`Execute`）：`e.applyHeaders(httpReq, apiToken, body, false, useMessages)` → `e.applyHeaders(httpReq, apiToken, body, false, useMessages, nil)`（Task 2 会把 nil 改为实际 extraBetas）
- 第 336 行（`ExecuteStream`）：`e.applyHeaders(httpReq, apiToken, body, true, useMessages)` → `e.applyHeaders(httpReq, apiToken, body, true, useMessages, nil)`（Task 2 同）

### Step 1.6：更新测试文件中所有 `applyHeaders` 调用（加 `nil` 作为最后一个参数）

在 `github_copilot_executor_test.go` 中，所有 `e.applyHeaders(...)` 调用都需要在末尾添加 `, nil`。共 13 处：

| 行号（原） | 原调用 | 新调用 |
|-----------|--------|--------|
| ~356 | `e.applyHeaders(req, "token", body, false, false)` | `e.applyHeaders(req, "token", body, false, false, nil)` |
| ~367 | `e.applyHeaders(req, "token", body, false, false)` | `e.applyHeaders(req, "token", body, false, false, nil)` |
| ~378 | `e.applyHeaders(req, "token", body, false, false)` | `e.applyHeaders(req, "token", body, false, false, nil)` |
| ~389 | `e.applyHeaders(req, "token", body, false, false)` | `e.applyHeaders(req, "token", body, false, false, nil)` |
| ~401 | `e.applyHeaders(req, "token", nil, false, false)` | `e.applyHeaders(req, "token", nil, false, false, nil)` |
| ~411 | `e.applyHeaders(req, "token", nil, false, false)` | `e.applyHeaders(req, "token", nil, false, false, nil)` |
| ~421 | `e.applyHeaders(req, "token", nil, false, false)` | `e.applyHeaders(req, "token", nil, false, false, nil)` |
| ~437 | `e.applyHeaders(req, "token", nil, true, false)` | `e.applyHeaders(req, "token", nil, true, false, nil)` |
| ~447 | `e.applyHeaders(req, "token", nil, false, true)` | `e.applyHeaders(req, "token", nil, false, true, nil)` |
| ~468 | `e.applyHeaders(outReq, "token", nil, false, false)` | `e.applyHeaders(outReq, "token", nil, false, false, nil)` |
| ~489 | `e.applyHeaders(outReq, "token", nil, false, true)` | `e.applyHeaders(outReq, "token", nil, false, true, nil)` |
| ~524 | `e.applyHeaders(outReq, "token", []byte(...), false, true)` | `e.applyHeaders(outReq, "token", []byte(...), false, true, nil)` |
| ~565 | `e.applyHeaders(outReq, "token", []byte(...), false, true)` | `e.applyHeaders(outReq, "token", []byte(...), false, true, nil)` |

### Step 1.7：运行测试，确认全部通过

```bash
go test ./internal/runtime/executor/... -v -run "TestApplyHeaders"
```

期望：所有 `TestApplyHeaders_*` 测试通过，包括新加的 3 个。

### Step 1.8：提交

```bash
git add internal/runtime/executor/github_copilot_executor.go
git add internal/runtime/executor/github_copilot_executor_test.go
git commit -m "fix: copilot claude beta header — add thinking beta, merge instead of override"
```

---

## Task 2：Execute/ExecuteStream 补齐 betas 提取和 disableThinking（Fix ①④）

**涉及问题：**
- ① `extractAndRemoveBetas(body)` 在 `useMessages=true` 时提取 body 中的 `betas` 字段，传给 `applyHeaders`
- ④ `disableThinkingIfToolChoiceForced(body)` 在 `useMessages=true` 时保证 tool_choice 强制工具调用时 thinking 被禁用

**Files:**
- Modify: `internal/runtime/executor/github_copilot_executor.go`
- Modify: `internal/runtime/executor/github_copilot_executor_test.go`

---

### Step 2.1：写失败测试 —— `betas` 从 body 被提取并写入 header

```go
func TestGitHubCopilotExecute_BetasExtractedFromBodyIntoHeader(t *testing.T) {
    t.Parallel()

    var capturedHeaders http.Header
    ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
        capturedHeaders = req.Header.Clone()
        return &http.Response{
            StatusCode: http.StatusOK,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4.6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
        }, nil
    }))

    e := NewGitHubCopilotExecutor(&config.Config{})
    e.cache["gh-access"] = &cachedAPIToken{
        token:       "copilot-api-token",
        apiEndpoint: "https://api.business.githubcopilot.com",
        expiresAt:   time.Now().Add(time.Hour),
    }
    auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
    // payload 中包含 betas 字段
    payload := []byte(`{"model":"claude-opus-4.6","max_tokens":128,"messages":[{"role":"user","content":"hi"}],"betas":["files-api-2025-04-14"]}`)

    _, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
        Model:   "claude-opus-4.6",
        Payload: bytes.Clone(payload),
    }, cliproxyexecutor.Options{
        SourceFormat: sdktranslator.FromString("claude"),
    })
    if err != nil {
        t.Fatalf("Execute() error = %v", err)
    }

    betaHeader := capturedHeaders.Get("Anthropic-Beta")
    if !strings.Contains(betaHeader, "files-api-2025-04-14") {
        t.Fatalf("Anthropic-Beta = %q, want to contain files-api-2025-04-14 (extracted from body)", betaHeader)
    }
}
```

### Step 2.2：写失败测试 —— tool_choice 强制时 thinking 被禁用

```go
func TestGitHubCopilotExecute_ThinkingDisabledWhenToolChoiceForced(t *testing.T) {
    t.Parallel()

    var capturedBody []byte
    ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
        var err error
        capturedBody, err = io.ReadAll(req.Body)
        if err != nil {
            return nil, err
        }
        return &http.Response{
            StatusCode: http.StatusOK,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4.6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
        }, nil
    }))

    e := NewGitHubCopilotExecutor(&config.Config{})
    e.cache["gh-access"] = &cachedAPIToken{
        token:       "copilot-api-token",
        apiEndpoint: "https://api.business.githubcopilot.com",
        expiresAt:   time.Now().Add(time.Hour),
    }
    auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
    // thinking enabled + tool_choice forced → thinking 应被禁用
    payload := []byte(`{"model":"claude-opus-4.6","max_tokens":128,"thinking":{"type":"enabled","budget_tokens":8000},"tool_choice":{"type":"tool","name":"my_tool"},"messages":[{"role":"user","content":"hi"}]}`)

    _, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
        Model:   "claude-opus-4.6",
        Payload: bytes.Clone(payload),
    }, cliproxyexecutor.Options{
        SourceFormat: sdktranslator.FromString("claude"),
    })
    if err != nil {
        t.Fatalf("Execute() error = %v", err)
    }

    thinkingType := gjson.GetBytes(capturedBody, "thinking.type").String()
    if thinkingType != "disabled" {
        t.Fatalf("thinking.type = %q, want disabled when tool_choice forces tool use", thinkingType)
    }
}
```

### Step 2.3：运行测试，确认失败

```bash
go test ./internal/runtime/executor/... -v -run "TestGitHubCopilotExecute_BetasExtractedFromBodyIntoHeader|TestGitHubCopilotExecute_ThinkingDisabledWhenToolChoiceForced"
```

期望：两个测试失败。

### Step 2.4：实现 —— 在 Execute 和 ExecuteStream 中补齐两个步骤

**在 Execute 方法中（`useMessages=true` 的路径，约第 180-198 行之间）：**

在 `applyPayloadConfigWithRoot` 之后、`body, _ = sjson.SetBytes(body, "stream", false)` 之前，插入：

```go
	// For Claude /v1/messages: extract betas from body into header, and enforce thinking constraints.
	var extraBetas []string
	if useMessages {
		extraBetas, body = extractAndRemoveBetas(body)
		body = disableThinkingIfToolChoiceForced(body)
	}
```

然后将 `applyHeaders` 调用（第 198 行）从：
```go
	e.applyHeaders(httpReq, apiToken, body, false, useMessages, nil)
```
改为：
```go
	e.applyHeaders(httpReq, apiToken, body, false, useMessages, extraBetas)
```

注意：`sjson.SetBytes(body, "stream", false)` 在上面之后执行没问题，betas 已提取完毕，stream 字段不影响。实际上这两行可以任意顺序，都在创建 httpReq 之前就行。

**在 ExecuteStream 方法中（约第 318-336 行之间），做同样的修改：**

在 `applyPayloadConfigWithRoot` 之后、`body, _ = sjson.SetBytes(body, "stream", true)` 之前，插入：

```go
	// For Claude /v1/messages: extract betas from body into header, and enforce thinking constraints.
	var extraBetas []string
	if useMessages {
		extraBetas, body = extractAndRemoveBetas(body)
		body = disableThinkingIfToolChoiceForced(body)
	}
```

然后将 `applyHeaders` 调用改为：
```go
	e.applyHeaders(httpReq, apiToken, body, true, useMessages, extraBetas)
```

### Step 2.5：运行测试，确认通过

```bash
go test ./internal/runtime/executor/... -v -run "TestGitHubCopilotExecute_BetasExtractedFromBodyIntoHeader|TestGitHubCopilotExecute_ThinkingDisabledWhenToolChoiceForced"
```

期望：两个新测试通过，原有测试不回归。

### Step 2.6：运行全部 executor 测试

```bash
go test ./internal/runtime/executor/... -v
```

期望：全部通过。

### Step 2.7：提交

```bash
git add internal/runtime/executor/github_copilot_executor.go
git add internal/runtime/executor/github_copilot_executor_test.go
git commit -m "fix: copilot claude — extract body betas to header, disable thinking on forced tool_choice"
```

---

## Task 3：模型元数据 —— claude-4.5/4 补充 thinking 声明（Fix ⑤）

**涉及问题：** claude-sonnet-4.5、claude-opus-4.5、claude-haiku-4.5、claude-sonnet-4 在注册表中无 `Thinking` 声明，导致 `applyThinkingWithUsageMeta` 将 thinking 配置主动剥除。按 Copilot 模型的统一 level-based 模式，为这些模型添加 `ThinkingLevels: ["low", "medium", "high"]`。

**Files:**
- Modify: `internal/registry/model_definitions.go`
- Modify: `internal/registry/model_definitions_static_data_test.go`

---

### Step 3.1：写失败测试

```go
func TestGetGitHubCopilotModels_ClaudeSonnet45SupportsThinking(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-sonnet-4.5" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-sonnet-4.5 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-sonnet-4.5 should support thinking")
    }
    if len(target.Thinking.Levels) == 0 {
        t.Fatal("claude-sonnet-4.5 thinking levels should not be empty")
    }
}

func TestGetGitHubCopilotModels_ClaudeOpus45SupportsThinking(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-opus-4.5" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-opus-4.5 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-opus-4.5 should support thinking")
    }
}

func TestGetGitHubCopilotModels_ClaudeHaiku45SupportsThinking(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-haiku-4.5" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-haiku-4.5 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-haiku-4.5 should support thinking")
    }
}

func TestGetGitHubCopilotModels_ClaudeSonnet4SupportsThinking(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-sonnet-4" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-sonnet-4 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-sonnet-4 should support thinking")
    }
}
```

### Step 3.2：运行测试，确认失败

```bash
go test ./internal/registry/... -v -run "TestGetGitHubCopilotModels_Claude.*SupportsThinking"
```

期望：4 个测试失败（`Thinking == nil`）。

### Step 3.3：实现 —— 为 4 个模型添加 ThinkingLevels

在 `model_definitions.go` 中，找到 `defs := []copilotModelDef{...}` 的 claude-sonnet-4、claude-sonnet-4.5、claude-opus-4.5、claude-haiku-4.5 的定义行，添加 `ThinkingLevels`:

将：
```go
		{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", Description: "Anthropic Claude Sonnet 4 via GitHub Copilot", ContextLength: 216000, MaxCompletionTokens: 16000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}},
		{ID: "claude-sonnet-4.5", DisplayName: "Claude Sonnet 4.5", Description: "Anthropic Claude Sonnet 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}},
		{ID: "claude-opus-4.5", DisplayName: "Claude Opus 4.5", Description: "Anthropic Claude Opus 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}},
		{ID: "claude-haiku-4.5", DisplayName: "Claude Haiku 4.5", Description: "Anthropic Claude Haiku 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}},
```
改为：
```go
		{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", Description: "Anthropic Claude Sonnet 4 via GitHub Copilot", ContextLength: 216000, MaxCompletionTokens: 16000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "claude-sonnet-4.5", DisplayName: "Claude Sonnet 4.5", Description: "Anthropic Claude Sonnet 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "claude-opus-4.5", DisplayName: "Claude Opus 4.5", Description: "Anthropic Claude Opus 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "claude-haiku-4.5", DisplayName: "Claude Haiku 4.5", Description: "Anthropic Claude Haiku 4.5 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}},
```

### Step 3.4：运行测试，确认通过

```bash
go test ./internal/registry/... -v -run "TestGetGitHubCopilotModels_Claude.*SupportsThinking"
```

期望：4 个测试通过。

### Step 3.5：运行全部 registry 测试

```bash
go test ./internal/registry/... -v
```

期望：全部通过，无回归。

### Step 3.6：提交

```bash
git add internal/registry/model_definitions.go
git add internal/registry/model_definitions_static_data_test.go
git commit -m "fix: copilot claude 4.5/4 models — declare thinking level support"
```

---

## Task 4：模型元数据 —— Claude 4.6 升为 Hybrid，支持 budget（Fix ⑥）

**涉及问题：** Copilot claude-opus-4.6 / claude-sonnet-4.6 目前只有 `Thinking.Levels`（`CapabilityLevelOnly`），客户端传 `budget_tokens` 时会被转成 level 导致精度丢失。将这两个模型升为 `CapabilityHybrid`（同时支持 Min/Max budget 和 Levels）。

**Files:**
- Modify: `internal/registry/model_definitions.go`
- Modify: `internal/registry/model_definitions_static_data_test.go`

---

### Step 4.1：写失败测试

```go
func TestGetGitHubCopilotModels_ClaudeOpus46IsHybridCapability(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-opus-4.6" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-opus-4.6 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-opus-4.6 should support thinking")
    }
    if len(target.Thinking.Levels) == 0 {
        t.Fatal("claude-opus-4.6 should have thinking levels (adaptive)")
    }
    if target.Thinking.Max == 0 {
        t.Fatal("claude-opus-4.6 should have thinking budget max (hybrid)")
    }
    if target.Thinking.Min == 0 {
        t.Fatal("claude-opus-4.6 should have thinking budget min (hybrid)")
    }
}

func TestGetGitHubCopilotModels_ClaudeSonnet46IsHybridCapability(t *testing.T) {
    models := GetGitHubCopilotModels()
    var target *ModelInfo
    for _, m := range models {
        if m != nil && m.ID == "claude-sonnet-4.6" {
            target = m
            break
        }
    }
    if target == nil {
        t.Fatal("claude-sonnet-4.6 not found in copilot models")
    }
    if target.Thinking == nil {
        t.Fatal("claude-sonnet-4.6 should support thinking")
    }
    if len(target.Thinking.Levels) == 0 {
        t.Fatal("claude-sonnet-4.6 should have thinking levels (adaptive)")
    }
    if target.Thinking.Max == 0 {
        t.Fatal("claude-sonnet-4.6 should have thinking budget max (hybrid)")
    }
}
```

### Step 4.2：运行测试，确认失败

```bash
go test ./internal/registry/... -v -run "TestGetGitHubCopilotModels_Claude.*46IsHybridCapability"
```

期望：2 个测试失败（`Thinking.Max == 0`）。

### Step 4.3：实现 —— 扩展 `copilotModelDef` 结构体并更新 Claude 4.6 定义

**1. 在 `model_definitions.go` 中扩展 `copilotModelDef` 结构体（在 `ThinkingLevels` 字段后添加）：**

将：
```go
	type copilotModelDef struct {
		ID                  string
		DisplayName         string
		Description         string
		ContextLength       int
		MaxCompletionTokens int
		SupportedEndpoints  []string
		ThinkingLevels      []string
	}
```
改为：
```go
	type copilotModelDef struct {
		ID                  string
		DisplayName         string
		Description         string
		ContextLength       int
		MaxCompletionTokens int
		SupportedEndpoints  []string
		ThinkingLevels      []string
		// ThinkingMinBudget/ThinkingMaxBudget enable budget-based (or hybrid) thinking.
		// When both ThinkingLevels and ThinkingMin/MaxBudget are set, the model is CapabilityHybrid.
		ThinkingMinBudget   int
		ThinkingMaxBudget   int
		ThinkingZeroAllowed bool
		ThinkingDynamic     bool
	}
```

**2. 更新注册循环，将 budget 字段也写入 `ThinkingSupport`：**

将：
```go
		if len(def.ThinkingLevels) > 0 {
			m.Thinking = &ThinkingSupport{Levels: append([]string(nil), def.ThinkingLevels...)}
		}
```
改为：
```go
		hasBudget := def.ThinkingMinBudget > 0 || def.ThinkingMaxBudget > 0
		if len(def.ThinkingLevels) > 0 || hasBudget {
			m.Thinking = &ThinkingSupport{
				Levels:         append([]string(nil), def.ThinkingLevels...),
				Min:            def.ThinkingMinBudget,
				Max:            def.ThinkingMaxBudget,
				ZeroAllowed:    def.ThinkingZeroAllowed,
				DynamicAllowed: def.ThinkingDynamic,
			}
		}
```

**3. 更新 claude-opus-4.6 和 claude-sonnet-4.6 的定义，添加 budget 字段：**

将：
```go
		{ID: "claude-opus-4.6", DisplayName: "Claude Opus 4.6", Description: "Anthropic Claude Opus 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/v1/messages", "/chat/completions"}, ThinkingLevels: []string{"low", "medium", "high"}},
		{ID: "claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", Description: "Anthropic Claude Sonnet 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}},
```
改为：
```go
		{ID: "claude-opus-4.6", DisplayName: "Claude Opus 4.6", Description: "Anthropic Claude Opus 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 64000, SupportedEndpoints: []string{"/v1/messages", "/chat/completions"}, ThinkingLevels: []string{"low", "medium", "high"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 64000, ThinkingZeroAllowed: true, ThinkingDynamic: true},
		{ID: "claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", Description: "Anthropic Claude Sonnet 4.6 via GitHub Copilot", ContextLength: 200000, MaxCompletionTokens: 32000, SupportedEndpoints: []string{"/chat/completions", "/v1/messages"}, ThinkingLevels: []string{"low", "medium", "high"}, ThinkingMinBudget: 1024, ThinkingMaxBudget: 32000, ThinkingZeroAllowed: true, ThinkingDynamic: true},
```

### Step 4.4：运行测试，确认通过

```bash
go test ./internal/registry/... -v -run "TestGetGitHubCopilotModels_Claude.*46IsHybridCapability"
```

期望：2 个测试通过。

### Step 4.5：运行全部 registry 和 thinking 测试

```bash
go test ./internal/registry/... ./internal/thinking/... -v
```

期望：全部通过，无回归。

### Step 4.6：提交

```bash
git add internal/registry/model_definitions.go
git add internal/registry/model_definitions_static_data_test.go
git commit -m "fix: copilot claude 4.6 — hybrid thinking (budget + level) support"
```

---

## Task 5：全量回归验证

### Step 5.1：在子模块运行全量测试

```bash
go test ./... 2>&1 | tail -30
```

期望：`ok` 开头的行对应所有包，无 `FAIL`。

### Step 5.2：确认修改文件清单

```bash
git diff --stat HEAD~4
```

期望：只有以下 4 个文件变动：
- `internal/runtime/executor/github_copilot_executor.go`
- `internal/runtime/executor/github_copilot_executor_test.go`
- `internal/registry/model_definitions.go`
- `internal/registry/model_definitions_static_data_test.go`

---

## 变更摘要

| Task | Fix # | 修改文件 | 核心改动 |
|------|-------|---------|---------|
| 1 | ②③ | `github_copilot_executor.go` | 常量加 thinking beta；`applyHeaders` 加 `extraBetas` 参数，改 override 为 merge |
| 1 | ②③ | `github_copilot_executor_test.go` | 所有 `applyHeaders` 调用加 `nil`；更新 beta 期望值；新增 3 个测试 |
| 2 | ①④ | `github_copilot_executor.go` | Execute/ExecuteStream 中加 `extractAndRemoveBetas` + `disableThinkingIfToolChoiceForced` |
| 2 | ①④ | `github_copilot_executor_test.go` | 新增 2 个集成测试 |
| 3 | ⑤ | `model_definitions.go` | claude-4.5/4 四个模型加 `ThinkingLevels` |
| 3 | ⑤ | `model_definitions_static_data_test.go` | 新增 4 个 thinking 声明测试 |
| 4 | ⑥ | `model_definitions.go` | 扩展 `copilotModelDef` 结构体；claude-4.6 两个模型加 budget 字段（Hybrid） |
| 4 | ⑥ | `model_definitions_static_data_test.go` | 新增 2 个 Hybrid capability 测试 |
