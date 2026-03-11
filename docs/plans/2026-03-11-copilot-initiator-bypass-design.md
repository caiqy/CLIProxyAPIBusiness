# GitHub Copilot X-Initiator 周期性放行设计（model + Copilot Token）

## 1. 目标与边界

### 1.1 目标

在现有 `force-agent-initiator` 行为基础上，新增“**滚动窗口内严格一次不强制注入**”能力，以降低上游长期看到 `X-Initiator: agent` 的风险。

已确认规则：

1. 分桶维度：`model + 上游 Copilot Token`
2. 计时方式：滚动窗口（可配置时长）
3. 语义：每个窗口严格一次放行（不注入 fake assistant）
4. 仅当请求原本会是 `user` 时才参与命中；原本就是 `agent` 的请求不计入机会
5. 状态需要持久化，重启后继续生效
6. 部署形态：单实例

### 1.2 非目标

- 不做多实例分布式一致性
- 不新增管理面板开关
- 不改变 `X-Initiator` 的判定机制（仍由 payload 角色本地推导）

## 2. 方案对比

### 方案 A（采纳）

**内存热态 + 本地 JSON 持久化（原子写盘）**

- 内存维护 `bucketKey -> nextEligibleAt`
- 命中放行后写盘
- 启动时加载状态文件

优点：
- 满足单实例场景，依赖少，落地快
- 风险面小，回滚简单

代价：
- 高频写盘下有 IO 压力（可后续做合并写优化）

### 方案 B（不采纳）

嵌入式 DB（SQLite/BoltDB）持久化。

不采纳原因：
- 复杂度高于当前需求
- 对单实例收益有限

### 方案 C（不采纳）

外部 Redis 等集中状态存储。

不采纳原因：
- 当前明确单实例，不需要引入外部依赖

## 3. 配置与状态模型

## 3.1 新增配置（建议）

```yaml
github-copilot:
  force-agent-initiator: true
  fake-assistant-content: "OK."

  force-agent-initiator-bypass:
    enabled: true
    window: 1h
    state-file: /CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json
```

字段语义：

- `enabled`：是否启用周期性放行
- `window`：滚动窗口时长（`time.Duration`）
- `state-file`：状态文件路径

### 3.2 状态文件结构（JSON）

```json
{
  "version": 1,
  "buckets": {
    "<model>|<sha256(copilot_token)>": {
      "next_eligible_at_unix": 1770000000,
      "updated_at_unix": 1769999900
    }
  }
}
```

安全约束：

- 不明文落盘 token，仅存 `sha256(token)`

## 4. 请求决策流程

在 `Execute` / `ExecuteStream` 中统一执行如下流程：

1. 若 `force-agent-initiator=false`：不进入该逻辑
2. 计算 `hasAgentRole = containsAgentConversationRole(body)`
3. 若 `hasAgentRole=true`：保持现有行为，不消耗放行机会
4. 若 `hasAgentRole=false`：进入 bypass 判定
   - 计算 bucket：`model + sha256(upstreamCopilotToken)`
   - 读取 `nextEligibleAt`
   - `now >= nextEligibleAt`：本次放行（不注入 fake assistant），并更新 `nextEligibleAt = now + window`
   - 否则：执行注入（维持 `agent`）

> 放行“严格一次”由“判定+更新”原子临界区保证。

## 5. 并发、异常与恢复

### 5.1 并发控制

- 使用进程内互斥锁保护状态 map
- “检查是否可放行 + 更新下一次时间”在同一临界区完成

### 5.2 持久化策略

- 启动时加载 `state-file`（不存在则空状态）
- 写盘采用 `tmp -> fsync -> rename` 原子替换，避免半写损坏

### 5.3 失败降级

- 写盘失败：记录 WARN/ERROR，当前请求不中断（以内存态继续）
- 读盘失败或格式损坏：记录错误并以空状态启动（fail-open）

### 5.4 清理策略

- 可在写盘时顺带清理长期未更新 bucket，控制文件增长

## 6. Docker 路径设计（已确认）

按“容器工作目录”要求，状态文件放在：

- 容器内：`/CLIProxyAPIBusiness/data/copilot_initiator_bypass_state.json`
- 宿主机：`./data/cpab/copilot_initiator_bypass_state.json`

`docker-compose.yml` 需为 `cpab` 服务增加挂载：

```yaml
services:
  cpab:
    volumes:
      - ./data/cpab:/CLIProxyAPIBusiness/data
```

## 7. 测试与验收

### 7.1 单元测试

1. bucket 维度正确（model + token hash）
2. 滚动窗口内严格一次放行
3. `hasAgentRole=true` 时不计入窗口
4. `window` 边界条件（0、负值、极小值）
5. 读盘损坏/写盘失败的降级行为

### 7.2 集成测试

1. `Execute`：首个 user-only 请求放行，后续请求注入
2. `ExecuteStream`：行为一致
3. 重启恢复：加载旧状态后仍遵守窗口

### 7.3 验收标准

1. 开启 bypass 后，同一 `model+token` 在窗口内恰好 1 次不注入
2. 原本 agent 请求不占用这 1 次机会
3. 重启后窗口状态延续
4. 未开启 bypass 时行为与现状一致
