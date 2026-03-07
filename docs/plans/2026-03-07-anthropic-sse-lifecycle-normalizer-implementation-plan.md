# Anthropic SSE Lifecycle Normalizer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 `Claude -> Claude` 直通热路径增加贴合现有代码结构的 Anthropic SSE 生命周期归一化 helper，修复 `content_block_stop` 提前透传导致的非法时序，同时保留默认开启、可关闭的运维能力。

**Architecture:** 首版不急于新建通用 `internal/runtime/streamnormalize` 包，而是在 `third_party/CLIProxyAPIPlus/internal/runtime/executor` 内新增 Claude 定向 helper（例如 `claude_sse_normalizer.go`），对 executor 暴露 `ProcessLine/Finalize` 契约，并在 helper 内部把 line-oriented SSE 输入聚合成完整 event 后再做 lifecycle 状态机判断。首批仅接入 `claude_executor.go` 的 `from == to` 直通分支；若后续出现第二个 Anthropic SSE 输出消费者，再评估抽取共享包。

**Tech Stack:** Go（标准库、logrus、httptest、testing）、YAML 配置。

---

### Task 1: 先用状态机测试锁定正常流与异常流边界

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer_test.go`

**Step 1: 写失败测试（正常流与提前 stop）**

在 `claude_sse_normalizer_test.go` 增加最小输入输出用例，至少覆盖：

- 正常流：
  - `content_block_start(index=0)`
  - `content_block_delta(index=0)`
  - `content_block_stop(index=0)`
  - `message_delta`
  - 期望输出仍满足 `start -> delta -> stop -> message_delta`
- 异常流：
  - `start -> delta -> stop -> delta -> message_stop`
  - 期望第一个 stop 不立即输出，最终只输出一个合法 stop

**Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer`

Expected: FAIL（包/类型/方法尚不存在）。

**Step 3: 补充多 index / EOF flush / 重复 stop 测试**

继续补充：

- `index=0` 与 `index=1` 并发交错；
- `event:` / `data:` / 空行组成的完整 SSE event 边界；
- 有 `pendingStop` 时直接 `Finalize()`；
- 重复 `content_block_stop`；
- `text` / `thinking` / `tool_use` 三类 block 都走同一生命周期逻辑。

**Step 4: 再次运行测试确认失败**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer`

Expected: FAIL。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer_test.go
git commit -m "test(stream): lock anthropic SSE lifecycle normalization behavior"
```

### Task 2: 实现独立的 Anthropic SSE 生命周期 normalizer

**Files:**
- Create: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer.go`

**Step 1: 先让测试编译通过但语义仍失败**

先声明最小类型与接口，建议至少包含：

- `type AnthropicSSELifecycleNormalizer struct { ... }`
- `ProcessLine([]byte) ([][]byte, error)`
- `Finalize() ([][]byte, error)`
- 请求级状态 `RequestState`
- `BlockState`

注意：第一版不必为了未来复用过早抽象通用 `Normalizer` 接口；先做 Claude 定向 helper，更贴合当前代码结构。

**Step 2: 运行测试确认仍失败**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer`

Expected: FAIL（状态机逻辑尚未实现或输出不符合期望）。

**Step 3: 写最小实现（只修生命周期，不改内容）**

实现要点：

- helper 对外接收 scanner 的单行输入，但**内部必须按完整 SSE event 做状态机判断**；
- 仅解析 Anthropic SSE 事件中的 `type` / `index` / `content_block.type`；
- `content_block_stop` 到达时先挂起，不立即输出；
- 若后续同 index 出现 `content_block_delta`，继续保留 `pendingStop`，仅延后 stop 的对外可见时机；
- 在 `message_delta`、`message_stop`、`Finalize()` 时 flush `pendingStop`；
- 多个 `pendingStop` 统一按 `index` 升序 flush；
- 只修事件顺序，不改写 `text`、`thinking`、`partial_json`；
- 对超出本轮范围的非法序列记录最小必要日志并保守处理；
- 日志策略与当前仓库保持一致：修复动作走 `Debug`，真正异常走 `Warn`，关键上下文写入消息文本，不新增 feature-local `debug-log` 配置。

**Step 4: 运行测试验证通过**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer.go third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer_test.go
git commit -m "feat(stream): add anthropic SSE lifecycle normalizer"
```

### Task 3: 增加“默认开启、显式可关闭”的配置开关

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/sdk_config.go`
- Create: `third_party/CLIProxyAPIPlus/internal/config/streaming_config_test.go`
- Modify: `third_party/CLIProxyAPIPlus/config.example.yaml`
- Modify: `config.example.yaml`

**Step 1: 写失败测试（默认开启、显式 false 可关闭）**

在 `streaming_config_test.go` 覆盖至少两类行为：

- 配置缺省时，`streaming.anthropic-sse-lifecycle-enable` 视为启用；
- 显式配置 `anthropic-sse-lifecycle-enable: false` 时，视为关闭；

