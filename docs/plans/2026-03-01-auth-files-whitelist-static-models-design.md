# Auth Files 白名单改为静态模型源设计

## 背景

当前 Auth Files 白名单预置模型依赖运行时动态模型注册（`GlobalModelRegistry().GetAvailableModelsByProvider(...)`）。这会导致在认证失效、网络抖动、上游不可用时，管理端出现“该认证类型的提供方模型暂时不可用”，从而影响白名单配置操作。

本次目标是将 Auth Files 白名单模型来源从动态注册表切换为静态模型定义，并按同一原则覆盖 Provider API Keys 相关模型预置能力。

## 目标

1. 将 Auth Files 白名单 universe 统一改为静态来源，不再依赖运行时动态注册状态。
2. Provider API Keys 相关模型预置能力同步改为静态来源，避免模块间语义漂移。
3. 保持现有 reason code 兼容：当静态列表为空时仍返回 `provider_models_unavailable`。
4. 保持保存时转换语义不变：`excluded_models = universe - allowed_models`。

## 非目标

1. 不调整现有白名单字段结构（`whitelist_enabled/allowed_models/excluded_models`）。
2. 不改变运行时真实请求的可用性判断逻辑（仅改变“预置模型来源”）。
3. 不引入新的策略引擎或跨模块重构。

## 方案对比

### 方案 A：Business 侧维护静态表
- 优点：实现快。
- 缺点：与 `CLIProxyAPIPlus` 双份维护，长期一致性风险高。

### 方案 B：在 CLIProxyAPIPlus 暴露静态模型公共接口（采用）
- 优点：单一真相源、跨模块一致、维护成本低。
- 缺点：需要在 submodule 暴露稳定 SDK API 并补测试。

### 方案 C：静态优先 + 动态兜底
- 优点：兼容旧行为。
- 缺点：复杂度上升，且与“去动态方案”目标不一致。

## 总体设计

### 1) 静态源单一真相

在 `third_party/CLIProxyAPIPlus` 中基于现有静态模型定义，新增 `sdk/cliproxy` 公共只读接口（按 provider 返回静态模型 ID 列表）。

要求：
- 输入 provider 做 `trim + lower` 规范化。
- 输出列表去重、过滤空值、按字典序排序。
- unknown provider 返回空列表。

### 2) Business 侧统一接入

在 `cpab` 中将以下链路统一切换到静态 loader：

1. Auth Files `GET /v0/admin/auth-files/model-presets`
2. Auth Files Create/Update/Import 冲突路径中的 whitelist universe 计算
3. Provider API Keys 相关模型预置读取路径

切换后不再使用运行时 `GetAvailableModelsByProvider(...)` 作为预置模型来源。

### 3) 错误与兼容语义

保持 reason code 协议稳定：

- `unsupported_auth_type`：provider 不在支持集合
- `provider_models_unavailable`：provider 支持但静态列表为空

接口继续返回 `reason`（兼容）+ `reason_code`（推荐前端映射）。

### 4) 行为变化（已确认）

改造后即使当前没有可用认证、没有运行时模型注册，也应当可以展示并保存白名单预置模型。

## 数据流

`type/provider -> canonical provider -> static universe loader ->` 

- 若有模型：返回 `supported=true` + `models[]`，保存时据此计算 `excluded_models`
- 若为空：返回 `supported=false` + `reason_code=provider_models_unavailable`

## 测试设计

### A. CLIProxyAPIPlus（静态接口）

1. provider 返回列表非空（覆盖主要 provider，包括 antigravity）
2. unknown provider 返回空
3. 输出稳定性（排序/去重/空值过滤）

### B. Auth Files（cpab）

1. `ListModelPresets` 支持 provider 返回静态列表
2. 支持但静态空列表时返回 `provider_models_unavailable`
3. Create/Update/Import 冲突重算均基于静态 universe，差集计算正确

### C. Provider API Keys（cpab）

1. 模型预置读取路径改为静态源
2. 与 Auth Files 对同 provider 返回集合一致

## 风险与缓解

1. **静态定义过期风险**：通过单一真相源（CLIProxyAPIPlus）和测试矩阵降低漂移风险。
2. **行为变化感知风险**：在变更说明中明确“预置模型与运行时可用性解耦”。
3. **跨模块改动风险**：先补 SDK 接口单测，再做业务接入回归，分层验证。

## 验收标准

1. Auth Files 与 Provider API Keys 均不再依赖动态注册表提供预置模型。
2. antigravity 在运行时未注册模型时，预置模型仍可展示。
3. reason code 兼容稳定，静态空列表仍为 `provider_models_unavailable`。
4. 保存时白名单转换结果正确且可回归。
