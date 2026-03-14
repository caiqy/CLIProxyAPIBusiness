# Copilot Header 治理 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 GitHub Copilot 上游 header 统一改为声明式可审计来源，落地 `legacy / dual-run / strict` 三模式，并严格满足 Session/Interaction 规则。

**Architecture:** 引入独立 `Header Compiler`（纯函数规则）与 `Session State Manager`（持久化 pair 状态）。`executor` 只负责模式编排：legacy 保持旧行为、dual-run 旁路计算 + shadow 存储 + diff 观测、strict 仅使用编译器结果。默认禁止透传入站 header。

**Tech Stack:** Go 1.26, gin, github.com/google/uuid, os file locking + atomic rename, logrus

---

## File Structure

- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go`
  - HeaderSpec、来源优先级、固定值写入、禁止透传。
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go`
  - 覆盖来源矩阵、UUIDv7、`X-Agent-Task-Id=X-Request-Id`、固定值头、缺值省略。
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go`
  - `Vscode-Sessionid` / `X-Interaction-Id` pair 生成与持久化。
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go`
  - 覆盖 user 生成、agent 复用、agent 无状态生成、锁超时降级、并发一致性。
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
  - 模式切换与 dual-run guard（禁止写主状态）。
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`
  - 覆盖 strict 禁透传、dual-run diff、messages/responses 路径、`X-Initiator` 回归。
- Create: `internal/copilotgate/evaluator.go`
  - dual-run 7 天门禁评估器（关键 diff 与未声明透传计数）。
- Create: `internal/copilotgate/evaluator_test.go`
  - 门禁规则与 diff 契约单测。
- Create: `internal/copilotgate/testdata/copilot_dualrun_7day_pass.json`
  - 7天窗口达标样例。
- Create: `internal/copilotgate/testdata/copilot_dualrun_7day_fail.json`
  - 7天窗口不达标样例。
- Modify: `third_party/CLIProxyAPIPlus/internal/config/config.go`
  - 增加 `github-copilot.header-policy` 配置结构和 sanitize。
- Create: `third_party/CLIProxyAPIPlus/internal/config/github_copilot_config_test.go`
  - 覆盖默认值、非法值回退、头字段配置解析。
- Modify: `config.example.yaml`
  - 增加 header-policy 示例（含 state/shadow 路径与说明）。
- Modify: `cmd/business/main.go`
  - 增加 `cpab gate copilot-dualrun --report <file>` 命令入口。
- Create: `cmd/business/main_test.go`
  - 覆盖门禁命令退出码（0/2/1）。
- Reference: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass.go`
  - 复用 identity 分桶语义、持久化写盘风格。

## Chunk 1: 配置与 Header Compiler 基础

