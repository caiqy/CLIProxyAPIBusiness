# Anthropic SSE 生命周期归一化设计

**日期：** 2026-03-07  
**范围：** 为 Anthropic / Claude SSE 输出链路增加协议内生命周期归一化能力，首批接入 `Claude -> Claude` 直通热路径。  
**目标：** 在不改写内容和业务语义的前提下，修复 `content_block_stop` 提前透传导致的非法时序，保证下游始终看到合法的 `start -> 0..n delta -> stop` 序列。

---

## 1. 背景与问题

当前热路径中，`third_party/CLIProxyAPIPlus/internal/runtime/executor/claude_executor.go` 在 `from == to`（`Claude -> Claude`）时会直接透传上游 SSE，而不会经过 translator 层。这样做符合现有“同协议直通、跨协议翻译”的整体惯例，但也意味着只要上游流中存在协议级时序异常，就会被原样暴露给下游。

本次已确认的异常是：同一 `content_block` 在 `content_block_stop(index=x)` 之后，又出现了同一 `index=x` 的 `content_block_delta`。这会破坏 Anthropic SSE 的生命周期约束，并在依赖严格 block 生命周期的下游 SDK 中触发类似 `text part 0 not found` 的解析错误。

本设计聚焦于**SSE 协议生命周期修复**本身，不把 beta header 作为解决方向，因为 beta header 不是该问题的根因。

---

## 2. 设计目标与非目标

### 2.1 设计目标

1. **默认修复热路径**：对 `Claude -> Claude` 直通流默认启用生命周期归一化，并允许通过配置关闭。
2. **协议内修复，不改内容**：仅修正事件输出时序，不改写 `text`、`thinking`、`partial_json` 等内容本身。
3. **统一覆盖所有 `content_block` 类型**：同一机制适配 `text` / `thinking` / `tool_use`，避免未来同类问题重复出现。
4. **请求级隔离、按 index 跟踪**：状态只能在单请求内生效，绝不跨请求共享。
5. **可复用扩展**：把能力抽象为“流式归一化层”，后续其他最终输出 Anthropic SSE 的链路也可复用。
6. **可观测、可回滚**：记录结构化日志，支持默认开启但可快速关闭。

### 2.2 非目标（YAGNI）

1. 本轮不把 `Claude -> Claude` 直通强行改造成 `Claude -> Claude translator`。
2. 本轮不尝试修复所有可能的 SSE 非法序列，只聚焦已确认的生命周期问题与最小必要防御。
3. 本轮不通过删除/降级 beta header 规避问题。
4. 本轮不引入复杂 metrics 体系；若现有基础设施不足，至少保证结构化日志可统计。

---

## 3. 方案对比与选型

### 方案 A：在热路径前增加请求级 SSE normalizer（基础版）

在 executor 的 SSE scanner 与 downstream chunk 输出之间插入一个请求级 normalizer，修复生命周期时序后再透传。

**优点：**
- 贴近根因链路；
- 改动集中，风险相对可控；
- 对热路径修复最直接。

**缺点：**
- 如果实现为 Claude 特判，后续扩展边界不够清晰。

### 方案 B：新增 `Claude -> Claude` translator

即使 `from == to` 也统一走 `sdktranslator.TranslateStream(...)`，新增一个同协议 translator 专门做修复。

**优点：**
- 从形式上最统一；
- 所有流式改写都进入 translator 框架。

**缺点：**
- 不符合项目当前“同协议直通、跨协议翻译”的既有约定；
- 会扩大 translator 职责，把“协议内修复”和“协议间转换”混在一起；
- 为当前问题引入了不必要的结构性改动。

### 方案 C：在各下游转换器/解析器分别兜底

在 OpenAI / Gemini / Responses 等下游链路中分别容忍 stop 后仍有 delta 的异常流。

**优点：** 改单点可能快。  
**缺点：** 根因治理错误、逻辑分散、行为不一致、长期维护差。

### 最终选型：A+（贴合现状的收敛版）

采用 **A 的热路径修复路径**，但对落地方式做两点收敛：

> **首版在 `Claude -> Claude` 直通 streaming 分支增加 request-scoped 的 `AnthropicSSELifecycleNormalizer` helper；代码先落在 executor 侧，等出现第二个消费者后再评估是否抽到共享包。**

这样做的原因不是“translator 天然不能做协议内修复”，而是：

- 当前问题实际发生在 `claude_executor.go` 的直通分支；
- 在 executor 侧接入改动最小、最贴近问题源头；
- 现有仓库中 translator 已经存在同协议 normalization 先例，因此本次只是**选择更贴合当前热路径的挂接点**，而不是重新定义 translator 的绝对职责边界。

