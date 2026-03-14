# GitHub Copilot 上游 Header 治理设计

## 1. 背景

当前 Copilot 上游请求仍存在混合策略：

- 一部分 header 由代码常量写死
- 一部分 header 从入站请求透传
- 一部分 header 从 auth metadata 注入

这会导致来源不可审计、行为不稳定、抓包对齐成本高。目标是建立统一、可治理、可回放的 header 生成体系。

## 2. 设计目标

1. **可维护与可审计优先**：每个上游 header 都必须声明来源。
2. **去透传化**：默认不透传入站 header（除非明确列入治理白名单并声明来源）。
3. **会话一致性**：`Vscode-Sessionid` / `X-Interaction-Id` 使用同策略（不同值），并与 `X-Initiator` 共享隔离机制。
4. **行为可迁移**：支持 `legacy -> dual-run -> strict` 三阶段切换。

## 3. 已确认决策（用户确认）

1. `Editor-Device-Id` / `Vscode-Abexpcontext` / `Vscode-Machineid` 从 auth 文件（`auth.Metadata`）获取。
2. `X-Agent-Task-Id = X-Request-Id`（同值）。
3. `X-Interaction-Id` 与 `Vscode-Sessionid`：同策略、不同值。
4. `X-Initiator=user` 时触发生成并保存 session/interaction。
5. `X-Initiator=agent` 且无已保存值时：**立即生成新的并保存**。

## 4. 总体架构

新增“Header 编译器”层，所有 Copilot 上游 header 统一由该层产出：

1. 输入：
   - `auth.Metadata`
   - 请求上下文（request scope）
   - 配置（`github-copilot`）
   - 计算结果（如 `X-Initiator`）
2. 处理：
   - 按 `HeaderSpec` 声明规则计算
   - 执行校验、默认值、冲突优先级（固定判定表）
   - 记录来源审计信息（不记录敏感明文）
3. 输出：
   - 最终上游 header map

## 5. Header 来源矩阵（目标态）

统一判定原则（强约束）：

- **默认不透传**：入站请求头默认不可直接进入上游。
- **唯一来源**：每个上游 header 只有一个主来源定义。
- **缺失处理统一**：除非特殊声明，缺值时“省略 header”，不发送空字符串。
- **冲突优先级固定**：`computed > auth_metadata > config > constant > incoming(disabled)`。

### 5.1 Auth Metadata 来源

- `Editor-Device-Id` <- `auth.Metadata["editor_device_id"]`
- `Vscode-Abexpcontext` <- `auth.Metadata["vscode_abexpcontext"]`
- `Vscode-Machineid` <- `auth.Metadata["vscode_machineid"]`

### 5.2 请求级生成

- `X-Request-Id`：每请求生成（建议 UUIDv7）
- `X-Agent-Task-Id`：复制 `X-Request-Id`

### 5.3 会话/交互生成（持久化）

- `Vscode-Sessionid`
- `X-Interaction-Id`

规则：

- 二者使用同一生成策略与生命周期策略
- 二者值独立（不同 ID）
- 按与 `X-Initiator` 相同隔离键做持久化

### 5.4 固定/配置来源

- 固定：
  - `X-Interaction-Type=conversation-agent`
  - `X-Vscode-User-Agent-Library-Version=electron-fetch`
  - `Sec-Fetch-Site=none`
  - `Sec-Fetch-Mode=no-cors`
  - `Sec-Fetch-Dest=empty`
  - `Priority=u=4, i`
  - `Accept-Encoding=gzip, deflate, br, zstd`
- 配置：
  - `User-Agent`
  - `Editor-Version`
  - `Editor-Plugin-Version`
  - `Anthropic-Beta`（默认收敛单值）

### 5.5 判定表（可执行）

| Header | 来源 | 允许透传 | 缺失处理 |
|---|---|---|---|
| Editor-Device-Id | auth_metadata | 否 | 省略 |
| Vscode-Abexpcontext | auth_metadata | 否 | 省略 |
| Vscode-Machineid | auth_metadata | 否 | 省略 |
| X-Request-Id | computed(request) | 否 | 必有（生成） |
| X-Agent-Task-Id | computed(from X-Request-Id) | 否 | 必有（复制） |
| Vscode-Sessionid | computed(session-state) | 否 | 必有（生成/复用） |
| X-Interaction-Id | computed(session-state) | 否 | 必有（生成/复用） |
| X-Interaction-Type | constant | 否 | 固定写入 |
| X-Vscode-User-Agent-Library-Version | constant | 否 | 固定写入 |
| Sec-Fetch-Site | constant | 否 | 固定写入 |
| Sec-Fetch-Mode | constant | 否 | 固定写入 |
| Sec-Fetch-Dest | constant | 否 | 固定写入 |
| Priority | constant | 否 | 固定写入 |
| Accept-Encoding | constant | 否 | 固定写入 |
| User-Agent | config | 否 | 配置缺失回退 constant |
| Editor-Version | config | 否 | 配置缺失回退 constant |
| Editor-Plugin-Version | config | 否 | 配置缺失回退 constant |
| Anthropic-Beta | config | 否 | 配置缺失回退单默认值 |