### Task 1: 新增 header-policy 配置并做默认值保护

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/config/github_copilot_config_test.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/config/config.go`
- Modify: `config.example.yaml`

- [ ] **Step 1: 写 mode 默认值失败测试**

新增用例：`TestGitHubCopilotHeaderPolicy_DefaultModeIsLegacy`。

- [ ] **Step 2: 运行单测确认失败**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_DefaultModeIsLegacy -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL（字段或默认逻辑不存在）。

- [ ] **Step 3: 实现最小配置结构与默认值逻辑**

新增：

```go
type GitHubCopilotHeaderPolicyConfig struct {
    Mode string `yaml:"mode" json:"mode"`
    UserAgent string `yaml:"user-agent" json:"user-agent"`
    EditorVersion string `yaml:"editor-version" json:"editor-version"`
    EditorPluginVersion string `yaml:"editor-plugin-version" json:"editor-plugin-version"`
    AnthropicBeta string `yaml:"anthropic-beta" json:"anthropic-beta"`
    SessionStateFile string `yaml:"session-state-file" json:"session-state-file"`
    ShadowStateFile string `yaml:"shadow-state-file" json:"shadow-state-file"`
}
```

此步骤只做“结构声明 + 默认 mode=legacy”，不实现非法值回退。

- [ ] **Step 4: 运行单测确认通过**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_DefaultModeIsLegacy -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。


- [ ] **Step 5: 写非法 mode 回退失败测试**

新增：`TestGitHubCopilotHeaderPolicy_InvalidModeFallsBackToLegacy`。

- [ ] **Step 6: 运行非法 mode 测试确认失败**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_InvalidModeFallsBackToLegacy -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 7: 实现非法 mode 回退逻辑**

只实现 mode sanitize 回退，不做其他改动。

- [ ] **Step 8: 运行非法 mode 测试确认通过**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_InvalidModeFallsBackToLegacy -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 9: 写头字段解析失败测试**

新增：`TestGitHubCopilotHeaderPolicy_ParsesHeaderDefaults`，断言 `user-agent/editor-version/editor-plugin-version/anthropic-beta` 解析正确。

- [ ] **Step 10: 运行头字段解析测试确认失败**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_ParsesHeaderDefaults -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 11: 实现头字段解析**

- [ ] **Step 12: 运行头字段解析测试确认通过**

Run: `go test ./internal/config -run TestGitHubCopilotHeaderPolicy_ParsesHeaderDefaults -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 13: 更新 `config.example.yaml` 示例**

更新片段：

```yaml
# github-copilot:
#   header-policy:
#     mode: strict # legacy / dual-run / strict
#     user-agent: GitHubCopilotChat/0.39.0
#     editor-version: vscode/1.111.0
#     editor-plugin-version: copilot-chat/0.39.0
#     anthropic-beta: advanced-tool-use-2025-11-20
#     session-state-file: /CLIProxyAPIBusiness/data/copilot_session_state.json
#     shadow-state-file: /CLIProxyAPIBusiness/data/copilot_session_state.shadow.json
```

- [ ] **Step 14: 运行配置包测试确认通过**

Run: `go test ./internal/config -run GitHubCopilotHeaderPolicy -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 15: 提交配置改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/config/config.go third_party/CLIProxyAPIPlus/internal/config/github_copilot_config_test.go config.example.yaml
git commit -m "feat(copilot): add header policy config and defaults"
```

### Task 2: 建立 Header Compiler 的来源判定（严格 TDD）

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go`
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go`

- [ ] **Step 1: 只写 auth metadata 映射失败测试**

新增：`TestHeaderCompiler_MapsAuthMetadataContextHeaders`。

- [ ] **Step 2: 运行该测试确认失败**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_MapsAuthMetadataContextHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL（`CompileHeaders` 未定义）。

- [ ] **Step 3: 仅实现 metadata 映射最小逻辑**

只实现：
- `Editor-Device-Id <- auth.Metadata[editor_device_id]`
- `Vscode-Abexpcontext <- auth.Metadata[vscode_abexpcontext]`
- `Vscode-Machineid <- auth.Metadata[vscode_machineid]`
- 缺失时省略，禁止空字符串写入。

- [ ] **Step 4: 运行该测试确认通过**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_MapsAuthMetadataContextHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 写 request-id / task-id 失败测试**

新增：
- `TestHeaderCompiler_GeneratesUUIDv7RequestID`
- `TestHeaderCompiler_TaskIDEqualsRequestID`

- [ ] **Step 6: 运行 request-id / task-id 测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_GeneratesUUIDv7RequestID|TestHeaderCompiler_TaskIDEqualsRequestID" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 7: 实现 request-id / task-id 规则**

实现：
- `X-Request-Id` 使用 UUIDv7
- `X-Agent-Task-Id` 直接复制 `X-Request-Id`

- [ ] **Step 8: 运行 request-id / task-id 测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_GeneratesUUIDv7RequestID|TestHeaderCompiler_TaskIDEqualsRequestID" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 9: 写固定值头失败测试**