---

## 4. 架构与组件边界

### 4.1 分层职责

结合现有代码，建议把这次能力理解为四个贴近现状的层次：

1. **Claude executor 直通分支**
   - 负责发请求、收响应、按行扫描 SSE、控制开关；
   - 当前本身已经承担少量 Claude 协议感知后处理（如 usage 解析、tool prefix 处理），因此本次是在该分支中再收敛出一个更明确的 lifecycle helper，而不是从“纯转发 executor”开始重新分层。

2. **Claude SSE lifecycle helper**
   - 首版实现 `AnthropicSSELifecycleNormalizer`；
   - request-scoped，按 `index` 持有 block 生命周期状态；
   - 内部负责把 line-oriented SSE 输入聚合成完整事件，再做 lifecycle 修复。

3. **translator 边界说明**
   - `sdk/translator` 在当前仓库中主要是 registry / contract；
   - 真正的协议转换实现位于 `internal/translator/*`；
   - 因此本设计讨论的“是否放 translator”，本质上是在比较“挂在 executor 直通分支”还是“挂在具体 translator 输出之后”，而不是在比较某个单一包。

4. **日志 / 回滚层**
   - 记录异常输入、修复动作、flush 原因；
   - 保证默认开启但能快速关闭，便于线上排障与回滚。

### 4.2 推荐数据流

由于现有热路径的 scanner 是**按行**工作的，而不是按完整 SSE event 工作，第一版更贴合现状的数据流应为：

`Claude Executor -> SSE line scanner -> Claude SSE helper(内部聚合完整 event) -> normalized lines -> 既有 usage/prefix 处理 -> downstream chunks`

也就是说：

- executor 仍保留 line scanner；
- lifecycle 判断在 helper 内按**完整 SSE event**完成，而不是把 helper 做成简单的“逐行 mapper”；
- 对下游可见的最终输出，必须已经过 lifecycle 修复。

### 4.3 为什么首版不放进 translator

这里的结论应更精确地表述为：

- 当前仓库中，translator **并不只做 A -> B 转换**，已经存在同协议 normalization 的先例；
- 但本次问题发生在 `Claude -> Claude` 的直通 streaming 分支；
- 因此首版放在 executor 侧，不是因为 translator 不能做，而是因为这能以最小改动覆盖当前热路径。

未来若第二条 Anthropic SSE 输出链路也需要同样的 lifecycle 修复，再评估把 helper 抽到共享位置，或挂到具体 translator 输出之后。

---

## 5. 状态模型与生命周期状态机

### 5.1 状态隔离原则

normalizer 必须按**单请求**维护状态，不能使用任何全局共享状态；并且要按 `index` 跟踪每个 `content_block` 的生命周期。

### 5.2 RequestState

每个请求维护一个 `RequestState`，至少包含：

- `blocks[index] -> BlockState`
- 输入事件序号（用于日志）
- 输出事件序号（用于日志）
- 流结束标记
- 最近 message 级事件信息（可选，用于日志）

### 5.3 BlockState

每个 `index` 维护最小必要状态：

- `started`
- `closed`
- `pendingStop`
- `blockType`
- `startSeenOrder`
- `lastDeltaSeenOrder`
- `pendingStopSeenOrder`
- `finalStopEmitOrder`

其中关键语义为：

- `pendingStop=true`：stop 已收到，但暂不对下游可见；
- `closed=true`：stop 已最终输出，生命周期正式结束。

### 5.4 事件处理规则

#### `content_block_start(index=x)`
- 首次出现：创建/初始化 `BlockState`，立即透传；
- 若 block 未关闭又重复 start：记录异常日志，默认保守透传或按后续策略处理。

#### `content_block_delta(index=x)`
- block 活跃：直接透传；
- 若 `pendingStop=true`：说明之前 stop 提前，继续透传当前 delta，但**保留挂起 stop，延后到后续收口点再补发**，并记录一次修复日志；
- 若 `closed=true`：说明发生更严重非法序列，记录异常日志并执行保守策略。

#### `content_block_stop(index=x)`
- block 活跃：**先挂起，不立即透传**，设置 `pendingStop=true`；
- 若已 `pendingStop=true`：记录重复 stop；
- 若已 `closed=true`：记录重复/无效 stop。

#### `message_delta` / `message_stop` / EOF
- 视为 message 级收口点；
- 在透传这些事件之前，先 flush 所有挂起的 `pendingStop`。

### 5.5 核心设计原则

此次修复的关键不在于缓存或改写 delta，而在于：

> **把 stop 从“立即可见事件”改为“延迟确认事件”。**

