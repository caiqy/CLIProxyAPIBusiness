# Auth Files 白名单修复加固设计

## 背景

当前 Auth Files 白名单主流程已具备，但在代码评审中发现 3 类重要风险：

1. provider 支持判定与模型 universe 查询存在归一化语义不一致，可能导致“声明支持但实际不支持”；
2. 前端编辑弹窗加载预置模型存在异步竞态，可能错误覆盖编辑态；
3. Import 覆盖（同 key）路径未同步白名单字段，可能出现 content 与 whitelist 语义漂移。

本设计面向架构一致性与项目质量，目标是在不改变既有 API 主语义的前提下，完成“统一能力判定 + 状态一致性 + 可测试回归”。

## 目标

1. Auth Files 白名单能力判定在后端形成单一真相源；
2. Import 覆盖路径与 Create/Update 保持一致的数据语义；
3. 前端编辑态在快速切换场景下保证状态隔离和结果正确；
4. 通过覆盖关键边界的测试矩阵，将同类回归风险前移到 CI。

## 非目标

1. 不改动 Provider API Keys 模块已有 provider 语义；
2. 不新增复杂策略引擎或跨模块大规模重构；
3. 不调整现有路由路径与权限模型。

## 方案对比

### 方案 A：最小补丁
- 仅在各点位修复问题。
- 优点：快；缺点：规则继续分散，长期维护成本高。

### 方案 B：能力收口（采用）
- 在 Auth Files 维度收口 provider 规范化、支持判定、reason code 与模型 universe 逻辑；
- Create/Update/ListModelPresets/Import 覆盖统一复用；
- 前端并发安全与 i18n 一并标准化。
- 优点：一致性好、风险可控、增量成本适中。

### 方案 C：策略引擎化
- 优点：扩展性强；缺点：当前需求下过度设计。

## 详细设计

### 1) 后端能力收口

在 Auth Files handler 内建立统一 helper（可同文件或拆分文件）：

- `canonicalAuthFileProvider(...)`：统一从 type/provider 推导 canonical provider；
- `supportsAuthFileWhitelistProvider(...)`：基于 Auth Files 支持集合判定；
- `authFilePresetReasonCode(...)`：输出稳定 reason code；
- `computeAuthFileWhitelistFields(...)`：继续作为保存时转换入口。

关键点：
- 不再将 Auth Files 的 provider 支持判定绑定到 `normalizeProvider`（API Keys 专用语义）；
- 模型 universe 查询继续使用 `providerUniverseLoader(strings.ToLower(strings.TrimSpace(provider)))`；
- 对外响应增加 `reason_code`，并保留 `reason` 兼容字段。

### 2) Import 覆盖一致性策略

当同 key 导入触发 `OnConflict DoUpdates` 时，新增白名单一致性处理：

- 读取旧记录 whitelist 状态与 allowed；
- 基于新 content provider 计算新 universe；
- 若旧 `whitelist_enabled=false`：写入关闭并清空 allowed/excluded；
- 若旧 `whitelist_enabled=true`：
  - 计算 `oldAllowed ∩ newUniverse`；
  - 交集非空：开启白名单并重算 excluded；
  - 交集为空：自动兜底关闭白名单并清空 allowed/excluded；
- 若 universe 不可用：同样兜底关闭并清空，避免误全禁。

该策略与用户确认一致：**自动重算并兜底**。

### 3) 前端竞态与状态隔离

在 `AuthFiles.tsx` 编辑弹窗中引入“仅最新请求生效”机制：

- 使用请求序号（或 AbortController）标记请求代次；
- 仅当响应代次等于当前代次时更新 `editWhitelistSupported/editPresetModels/editAllowedModels/editWhitelistReason/editWhitelistLoading`；
- `authType` 为空时也显式结束 loading；
- 关闭弹窗或切换对象时使旧请求失效。

### 4) i18n 与接口兼容

`GET /v0/admin/auth-files/model-presets` 返回：

- `supported`
- `provider`
- `reason_code`（新）
- `reason`（兼容）
- `models`

前端优先映射 `reason_code` 到本地化文案，`reason` 仅兜底显示。

## 测试设计

### 后端

1. provider 支持矩阵测试：覆盖 `antigravity/qwen/kiro/kimi/github-copilot/kilo/iflow`；
2. `ListModelPresets`：支持/不支持/无模型 三类返回（含 `reason_code`）；
3. Import 覆盖：
   - 旧 whitelist=true 且有交集；
   - 旧 whitelist=true 且无交集（兜底关闭）；
   - universe 不可用（兜底关闭）；
4. 回归现有 Create/Update/Get/List/Watcher 测试。

### 前端

1. 竞态回归：快速切换编辑对象，仅最新请求生效；
2. 空 authType 分支：loading 正确复位；
3. reason_code 映射与 fallback 文案。

## 风险与缓解

1. **兼容风险**：新增 `reason_code` 但保留 `reason`，前后端可渐进演进；
2. **导入性能风险**：冲突路径增加一次旧值读取与计算，保持单条导入粒度，影响可接受；
3. **行为变更风险**：provider 变更后 whitelist 可能被兜底关闭，通过测试与变更说明明确预期。

## 验收标准

1. 支持 provider 上白名单可正常开启并计算 excluded；
2. Import 覆盖后不出现 whitelist 与 content 语义漂移；
3. 前端快速切换编辑对象不再出现状态串写；
4. 后端受影响包测试、前端目标测试与 build 全通过。
