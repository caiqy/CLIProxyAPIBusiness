# GitHub Copilot X-Initiator 周期性放行 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 `force-agent-initiator` 增加按 `model + Copilot API token` 分桶的滚动窗口“严格一次放行”，并持久化状态到工作目录文件，同时更新 `docker-compose.yml` 挂载路径。

**Architecture:** 在 `GitHubCopilotExecutor` 内引入一个线程安全的 bypass 状态管理器：仅当原始请求会被判定为 `user` 时才尝试放行，命中放行后记录 `nextEligibleAt` 并原子写盘。`Execute` 与 `ExecuteStream` 共用同一决策逻辑，配置通过 `internal/config` 扩展并在构造器一次性初始化。

**Tech Stack:** Go（标准库 `sync`/`time`/`os`/`encoding/json`/`crypto/sha256`）、现有 executor 测试框架、Docker Compose。

---

### Task 1: 扩展配置模型（bypass 开关/窗口/状态文件）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/config.go`
- Test: `third_party/CLIProxyAPIPlus/internal/config/github_copilot_bypass_config_test.go`

**Step 1: 写失败测试（YAML 能解析新字段）**

```go
func TestLoadConfig_GitHubCopilotBypassConfig(t *testing.T) {
    cfgYAML := `github-copilot:
  force-agent-initiator: true
  force-agent-initiator-bypass:
    enabled: true
    window: 1h
    state-file: /CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json
`
    // 写入临时配置文件并调用 LoadConfig
    // 断言 Enabled=true, Window="1h", StateFile 非空
}
```

**Step 2: 运行测试确认失败**

Run（在 `third_party/CLIProxyAPIPlus` 目录）: `go test ./internal/config -run TestLoadConfig_GitHubCopilotBypassConfig -count=1`

Expected: FAIL（新字段尚未定义或值为空）

**Step 3: 最小实现（新增配置结构）**

```go
type ForceAgentInitiatorBypassConfig struct {
    Enabled   bool   `yaml:"enabled" json:"enabled"`
    Window    string `yaml:"window" json:"window"`
    StateFile string `yaml:"state-file" json:"state-file"`
}

type GitHubCopilotConfig struct {
    ForceAgentInitiator bool                              `yaml:"force-agent-initiator" json:"force-agent-initiator"`
    FakeAssistantContent string                           `yaml:"fake-assistant-content,omitempty" json:"fake-assistant-content,omitempty"`
    ForceAgentInitiatorBypass ForceAgentInitiatorBypassConfig `yaml:"force-agent-initiator-bypass" json:"force-agent-initiator-bypass"`
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/config -run TestLoadConfig_GitHubCopilotBypassConfig -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/config/config.go third_party/CLIProxyAPIPlus/internal/config/github_copilot_bypass_config_test.go
git commit -m "feat(config): add copilot initiator bypass settings"
```

### Task 2: 实现 bypass 状态管理器（内存 + JSON 持久化）

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass.go`
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass_test.go`

**Step 1: 写失败测试（滚动窗口严格一次）**

```go
func TestInitiatorBypass_AllowOncePerWindow(t *testing.T) {
    // fixed clock: t0
    // 第1次 user-only: shouldBypass=true
    // 第2次（同窗口）: shouldBypass=false
    // 时钟推进到 t0+window 后: shouldBypass=true
}
```

**Step 2: 写失败测试（原本 agent 不消耗机会）**

```go
func TestInitiatorBypass_AgentRequestDoesNotConsume(t *testing.T) {
    // hasAgentRole=true -> shouldBypass=false 且不更新 nextEligibleAt
    // 紧接着 user-only 请求仍应拿到首次放行
}
```

**Step 3: 写失败测试（持久化恢复）**

```go
func TestInitiatorBypass_PersistAndReload(t *testing.T) {
    // manager A 放行一次并写盘
    // manager B 从同一 state-file 启动
    // 同窗口内再次 user-only -> shouldBypass=false
}
```

**Step 4: 运行测试确认失败**

Run（在 `third_party/CLIProxyAPIPlus` 目录）: `go test ./internal/runtime/executor -run TestInitiatorBypass -count=1`

Expected: FAIL（文件/类型/函数未实现）

**Step 5: 最小实现状态管理器**

```go
type initiatorBypassManager struct {
    mu        sync.Mutex
    window    time.Duration
    stateFile string
    now       func() time.Time
    buckets   map[string]int64 // nextEligibleAt unix
}

func (m *initiatorBypassManager) ShouldBypass(model, apiToken string, hasAgentRole bool) bool {
    if hasAgentRole { return false }
    key := model + "|" + sha256Hex(apiToken)
    // 临界区内判断+更新+写盘
}
```

要求：
- token 仅用 hash 参与 key，不明文落盘
- 写盘使用 `tmp + rename` 原子替换
- 读盘失败时 fail-open（记录日志并空状态继续）

**Step 6: 运行测试确认通过**

Run: `go test ./internal/runtime/executor -run TestInitiatorBypass -count=1`

Expected: PASS

**Step 7: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass_test.go
git commit -m "feat(executor): add persisted initiator bypass manager"
```

### Task 3: 接入 Execute/ExecuteStream 决策链

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`