只要 stop 还可能被后续同 index 的 delta 证明为“提前 stop”，它就不应该对下游可见。

---

## 6. 数据流与 flush 时机

### 6.1 输入输出契约

为贴合现有 `bufio.Scanner` 按行扫描的实现，helper 对 executor 暴露的第一版契约建议为：

1. `ProcessLine(line) -> []lines`
2. `Finalize() -> []lines`

其中：

- `ProcessLine` 接收 scanner 产出的单行输入；
- helper **内部**负责缓存并识别完整 SSE event 边界（例如空行结束）；
- 只有在形成完整 event、并完成 lifecycle 判断后，才向外输出可下发的 line 序列；
- `Finalize()` 用于 EOF / scanner 结束 / 上层主动收口时补发挂起 stop。

这样既不要求重写现有 scanner 模型，又避免把 lifecycle 修复误做成逐行字符串改写。

### 6.2 正常流处理

对正常序列：

`start -> delta -> delta -> stop -> message_delta -> message_stop`

normalizer 的行为是：

- `start`：立即透传
- `delta`：立即透传
- `stop`：先挂起
- `message_delta` 到达时：先 flush `pendingStop`，再透传 `message_delta`
- `message_stop` 到达时：若仍有挂起 stop，先 flush 再透传

这样输出给下游的仍是合法序列，只是 stop 的“对外可见时机”被延迟了。

### 6.3 异常流处理

对异常序列：

`start -> delta -> stop -> delta -> ... -> message_stop`

normalizer 的行为是：

1. `start` 透传
2. `delta` 透传
3. `stop` 挂起，不透传
4. 后续 `delta` 到达时，保持 `pendingStop` 继续挂起，透传该 delta，并记录“stop 被延后输出”日志
5. 到 `message_stop` / `message_delta` / EOF 时，再补发真正 stop

最终下游看到的是合法序列：

`start -> delta -> delta -> ... -> stop -> message_stop`

### 6.4 flush 时机

第一版必须支持的 flush 时机：

1. 收到 `message_delta`
2. 收到 `message_stop`
3. 流结束 / scanner 结束 / EOF
4. 上层显式调用 `Finalize()`

### 6.5 flush 顺序

当多个 `pendingStop` 并存时，统一按 **index 升序** flush，保证行为稳定、可预测、易测试。

### 6.6 第一版明确不做的事

- 不缓存 text/thinking 内容再重组；
- 不补造缺失 start；
- 不尝试修正所有类型的非法序列；
- 不跨请求共享任何状态。

---

## 7. 配置、接入与复用边界

### 7.1 启用策略

本设计要求：

- **默认开启**
- **允许关闭**

但第一版应尽量收敛配置面，不引入新的多级 `normalizers` 树，也不单独增加 feature-local `debug-log`。更贴合现有配置风格的形态是：

```yaml
streaming:
  anthropic-sse-lifecycle-enable: true
```

原因：

- 当前 `streaming` 配置本身已存在；
- 先以单一布尔开关落地，改动最小；
- 若未来真的出现第二个 normalizer，再把配置演进为更通用的嵌套结构。

为兼容“默认开启、显式可关闭”，实现层仍建议使用可区分“未配置”和“显式 false”的配置表达（例如 `*bool`）。

### 7.2 首批接入范围

第一批仅接入：

- `Claude -> Claude` 直通热路径

即 `claude_executor.go` 的 `from == to` 分支。

### 7.3 后续复用边界

该 helper 的复用边界不是“上游 provider 是否是 Claude”，而是：

> **凡是最终对下游输出 Anthropic / Claude SSE 的链路，都可以复用同一套 lifecycle-normalization 逻辑。**

但要明确：**复用的是组件，不是固定接入层位。**

可能的挂接点包括：

- `Claude -> Claude` 直通链路：挂在 executor scanner 之后；
- 常规 `* -> claude` translator 链路：挂在 `TranslateStream(...)` 输出之后；
- 类似 Kiro 这种还会在 executor 内二次调整 SSE 的链路：挂在最终对外发出 Anthropic SSE 之前。

因此，未来即使上游是 OpenAI / Gemini / Kiro，只要某条链路的最终输出协议仍是 Anthropic SSE，就可以复用本组件；而对于最终输出 OpenAI / Gemini / Responses 的链路，则应保留独立协议 normalizer 的扩展空间。

---

## 8. 异常处理与可观测性

### 8.1 基本原则

normalizer 应遵循：

> **尽量修复、绝不静默吞问题；对超出本轮设计范围的非法序列，记录带关键上下文的诊断日志，并采取保守策略。**

### 8.2 日志与关键信息表达

