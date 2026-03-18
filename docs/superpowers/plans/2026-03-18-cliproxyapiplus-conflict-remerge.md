# CLIProxyAPIPlus 冲突文件重处理 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 仅重处理 CLIProxyAPIPlus 与 `upstream/main` 合并时产生的 11 个冲突文件，优先保留上游代码，同时尽量保住本地功能，并恢复主仓库与子模块的 Go 回归测试可运行状态。

**Architecture:** 以当前子模块为基线，不重做整次 merge；按冲突文件分三类处理：上游优先、手工混合、本地增强保留。每次只处理少量文件并用最小相关测试验证，最后再跑完整 `go test ./...`。

**Tech Stack:** Git, Go 1.26, go test, CLIProxyAPIPlus 子模块, 主仓库 replace 依赖。

---

**允许的最少相邻文件例外：**
- `third_party/CLIProxyAPIPlus/internal/registry/model_definitions.go`：因为冲突文件 `model_definitions_static_data.go` 已被删除，兼容入口只能落到当前仍存在的 registry 定义文件中。
- `third_party/CLIProxyAPIPlus/internal/registry/model_definitions_static_data_test.go`：仅当现有测试断言与 upstream 当前数据来源不一致时，才允许做最小调整；否则不改。

## Chunk 1: 建立基线并锁定目标差异

### Task 1: 记录 11 个冲突文件的当前状态与上游版本

**Files:**
- Read: `third_party/CLIProxyAPIPlus/README.md`
- Read: `third_party/CLIProxyAPIPlus/README_CN.md`
- Read: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`
- Read: `third_party/CLIProxyAPIPlus/internal/registry/model_definitions_static_data.go`（若不存在则记为已删）
- Read: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor_test.go`
- Read: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
- Read: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`
- Read: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers.go`
- Read: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go`
- Read: `third_party/CLIProxyAPIPlus/internal/util/proxy.go`
- Read: `third_party/CLIProxyAPIPlus/sdk/cliproxy/service.go`

- [ ] **Step 1: 记录当前失败证据**

Run: `go test ./...`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: FAIL，至少包含 GitLab handler 缺失、`GetAntigravityModelConfig` 缺失、`internal/util/proxy_test.go` 失败。

- [ ] **Step 2: 导出上游版本用于对照**

Run: `git show upstream/main:internal/api/handlers/management/auth_files.go && git show upstream/main:internal/util/proxy.go && git show upstream/main:sdk/cliproxy/service.go`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: 能看到 3 个核心文件的 upstream 内容。

- [ ] **Step 3: 写下每个文件的处理分类**

分类结果必须落在以下三类之一：
- 上游优先：`internal/util/proxy.go`、`internal/registry/model_definitions_static_data.go`
- 手工混合：`internal/api/handlers/management/auth_files.go`、`sdk/cliproxy/service.go`
- 本地增强保留：README、Copilot executor、proxy_helpers、对应 tests


## Chunk 2: 先修“上游优先”文件

### Task 2: 处理 `internal/util/proxy.go`

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/util/proxy.go`
- Test: `third_party/CLIProxyAPIPlus/internal/util/proxy_test.go`

- [ ] **Step 1: 写失败验证命令**

Run: `go test ./internal/util -run TestSetProxy -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: FAIL，包含 `InsecureSkipVerify` 相关断言失败。

- [ ] **Step 2: 以 upstream 实现为主体，最小补回本地能力**

要求：
- 保留 `proxyutil.BuildHTTPTransport(cfg.ProxyURL)` 的上游主路径
- 若 `TLSInsecureSkipVerify` 为 true，则确保 `httpClient.Transport` 最终带有 `TLSClientConfig.InsecureSkipVerify = true`
- 若没有 proxy URL 但开启了 `TLSInsecureSkipVerify`，也要创建可用的 `*http.Transport`

- [ ] **Step 3: 重新运行目标测试**

Run: `go test ./internal/util -run TestSetProxy -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS。


### Task 3: 为已删除的 `internal/registry/model_definitions_static_data.go` 提供最小兼容补丁

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/registry/model_definitions.go`
- Test: `third_party/CLIProxyAPIPlus/internal/registry/model_definitions_static_data_test.go`

- [ ] **Step 1: 写失败验证命令**

Run: `go test ./internal/registry -run TestGetAntigravityModelConfig -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: FAIL，提示 `GetAntigravityModelConfig` 未定义。

- [ ] **Step 2: 在不恢复旧静态文件的前提下提供最小兼容**

要求：
- 不重新引入 `model_definitions_static_data.go`
- 在 `model_definitions.go` 中补一个最小兼容入口 `GetAntigravityModelConfig()`
- 返回值必须基于 upstream 现有模型定义数据构造，而不是复制一整份旧静态表

- [ ] **Step 3: 重新运行目标测试**