新增：`TestHeaderCompiler_SetsFixedHeaders`，必须覆盖：
- `X-Interaction-Type=conversation-agent`
- `X-Vscode-User-Agent-Library-Version=electron-fetch`
- `Sec-Fetch-Site/Mode/Dest`
- `Priority=u=4, i`
- `Accept-Encoding=gzip, deflate, br, zstd`

- [ ] **Step 10: 运行固定值头测试确认失败**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_SetsFixedHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 11: 实现固定值头写入**

只实现固定头，不改禁透传逻辑。

- [ ] **Step 12: 运行固定值头测试确认通过**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_SetsFixedHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 13: 写“禁透传”失败测试**

新增：`TestHeaderCompiler_StrictIgnoresIncomingHeaders`，incoming 提供同名头，不得覆盖编译结果。

- [ ] **Step 14: 运行禁透传测试确认失败**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_StrictIgnoresIncomingHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 15: 实现优先级与禁透传**

本步骤仅实现 strict 模式下 `incoming(disabled)`（禁透传），不实现完整优先级矩阵。
严禁在此步骤新增 `resolveHeaderValue` 通用优先级逻辑。

- [ ] **Step 16: 运行禁透传测试确认通过**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_StrictIgnoresIncomingHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 17: 运行 HeaderCompiler 全测试确认通过**

Run: `go test ./internal/runtime/executor -run HeaderCompiler -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 18: 提交 HeaderCompiler 改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go
git commit -m "feat(copilot): implement deterministic header compiler rules"
```

### Task 2B: 补齐配置来源头与优先级矩阵验证

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go`

- [ ] **Step 1: 写配置来源头失败测试**

新增：
- `TestHeaderCompiler_ConfigHeaders_WhenConfigured`
- `TestHeaderCompiler_ConfigHeaders_FallbackWhenMissing`

覆盖头：`User-Agent`、`Editor-Version`、`Editor-Plugin-Version`、`Anthropic-Beta`。

- [ ] **Step 2: 运行配置来源头测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_ConfigHeaders_WhenConfigured|TestHeaderCompiler_ConfigHeaders_FallbackWhenMissing" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 实现配置来源头与缺失回退**

规则：
- 配置存在：优先使用配置值
- 配置缺失：回退到 constant 默认值
- 配置为空字符串：按缺失处理（不发送空字符串）

- [ ] **Step 4: 运行配置来源头测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_ConfigHeaders_WhenConfigured|TestHeaderCompiler_ConfigHeaders_FallbackWhenMissing" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 写优先级矩阵失败测试**

新增：`TestHeaderCompiler_PrecedenceMatrix`，显式覆盖：
- `resolveHeaderValue` 的通用优先级：`computed > auth_metadata > config > constant > incoming(disabled)`（使用同名 synthetic header 测试）
- 真实头 `User-Agent`：`config > constant`
- 真实头 `Editor-Device-Id`：`auth_metadata` 生效且 incoming 不可覆盖
- 表驱动“分层剥离”链路：`all-present -> remove computed -> remove auth -> remove config -> remove constant -> incoming-disabled`
- 每层使用唯一 sentinel 值，并断言 `resolved source + resolved value`

- [ ] **Step 6: 列出矩阵测试名称，确认测试已注册**

Run: `go test ./internal/runtime/executor -list '^TestHeaderCompiler_PrecedenceMatrix$'`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: 输出包含 `TestHeaderCompiler_PrecedenceMatrix`。

- [ ] **Step 7: 运行优先级矩阵测试确认失败**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_PrecedenceMatrix -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 8: 实现优先级矩阵**

- [ ] **Step 9: 运行优先级矩阵测试确认通过**

Run: `go test ./internal/runtime/executor -run TestHeaderCompiler_PrecedenceMatrix -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 10: 提交 2B 改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go
git commit -m "test(copilot): cover config header fallback and precedence matrix"
```

## Chunk 2: Session/Interaction 状态机

### Task 3: 先实现 user 路径（生成 + 持久化）

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go`
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go`

- [ ] **Step 1: 写 user 生成失败测试**

新增：`TestCopilotSessionState_UserGeneratesAndPersistsPair`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_UserGeneratesAndPersistsPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 实现最小 user 生成逻辑**

要求：
- 同时生成 `session_id` 与 `interaction_id`（同策略不同值）
- 写盘采用 `temp + rename`

- [ ] **Step 4: 运行 user 生成测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_UserGeneratesAndPersistsPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 提交 user 路径改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go
git commit -m "feat(copilot): add session state manager user path"
```

