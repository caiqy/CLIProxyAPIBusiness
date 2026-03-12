# Copilot 模型映射统一到框架默认机制（Design）

## 1. 背景与问题

当前 GitHub Copilot 渠道存在两套并行机制：

1. 渠道默认注入别名（例如 `claude-opus-4.6 -> claude-opus-4-6`）
2. 管理员面板的通用模型映射（`model_mappings` / `oauth-model-alias`）

两套机制并行会导致行为来源不单一，出现“UI 与运行时认知不一致”的问题（例如：是否显示 alias、何时可用 alias、如何排障）。

本次目标是让 Copilot 完全收敛到框架默认映射机制，移除渠道专用默认注入逻辑。

---

## 2. 目标

1. Copilot 不再自动注入默认别名。
2. Copilot alias 仅由管理员面板配置的模型映射决定。
3. 白名单逻辑保持现状，不做任何改动。

用户侧约定：
- 管理员可手工配置：
  - `claude-opus-4.6 -> claude-opus-4-6`
  - `claude-sonnet-4.6 -> claude-sonnet-4-6`

---

## 3. 非目标（明确不做）

1. 不改 Auth Files 白名单逻辑与数据结构。
2. 不在白名单预设中展示 alias。
3. 不自动为 Copilot 生成或迁移默认映射。

---

## 4. 设计决策

### 4.1 单一真相源

Copilot 渠道模型映射的唯一来源为 `model_mappings`（经 watcher 组装成 `OAuthModelAlias`）。

### 4.2 运行时行为

运行时继续沿用现有框架流程：

1. 认证刷新时，将 `OAuthModelAlias` 下发给 core manager。
2. 请求执行前，按 channel 做 alias -> upstream 解析。
3. 上游调用使用原始模型名。

即：
- 有映射：`claude-opus-4-6` 可调用，执行时转换为 `claude-opus-4.6`。
- 无映射：`claude-opus-4-6` 不可调用（符合“纯手动配置”策略）。

### 4.3 兼容性策略

采用硬切换：删除默认注入后，不保留兜底。

---

## 5. 代码改动范围

仅改 Copilot 默认别名注入相关逻辑：

1. `third_party/CLIProxyAPIPlus/internal/config/config.go`
   - 移除 `github-copilot` 默认 `oauth-model-alias` 注入路径。

2. `third_party/CLIProxyAPIPlus/internal/config/oauth_model_alias_defaults.go`
   - 删除或停用 `defaultGitHubCopilotAliases` 及其仅用于默认注入的引用。

3. 对应测试调整：
   - 删除/改写“自动注入 github-copilot 默认 alias”的断言。
   - 保留并增强“用户显式配置 alias 时可生效”的断言。

不改动：
- `internal/http/api/admin/handlers/auth_files.go`（白名单）
- Provider API Keys 白名单相关逻辑

---

## 6. 验收标准

### 6.1 功能验收

1. 未配置 Copilot 模型映射时：
   - `claude-opus-4-6` 请求失败（无可用模型/认证）。

2. 配置映射后：
   - `claude-opus-4-6` 请求成功。
   - 上游接收到 `claude-opus-4.6`。

3. 白名单行为保持不变：
   - 现有测试全部通过。

### 6.2 回归验收

1. 非 Copilot 渠道 alias 行为不回归。
2. `oauth-model-alias` 管理接口行为不回归。

---

## 7. 风险与应对

### 风险
移除默认注入后，历史依赖默认 alias 的环境会立即失效。

### 应对
发布说明明确要求先在管理员面板配置映射；并提供两条标准映射示例。

---

## 8. 实施前置条件

1. 管理员面板已支持对 `github-copilot` 配置模型映射。
2. 运营/使用方知晓硬切换行为。

---

## 9. 总结

本方案通过“删除 Copilot 默认注入、完全依赖通用映射”实现架构收敛：

- 行为来源单一
- 排障路径清晰
- 与框架默认机制一致

同时遵循本次约束：白名单逻辑保持现状不改。