注意：第一版不引入 `debug-log` 之类的 feature-local 配置。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/config -run StreamingConfig_AnthropicSSELifecycle`

Expected: FAIL（结构体/默认值辅助函数尚不存在）。

**Step 3: 实现最小配置面并更新示例配置**

建议在 `StreamingConfig` 下新增最小字段：

- `AnthropicSSELifecycleEnable *bool`
- 辅助方法：`AnthropicSSELifecycleEnabled() bool`（`nil => true`，显式 `false => false`）

并在两个 `config.example.yaml` 中补充示例：

```yaml
streaming:
  keepalive-seconds: 0
  bootstrap-retries: 0
  anthropic-sse-lifecycle-enable: true
```

**Step 4: 运行测试验证通过**

Run: `go test ./internal/config -run StreamingConfig_AnthropicSSELifecycle`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/config/sdk_config.go third_party/CLIProxyAPIPlus/internal/config/streaming_config_test.go third_party/CLIProxyAPIPlus/config.example.yaml config.example.yaml
git commit -m "feat(config): add anthropic SSE lifecycle normalizer toggle"
```

### Task 4: 把 normalizer 接入 Claude -> Claude 直通热路径

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor_test.go`

**Step 1: 写失败测试（默认启用、可关闭、EOF flush）**

在 `claude_executor_test.go` 增加至少三类用例：

1. **默认启用修复**：上游返回 `start -> delta -> stop -> delta -> message_stop`，期望输出对下游不再出现 `stop` 后同 index 仍有 delta；
2. **显式关闭**：关闭配置后，保留当前原样直通行为；
3. **EOF flush**：上游结束前只有 `pendingStop` 未遇到 `message_delta/message_stop`，期望 scanner 结束时仍补发 stop。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run ClaudeExecutor_ExecuteStream_AnthropicLifecycle`

Expected: FAIL（热路径尚未接入 normalizer）。

**Step 3: 在 `from == to` 分支接入 normalizer**

实现要点：

- 仅在 `to == from == claude` 且配置开启时创建 `AnthropicSSELifecycleNormalizer`；
- scanner 每读到一条 SSE line，调用 `ProcessLine`，把返回结果顺序写入 `out`；
- scanner 正常结束后调用 `Finalize()`，把剩余挂起 stop 输出；
- 对 helper 输出的最终 line，再执行现有 usage 解析与 tool prefix 处理，避免与延迟 stop 的顺序冲突；
- 保持现有错误上报链路不变；
- 配置关闭时走现有原样直通分支，确保可回滚。

**Step 4: 运行测试验证通过**

Run: `go test ./internal/runtime/executor -run ClaudeExecutor_ExecuteStream_AnthropicLifecycle`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor.go third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor_test.go
git commit -m "fix(claude): normalize anthropic SSE lifecycle on direct stream path"
```

### Task 5: 加固 SSE 事件边界处理与最小日志策略

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer_test.go`

**Step 1: 写失败测试（事件边界与非生命周期行透传）**

增加最小边界测试，至少覆盖：

- 带 `event:` 前缀的行不会破坏 event 聚合；
- 非 lifecycle 相关的 `data:` 行原样透传；
- 空行作为 event 边界时，pending stop 的释放时机仍然正确。

避免把测试绑定到完整日志文本；日志策略以人工 code review 和最终回读为主。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer_EventBoundaries`

Expected: FAIL。

**Step 3: 实现事件边界收敛与最小日志策略**

实现要点：

- 确保 helper 以完整 SSE event 为判断单位，而不是逐行字符串替换；
- 日志仅保留最小必要信息：`index / action / reason / event_type` 写进消息文本；
- 修复动作走 `Debug`，真正异常走 `Warn`；
- 不新增 feature-local 日志开关。

**Step 4: 运行测试验证通过**

Run: `go test ./internal/runtime/executor -run ClaudeSSENormalizer_EventBoundaries`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer.go third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_sse_normalizer_test.go
git commit -m "chore(stream): harden claude SSE event boundary handling"
```

### Task 6: 全量回归与交付校验

**Files:**
- Modify: `docs/plans/2026-03-07-anthropic-sse-lifecycle-normalizer-design.md`（如实现中有必要补充设计偏差）

**Step 1: 运行 helper / config / executor 关键测试**

Run: `go test ./internal/config ./internal/runtime/executor`

Expected: PASS。

**Step 2: 运行 Claude executor 相关定向回归**

Run: `go test ./internal/runtime/executor -run ClaudeExecutor_ExecuteStream`

Expected: PASS。

**Step 3: 人工回读设计与实现是否一致**

核对以下口径未偏离设计：

- 只修事件时序，不改内容；
- 默认开启、可关闭；
- 仅首批接入 `Claude -> Claude` 热路径；
- 统一覆盖所有 `content_block` 类型；
- helper 内按完整 SSE event 判断，而不是逐行字符串替换；
- 不把 beta header 降级作为方案组成部分。

**Step 4: 如有偏差，补充设计文档说明**

若实现中对配置命名、日志字段、flush 顺序等做了等价调整，及时回写到设计文档，避免设计与代码口径漂移。

**Step 5: Commit**

```bash
git add docs/plans/2026-03-07-anthropic-sse-lifecycle-normalizer-design.md
git commit -m "docs(stream): align anthropic lifecycle normalizer design with implementation"
```