### Task 4: 实现 agent 复用与无状态立即生成

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go`

- [ ] **Step 1: 写 agent 复用失败测试**

新增：`TestCopilotSessionState_AgentReusesExistingPair`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_AgentReusesExistingPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 实现 agent 复用逻辑**

- [ ] **Step 4: 运行 agent 复用测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_AgentReusesExistingPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 写“agent 无状态立即生成”失败测试**

新增：`TestCopilotSessionState_AgentWithoutStateGeneratesAndPersists`。

- [ ] **Step 6: 运行无状态测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_AgentWithoutStateGeneratesAndPersists -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 7: 实现无状态立即生成分支**

- [ ] **Step 8: 运行无状态测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_AgentWithoutStateGeneratesAndPersists -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 9: 提交 agent 路径改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go
git commit -m "feat(copilot): add session state agent reuse and fallback generation"
```

### Task 5: 锁与降级语义（主/shadow 隔离）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go`

- [ ] **Step 1: 写 lock timeout 降级失败测试**

新增：`TestCopilotSessionState_LockTimeoutFallsBackToFreshPair`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_LockTimeoutFallsBackToFreshPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 实现 sidecar lock + lock-timeout 降级**

实现要点：
- lock 文件按 bucket 维度
- 主状态与 shadow 状态不同路径、不同 lock 路径
- 锁超时时直接一次性生成 pair

- [ ] **Step 4: 运行 lock-timeout 降级测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_LockTimeoutFallsBackToFreshPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 写“写盘失败仍返回本次 pair”失败测试**

新增：`TestCopilotSessionState_PersistFailureStillReturnsGeneratedPair`。

- [ ] **Step 6: 写“写盘失败时 pair 仍同发/同省略”失败测试**

新增：`TestCopilotSessionState_PersistFailure_BothOrNeither`。

- [ ] **Step 7: 运行写盘失败相关测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestCopilotSessionState_PersistFailureStillReturnsGeneratedPair|TestCopilotSessionState_PersistFailure_BothOrNeither" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 8: 实现写盘失败仍返回 pair 且保持 pair 一致性**

- [ ] **Step 9: 运行写盘失败相关测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestCopilotSessionState_PersistFailureStillReturnsGeneratedPair|TestCopilotSessionState_PersistFailure_BothOrNeither" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 10: 写“读失败立即生成 pair”失败测试**

新增：`TestCopilotSessionState_ReadFailureFallsBackToFreshPair`。

- [ ] **Step 11: 运行读失败降级测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_ReadFailureFallsBackToFreshPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 12: 实现读失败降级**

- [ ] **Step 13: 运行读失败降级测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_ReadFailureFallsBackToFreshPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 14: 写“写盘失败后下一请求再次生成”失败测试**

新增：`TestCopilotSessionState_PersistFailure_NextRequestRegeneratesPair`。

- [ ] **Step 15: 运行再次生成测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_PersistFailure_NextRequestRegeneratesPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 16: 实现再次生成语义**

- [ ] **Step 17: 运行再次生成测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_PersistFailure_NextRequestRegeneratesPair -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 18: 写主/shadow 锁域隔离失败测试**

新增：`TestCopilotSessionState_PrimaryAndShadowUseDifferentLockPaths`。

- [ ] **Step 19: 运行锁域隔离测试确认失败**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_PrimaryAndShadowUseDifferentLockPaths -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 20: 实现主/shadow 锁域隔离**