## 6. Session / Interaction 状态机

输入：`initiator`（本地计算，不透传）、`bucketKey`

1. 计算 `initiator`：
   - user-only 对话 => `user`
   - 包含 assistant/tool => `agent`
2. 读取 `bucketKey` 下持久化状态：`session_id`, `interaction_id`
3. 分支：
   - 若 `initiator=user`：生成新 `session_id` + 新 `interaction_id`，并持久化覆盖
   - 若 `initiator=agent`：
     - 有已保存值：直接复用
     - 无已保存值：立即生成新值并持久化
4. 写入请求头：
   - `Vscode-Sessionid=session_id`
   - `X-Interaction-Id=interaction_id`

一致性规则（强约束）：

- `Vscode-Sessionid` 与 `X-Interaction-Id` 必须成对存在，禁止单边发送。
- 两者由同一函数一次性生成（事务化计算），确保“同策略不同值”。
- 任一字段计算失败时，两者都不写入，并记录 error 日志。

## 7. 隔离机制

沿用 `X-Initiator` 现有隔离思想：

- 隔离 identity：复用 `initiatorBypassIdentity(auth, apiToken)` 的 identity 语义
- bucket key：`model + hash(identity)`（避免原文暴露）
- 存储：独立状态文件（可与 bypass 文件并行管理，但结构分离）

并发与原子性：

- 进程内：按 bucket 使用互斥锁，避免同 bucket 并发读改写竞争。
- 跨进程：对“bucket 对应的 sidecar lock 文件”加文件锁，锁内执行完整临界区：`读 -> 改 -> 写(temp) -> rename`。
- 锁域隔离：主状态与 shadow 状态使用不同 state 文件路径 + 不同 lock 文件路径，禁止共享锁域。
- 持久化：使用“临时文件 + 原子 rename”写入，保证文件级原子替换。
- 失败策略：写入失败不阻断请求，但保留本次已生成 pair 继续发送（并记录 warning）。
- 下次请求若仍无持久化状态，按“无状态立即生成”规则再次生成。

锁超时降级规则（强约束）：

- 锁超时/读失败时，不读旧状态，直接一次性生成 `session_id` + `interaction_id` pair。
- 降级场景仍要求 pair 一致性：二者同发或同省略，禁止单边发送。

目标：同账号/同隔离域内稳定复用，不跨域污染。

## 8. 迁移策略

### 8.1 模式

- `legacy`：沿用旧逻辑
- `dual-run`：新旧并行计算，主用旧逻辑，记录 diff；新逻辑状态写入 shadow 存储，不影响主状态
- `strict`：只使用 Header 编译器

### 8.2 迁移步骤

1. 引入 `HeaderSpec` + `CompileHeaders`
2. 接入 `dual-run` 观测差异（header 值与来源）
3. 修正差异并冻结规则
4. 切换 `strict`
5. 删除旧透传路径与旧测试

## 9. 错误处理与降级

1. auth metadata 缺失（三项上下文字段）：不崩溃，**省略该 header** 并告警。
2. session/interaction 状态文件异常：
   - 读取失败：按“无状态”处理并重新生成
   - 写入失败：请求继续；记录 warning（不中断主链路）
3. 生成器异常：
   - 对 `Vscode-Sessionid` / `X-Interaction-Id`：按 pair 处理，二者要么同时成功写入，要么同时省略。
   - 对其他 header：仅影响对应字段，不影响请求核心认证头。

`dual-run` 副作用硬边界：

- 禁止写主状态（代码级 guard）。
- 仅允许写 shadow 命名空间。
- shadow 写失败不得影响主请求头与主状态。

ID 格式约束：

- `X-Request-Id` / `X-Agent-Task-Id` / `Vscode-Sessionid` / `X-Interaction-Id` 统一使用 UUIDv7 字符串。

## 10. 测试设计

1. 单测：
   - HeaderSpec 来源优先级
   - `X-Agent-Task-Id = X-Request-Id`
   - `user` 触发新生成并持久化
   - `agent` 复用/无状态新生成分支
2. 集成测试：
   - `dual-run` diff 输出正确（字段名、来源、值 hash、差异类型）
   - `strict` 下无入站透传泄漏
3. 回归测试：
   - 现有 `X-Initiator` 逻辑保持一致
   - messages/responses 两条路径都覆盖

## 11. 非目标（本次不做）

1. 不改上游 endpoint 选择逻辑（`/chat/completions`、`/responses`、`/v1/messages`）。
2. 不重构 payload 转换器。
3. 不变更认证令牌刷新策略。

## 12. 验收标准

1. Copilot 上游 header 全部可追溯来源。
2. 禁止未声明透传字段进入上游请求。
3. Session/Interaction 行为满足已确认规则。
4. `strict` 模式下测试全部通过。
5. `dual-run` 观测期连续 7 天无新增关键 diff，且无未声明透传告警。
6. metadata 缺失场景下，不允许发送空字符串 header。