当前仓库的 logrus formatter 只会稳定展示有限字段，因此第一版设计不应依赖“新增大量结构化字段后日志自然可见”这一假设。更贴合现状的策略是：

- 继续沿用现有 `request_id` 注入方式；
- 关键诊断信息直接写入日志消息文本；
- 如有需要，可附带少量 fields，但不把它们作为唯一观测载体。

建议日志消息至少能体现：

- `index`
- `event_type`
- `action`
- `reason`

其中 `action` 可包括：

- `hold_stop`
- `release_pending_stop`
- `delay_stop`
- `replace_pending_stop`
- `delta_after_closed_passthrough`

### 8.3 日志级别策略

第一版建议直接复用全局日志级别：

- 修复动作、补发 stop：`Debug`
- 真正超出预期的非法序列：`Warn`

不额外引入 feature-local 的 `debug-log` 配置。

### 8.4 回滚要求

必须支持快速回滚：

- 关闭 normalizer 后恢复现有直通行为；
- 不影响其他 executor / translator；
- 不需要改动业务协议与调用方配置。

### 8.5 指标建议（可选）

若后续现有指标体系易于接入，可考虑增加：

- `normalizer_pending_stop_held_total`
- `normalizer_pending_stop_released_total`
- `normalizer_early_stop_repaired_total`
- `normalizer_unexpected_delta_after_closed_total`
- `normalizer_duplicate_stop_total`

若当前不便接入 metrics，则至少保证日志包含足够关键上下文，便于离线统计。

---

## 9. 测试计划与验收标准

### 9.1 测试层次

1. **Claude SSE helper 单元测试**：放在独立测试文件中，直接输入 line-oriented SSE，验证事件聚合与输出序列。  
2. **executor 接入测试**：保留在 `claude_executor_test.go`，验证热路径默认启用、可关闭、EOF flush 生效。  
3. **配置测试**：仅验证默认开启与显式关闭行为，不扩散到过重的配置矩阵。  
4. **问题回归测试**：稳定复现“stop 后又来 delta”的旧问题，并验证修复后输出合法。

### 9.2 关键单测场景

- 正常流：`start -> delta -> stop -> message_delta -> message_stop`
- 提前 stop：`start -> delta -> stop -> delta -> message_stop`
- 多 block 并发：`index=0/1` 交错
- 所有 block 类型：`text / thinking / tool_use`
- `event:` / `data:` / 空行构成的完整 SSE event 边界
- 流结束补发：存在 `pendingStop` 时直接 EOF
- 重复 stop
- closed 后再来 delta

### 9.3 接入测试重点

- `Claude -> Claude` 热路径默认启用 normalizer；
- 配置关闭后恢复原始直通；
- scanner 结束时会执行 `Finalize()`；
- 对外输出前的 usage 解析 / tool prefix 处理顺序不回归；
- 正常合法流不应被误改。

### 9.4 验收标准

1. 对同一 `index`，输出给下游的序列始终满足：`start -> 0..n delta -> stop`
2. 不再对下游暴露 `content_block_stop` 后同 index 仍有 delta 的非法序列
3. `text` / `thinking` / `tool_use` 统一适用相同生命周期修复规则
4. 文本内容、thinking 内容、tool_use 输入内容保持不变
5. 并发请求之间状态隔离正确
6. 关闭开关后，行为退回现有直通模式

---

## 10. 发布与演进建议

1. 先以默认开启、可关闭的形式接入 `Claude -> Claude` 热路径。
2. 重点观察 `Debug/Warn` 日志中“提前 stop 修复次数”和“closed 后 delta”等异常分布。
3. 若出现第二个 Anthropic SSE 消费点，再把 helper 从 executor 侧抽到共享位置；在此之前不急于引入新的通用 runtime 包。
4. 若未来其他协议也出现类似“协议内语义修复”需求，再新增对应协议 helper / normalizer，而不是预先扩大抽象层级。

---

本设计已确认采用：

- 设计方向：**以 `Claude -> Claude` 直通分支为首个落点的 `AnthropicSSELifecycleNormalizer` helper**
- 首批范围：**先接入 `Claude -> Claude` 直通热路径**
- 启用策略：**默认开启，可关闭；首版使用单一布尔配置而非多级 normalizers 配置树**
- 修复对象：**统一覆盖所有 `content_block` 类型**
- 输入契约：**对 executor 暴露 line-oriented API，但在 helper 内按完整 SSE event 做生命周期判断**
- 复用边界：**组件可复用，但具体挂接点按链路而异**
- 根因口径：**聚焦 SSE 生命周期修复，不把 beta header 作为方案组成部分**