- [ ] **Step 21: 运行锁域隔离测试确认通过**

Run: `go test ./internal/runtime/executor -run TestCopilotSessionState_PrimaryAndShadowUseDifferentLockPaths -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 22: 写并发同 bucket 一致性失败测试**

新增：`TestCopilotSessionState_ConcurrentSameBucket_NoPartialPair`。

- [ ] **Step 23: 写 pair 同发/同省略失败测试**

新增：`TestCopilotSessionState_PairAlwaysBothPresentOrBothAbsent`。

- [ ] **Step 24: 运行并发+pair 一致性测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestCopilotSessionState_ConcurrentSameBucket_NoPartialPair|TestCopilotSessionState_PairAlwaysBothPresentOrBothAbsent" -count=1 -race`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 25: 实现并发一致性与 pair 强一致**

- [ ] **Step 26: 运行并发+pair 一致性测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestCopilotSessionState_ConcurrentSameBucket_NoPartialPair|TestCopilotSessionState_PairAlwaysBothPresentOrBothAbsent" -count=1 -race`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 27: 执行 SessionState 全测试确认通过**

Run: `go test ./internal/runtime/executor -run CopilotSessionState -count=1 -race`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 28: 提交 SessionState 改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_session_state_test.go
git commit -m "feat(copilot): finalize session state locking and degradation semantics"
```

## Chunk 3: Executor 接线与迁移门禁

### Task 6: 将 SessionState 接入 Header Compiler

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go`

- [ ] **Step 1: 写 user/agent 接线失败测试**

新增：
- `TestHeaderCompiler_UserInitiatorGeneratesSessionPair`
- `TestHeaderCompiler_AgentInitiatorReusesOrGeneratesSessionPair`
- `TestHeaderCompiler_PersistFailure_StillEmitsConsistentPairHeaders`

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_UserInitiatorGeneratesSessionPair|TestHeaderCompiler_AgentInitiatorReusesOrGeneratesSessionPair|TestHeaderCompiler_PersistFailure_StillEmitsConsistentPairHeaders" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 接入状态管理器并实现 pair 一致性规则**

规则：
- `X-Initiator` 必须本地推导，不读 incoming
- 生成失败：两字段同省略
- 写盘失败：继续发送本次生成 pair + warning

- [ ] **Step 4: 运行接线测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestHeaderCompiler_UserInitiatorGeneratesSessionPair|TestHeaderCompiler_AgentInitiatorReusesOrGeneratesSessionPair|TestHeaderCompiler_PersistFailure_StillEmitsConsistentPairHeaders" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 提交 HeaderCompiler 接线改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_header_compiler_test.go
git commit -m "feat(copilot): wire session pair policy into header compiler"
```

### Task 7: executor 模式切换（legacy / dual-run / strict）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`

- [ ] **Step 1: 写 strict 禁透传失败测试**

新增：`TestApplyHeaders_StrictMode_DoesNotForwardIncomingHeaders`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run TestApplyHeaders_StrictMode_DoesNotForwardIncomingHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 3: 实现 strict 分支**

- [ ] **Step 4: 运行 strict 分支测试确认通过**

Run: `go test ./internal/runtime/executor -run TestApplyHeaders_StrictMode_DoesNotForwardIncomingHeaders -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 5: 写 dual-run guard 失败测试**

新增：`TestApplyHeaders_DualRun_UsesShadowStateAndDoesNotMutatePrimaryState`。

- [ ] **Step 6: 运行 dual-run guard 测试确认失败**

Run: `go test ./internal/runtime/executor -run TestApplyHeaders_DualRun_UsesShadowStateAndDoesNotMutatePrimaryState -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 7: 实现 dual-run guard**

同时输出双边 diff 字段：
- `header`
- `diff_type`
- `legacy.source` / `candidate.source`
- `legacy.normalized_value` / `candidate.normalized_value`
- `legacy.value_hash` / `candidate.value_hash`