**Step 1: 写失败测试（Execute：首个 user-only 放行，第二个注入）**

```go
func TestExecute_ForceAgentInitiatorBypass_OncePerWindow(t *testing.T) {
    // 同一 model + apiToken 连续两次 user-only 请求
    // 第1次 X-Initiator=user
    // 第2次 X-Initiator=agent
}
```

**Step 2: 写失败测试（ExecuteStream 行为一致）**

```go
func TestExecuteStream_ForceAgentInitiatorBypass_OncePerWindow(t *testing.T) {
    // 流式请求重复上述断言
}
```

**Step 3: 写失败测试（原本 agent 不占用机会）**

```go
func TestExecute_ForceAgentInitiatorBypass_AgentDoesNotConsume(t *testing.T) {
    // 第1次请求 body 已含 assistant -> X-Initiator=agent（自然）且不占用
    // 第2次 user-only 仍应得到放行 -> X-Initiator=user
}
```

**Step 4: 运行测试确认失败**

Run（在 `third_party/CLIProxyAPIPlus` 目录）: `go test ./internal/runtime/executor -run ForceAgentInitiatorBypass -count=1`

Expected: FAIL

**Step 5: 最小实现（接入判断）**

```go
// NewGitHubCopilotExecutor 中初始化 bypass manager（解析 window）

hasAgentRole := containsAgentConversationRole(body)
if e.cfg.GitHubCopilot.ForceAgentInitiator && !hasAgentRole {
    if e.initiatorBypass != nil && e.initiatorBypass.ShouldBypass(req.Model, apiToken, false) {
        // 放行：不注入
    } else {
        body = injectFakeAssistantMessage(body, e.cfg.GitHubCopilot.FakeAssistantContent, useResponses)
    }
}
```

**Step 6: 运行测试确认通过**

Run: `go test ./internal/runtime/executor -run ForceAgentInitiatorBypass -count=1`

Expected: PASS

**Step 7: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go
git commit -m "feat(executor): apply one-pass bypass before initiator injection"
```

### Task 4: 异常路径与健壮性补测

**Files:**
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass_test.go`

**Step 1: 写失败测试（损坏 state-file 读入）**

```go
func TestInitiatorBypass_CorruptStateFile_FailOpen(t *testing.T) {
    // 写入非法 JSON
    // 初始化 manager 不应 panic
    // 首次 user-only 应可放行
}
```

**Step 2: 写失败测试（写盘失败不影响请求）**

```go
func TestInitiatorBypass_PersistFailure_DoesNotBlockDecision(t *testing.T) {
    // state-file 指向不可写路径
    // shouldBypass 仍按窗口决策返回（并记录错误日志）
}
```

**Step 3: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run "CorruptStateFile|PersistFailure" -count=1`

Expected: FAIL

**Step 4: 最小实现补全**

```go
func (m *initiatorBypassManager) loadState() {
    // json.Unmarshal 失败 -> log + 空 map
}

func (m *initiatorBypassManager) persistLocked() error {
    // 写失败只返回 error，不改变本次决策结果
}
```

**Step 5: 运行测试确认通过**

Run: `go test ./internal/runtime/executor -run "CorruptStateFile|PersistFailure" -count=1`

Expected: PASS

**Step 6: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass_test.go third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_initiator_bypass.go
git commit -m "test(executor): cover bypass state corruption and persist failures"
```

### Task 5: 配置示例与 Docker Compose 挂载落地

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/config.example.yaml`
- Modify: `docker-compose.yml`

**Step 1: 写失败校验（compose 语法）**

Run（仓库根目录）: `docker compose config`

Expected: 当前 PASS（作为基线），后续修改后仍需 PASS

**Step 2: 更新示例配置注释段**

```yaml
# github-copilot:
#   force-agent-initiator: true
#   fake-assistant-content: "OK."
#   force-agent-initiator-bypass:
#     enabled: true
#     window: 1h
#     state-file: /CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json
```

**Step 3: 更新根目录 docker-compose 挂载**

```yaml
services:
  cpab:
    volumes:
      - ./data/cpab:/CLIProxyAPIBusiness/data
```

**Step 4: 运行校验确认通过**

Run: `docker compose config`

Expected: PASS（输出规范化后的 compose）

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/config.example.yaml docker-compose.yml
git commit -m "chore: add copilot bypass state-file example and compose volume"
```

### Task 6: 全量回归与收尾

**Files:**
- Test-only（不改代码）

**Step 1: 运行 executor 相关测试集**

Run（在 `third_party/CLIProxyAPIPlus` 目录）: `go test ./internal/runtime/executor -count=1`

Expected: PASS

**Step 2: 运行 config 相关测试集**

Run（在 `third_party/CLIProxyAPIPlus` 目录）: `go test ./internal/config -count=1`

Expected: PASS

**Step 3: 运行目标性 smoke（可选）**

Run（仓库根目录）: `docker compose config`

Expected: PASS

**Step 4: 若有遗漏修复，补最后提交**

```bash
git add <修复文件>
git commit -m "fix: finalize copilot initiator bypass rollout"
```