Run: `go test ./internal/registry -run TestGetAntigravityModelConfig -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS。如果失败，先确认实现是否缺失；只有在确认测试断言仍依赖已删除静态表、且 upstream 当前模型定义已改变语义时，才允许对测试做最小同步调整。


## Chunk 3: 再修“手工混合”文件

### Task 4: 重处理 `internal/api/handlers/management/auth_files.go`

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`
- Test: `third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files_gitlab_test.go`
- Related Read Only: `third_party/CLIProxyAPIPlus/internal/api/server.go`

- [ ] **Step 1: 写失败验证命令**

Run: `go test ./internal/api/handlers/management -run TestRequestGitLabPATToken_SavesAuthRecord -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: FAIL，提示 `RequestGitLabPATToken` 未定义。

- [ ] **Step 2: 以上游为主体重引入 GitLab handler**

要求：
- 恢复 `RequestGitLabToken`
- 恢复 `RequestGitLabPATToken`
- 保留本地仍然依赖的既有管理 handler 行为
- 不把整个文件退回纯 HEAD 版本

- [ ] **Step 3: 运行管理 handler 定向测试**

Run: `go test ./internal/api/handlers/management -run TestRequestGitLabPATToken_SavesAuthRecord -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS。

- [ ] **Step 4: 运行一个非 GitLab 的既有管理 handler 冒烟测试**

Run: `go test ./internal/api/handlers/management -run TestDeleteAuthFile_ -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS，证明这次合并没有把原有 auth file 管理行为直接破坏。


### Task 5: 重处理 `sdk/cliproxy/service.go`

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/sdk/cliproxy/service.go`
- Test: `third_party/CLIProxyAPIPlus/sdk/cliproxy/service_gitlab_models_test.go`
- Related Read Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`

- [ ] **Step 1: 写失败验证命令**

Run: `go test ./sdk/cliproxy -run Test.*GitLab.* -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: FAIL 或 build fail（若 GitLab 相关注册链仍不完整）。

- [ ] **Step 2: 以 upstream 注册逻辑为主，最小保留本地别名能力**

要求：
- 保留 upstream 的 `gitlab` provider 注册路径
- 保留本地 `applyExcludedModelsWithAlias(...)` 的语义，仅用于本地确实依赖 alias 的 provider
- 对 `antigravity` 优先采用 upstream 的 registry 模式
- 不删除本地 Copilot 相关注册路径

- [ ] **Step 3: 重新运行目标测试**

Run: `go test ./sdk/cliproxy -run Test.*GitLab.* -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS。

- [ ] **Step 4: 增补一个非 GitLab 的定向保护测试**

Run: `go test ./sdk/cliproxy -run TestApplyOAuthModelAlias_ -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS，证明 alias 路径未被这次 service.go 重处理直接破坏。


## Chunk 4: 确认“本地增强保留类”没有被误伤

### Task 6: 复核本地增强文件仍保持本地实现

**Files:**
- Verify Only: `third_party/CLIProxyAPIPlus/README.md`
- Verify Only: `third_party/CLIProxyAPIPlus/README_CN.md`
- Verify Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
- Verify Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`
- Verify Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor_test.go`
- Verify Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers.go`
- Verify Only: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go`

- [ ] **Step 1: 检查关键本地标记仍存在**

Run: `rg "ForceAgentInitiator|initiatorBypass|dualRunDiffSink|response-header-timeout|connect-timeout" README.md README_CN.md internal/runtime/executor`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: 能搜到本地增强特征。

- [ ] **Step 2: 运行与本地增强直接相关的定向测试**

Run: `go test ./internal/runtime/executor -run "Test.*(Copilot|Proxy).*" -v`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS，至少不出现因本次重处理造成的新 build fail。


## Chunk 5: 最终回归

### Task 7: 跑子模块完整回归

**Files:**
- Verify: `third_party/CLIProxyAPIPlus/...`

- [ ] **Step 1: 执行完整测试**

Run: `go test ./...`
Workdir: `third_party/CLIProxyAPIPlus`
Expected: PASS；如果失败，输出剩余失败列表并标注是否属于本次 11 个冲突文件范围。


### Task 8: 跑主仓库完整回归

**Files:**
- Verify: `./...`

- [ ] **Step 1: 执行完整测试**

Run: `go test ./...`
Workdir: `.`
Expected: PASS；若失败，优先检查是否仍由 `third_party/CLIProxyAPIPlus` 的 replace 路径引起。

- [ ] **Step 2: 核对实际改动文件边界**

Run: `git status --short third_party/CLIProxyAPIPlus`
Workdir: `.`
Expected: 只出现 11 个冲突文件，加上已声明允许的最少相邻文件例外；若超出范围，先停下并说明原因。

- [ ] **Step 3: 输出变更与验证总结**

总结必须包含：
- 11 个冲突文件里，哪些最终偏向 upstream
- 哪些保留了本地功能
- 哪些是手工混合
- 子模块完整测试结果
- 主仓库完整测试结果