- [ ] **Step 8: 运行 dual-run guard 测试确认通过**

Run: `go test ./internal/runtime/executor -run TestApplyHeaders_DualRun_UsesShadowStateAndDoesNotMutatePrimaryState -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 9: 写路径回归失败测试（messages/responses + initiator）**

新增：
- `TestExecute_LegacyMode_MessagesPath_HeaderRules`
- `TestExecute_DualRunMode_MessagesPath_HeaderRules`
- `TestExecute_LegacyMode_ResponsesPath_HeaderRules`
- `TestExecute_DualRunMode_ResponsesPath_HeaderRules`
- `TestExecute_StrictMode_MessagesPath_HeaderRules`
- `TestExecute_StrictMode_ResponsesPath_HeaderRules`
- `TestApplyHeaders_InitiatorStillDerivedLocally`

- [ ] **Step 10: 运行回归测试确认失败**

Run: `go test ./internal/runtime/executor -run "TestExecute_LegacyMode_MessagesPath_HeaderRules|TestExecute_DualRunMode_MessagesPath_HeaderRules|TestExecute_LegacyMode_ResponsesPath_HeaderRules|TestExecute_DualRunMode_ResponsesPath_HeaderRules|TestExecute_StrictMode_MessagesPath_HeaderRules|TestExecute_StrictMode_ResponsesPath_HeaderRules|TestApplyHeaders_InitiatorStillDerivedLocally" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: FAIL。

- [ ] **Step 11: 完成接线实现**

- [ ] **Step 12: 运行回归测试确认通过**

Run: `go test ./internal/runtime/executor -run "TestExecute_LegacyMode_MessagesPath_HeaderRules|TestExecute_DualRunMode_MessagesPath_HeaderRules|TestExecute_LegacyMode_ResponsesPath_HeaderRules|TestExecute_DualRunMode_ResponsesPath_HeaderRules|TestExecute_StrictMode_MessagesPath_HeaderRules|TestExecute_StrictMode_ResponsesPath_HeaderRules|TestApplyHeaders_InitiatorStillDerivedLocally" -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 13: 提交 executor 接线改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go
git commit -m "feat(copilot): wire policy modes into executor"
```

### Task 8: 迁移门禁与最终验证

**Files:**
- Create: `internal/copilotgate/evaluator.go`
- Create: `internal/copilotgate/evaluator_test.go`
- Create: `internal/copilotgate/testdata/copilot_dualrun_7day_pass.json`
- Create: `internal/copilotgate/testdata/copilot_dualrun_7day_fail.json`
- Modify: `cmd/business/main.go`（增加门禁评估命令入口）
- Create: `cmd/business/main_test.go`（验证 gate 命令退出码 0/2/1）
- Modify: `docs/superpowers/specs/2026-03-14-copilot-header-governance-design.md`（如实现细节回填）

- [ ] **Step 1: 运行 executor 全量测试**

Run: `go test ./internal/runtime/executor -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 2: 运行 config 全量测试**

Run: `go test ./internal/config -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 3: 运行仓库级测试（必跑）**

Run: `go test ./... -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab/third_party/CLIProxyAPIPlus`

Expected: PASS。

- [ ] **Step 4: 新增 dual-run 门禁评估测试（先失败）**

新增：`TestDualRunGateEvaluator_RequiresSevenDayCleanWindow`。

- [ ] **Step 5: 列出目标测试名称，确认测试已注册**

Run: `go test ./internal/copilotgate -list '^TestDualRunGateEvaluator_RequiresSevenDayCleanWindow$'`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: 输出包含 `TestDualRunGateEvaluator_RequiresSevenDayCleanWindow`。

- [ ] **Step 6: 运行门禁评估测试确认失败**

Run: `go test ./internal/copilotgate -run TestDualRunGateEvaluator_RequiresSevenDayCleanWindow -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: FAIL。

- [ ] **Step 7: 实现门禁评估器并定义 diff 契约**

固定契约：
- `header`：标准化为 canonical MIME-style header 名称
- `diff_type` 枚举：`missing|extra|value_mismatch|source_mismatch`
- `legacy.source` 与 `candidate.source` 枚举：`computed|auth_metadata|config|constant`
- `normalized_value`：trim + 连续空白折叠 + 多值按字典序 join
- `legacy.value_hash` 与 `candidate.value_hash`：`sha256(lowercase(header)+"\n"+normalized_value)`
- 当 `diff_type` 为 `missing|extra` 时，缺失侧 `normalized_value=""`、`value_hash=""`（固定占位）

- [ ] **Step 8: 运行门禁评估测试确认通过**

Run: `go test ./internal/copilotgate -run TestDualRunGateEvaluator_RequiresSevenDayCleanWindow -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: PASS。

- [ ] **Step 9: 写 gate 命令退出码失败测试**

新增：`TestRunGateCopilotDualRun_ExitCodes`，覆盖：
- pass 报告 -> exit code 0
- fail 报告 -> exit code 2
- 参数错误/文件不存在 -> exit code 1

- [ ] **Step 10: 运行 gate 命令退出码测试确认失败**

Run: `go test ./cmd/business -run '^TestRunGateCopilotDualRun_ExitCodes$' -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: FAIL。

- [ ] **Step 11: 实现并接线运行时门禁命令**

在 `cmd/business/main.go` 增加命令：

```bash
cpab gate copilot-dualrun --report <path-to-7day-summary.json>
```

并将入口重构为可测形式：

```go
func run(args []string) int
```

退出码约定：
- `0`: 可切 strict
- `2`: 不可切 strict（有关键 diff 或未声明透传）
- `1`: 参数或运行错误

- [ ] **Step 12: 运行 gate 命令退出码测试确认通过**

Run: `go test ./cmd/business -run '^TestRunGateCopilotDualRun_ExitCodes$' -count=1`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: PASS。

- [ ] **Step 13: 执行 7 天门禁判定命令（pass 报告）并记录输出**

Run: `go run ./cmd/business gate copilot-dualrun --report ./internal/copilotgate/testdata/copilot_dualrun_7day_pass.json`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: exit code 0，且输出中 `critical_diff_new=0`、`undeclared_passthrough=0`。

- [ ] **Step 14: 执行 7 天门禁判定命令（fail 报告）并确认拦截**

Run: `go run ./cmd/business gate copilot-dualrun --report ./internal/copilotgate/testdata/copilot_dualrun_7day_fail.json`

Workdir: `d:/Caiqy/Projects/Github/cpab`

Expected: exit code 2，且输出包含 `critical_diff_new>0` 或 `undeclared_passthrough>0`。

- [ ] **Step 15: 提交最终改动**

Workdir: `d:/Caiqy/Projects/Github/cpab`

在实现说明中明确：切 strict 前需满足
- dual-run 连续 7 天无新增关键 diff
- 无未声明透传告警

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor third_party/CLIProxyAPIPlus/internal/config config.example.yaml internal/copilotgate cmd/business/main.go cmd/business/main_test.go docs/superpowers/specs/2026-03-14-copilot-header-governance-design.md docs/superpowers/plans/2026-03-14-copilot-header-governance-implementation-plan.md
git commit -m "feat(copilot): enforce governed upstream header policy"
```

## Done Criteria

- `strict` 模式下：没有任何未声明透传 header。
- `X-Agent-Task-Id` 始终等于 `X-Request-Id`，且为 UUIDv7。
- `Vscode-Sessionid` / `X-Interaction-Id`：同策略不同值，且同发或同省略。
- `X-Initiator=user` 必生成并保存 pair。
- `X-Initiator=agent` 无状态时立即生成并保存。
- 写盘失败不阻断请求，并继续发送本次已生成 pair。
- `dual-run` 仅写 shadow 状态，主状态不被污染。
