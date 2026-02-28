# Auth Files 模型白名单（预置模型）设计文档

## 背景

当前系统在运行时存在按模型过滤认证的能力（通过注册到模型注册表后执行 `ClientSupportsModel` 对比），但管理员面板的 **Auth Files** 页面没有可配置的 per-auth 模型白名单/排除能力。

用户目标是：

1. 在 **Auth Files** 页面提供白名单开关与模型勾选。
2. 模型列表使用后端“预置模型列表接口”自动加载。
3. 使用 `content.type` 自动识别 provider。
4. 白名单开启且允许列表为空时，语义为“该认证禁止所有模型”。
5. 若 provider 不支持白名单，UI 禁用并展示原因。

---

## 已确认决策

### 方案选择

采用 **方案 A：保存时转换为 excluded**。

### 模型来源

采用 **后端新接口返回预置模型列表**（不走 model-mappings available-models）。

### Provider 识别

采用 **按 `content.type` 自动识别**。

### 空白名单语义

白名单开启且 `allowed_models=[]` 时，**禁止该认证所有模型**。

### 不支持 provider 行为

在 Auth Files 页面 **禁用白名单开关并提示原因**。

---

## 总体架构

### 1) Auth 数据模型扩展

为 `auth` 增加以下字段：

- `whitelist_enabled`（bool，默认 false）
- `allowed_models`（json）
- `excluded_models`（json）

语义：

- `whitelist_enabled=false`：不启用白名单限制。
- `whitelist_enabled=true`：`excluded_models` 由后端在保存时根据预置模型全集计算。

### 2) 预置模型接口

新增 Admin 接口（示例）：

- `GET /v0/admin/auth-files/model-presets?type=<content.type>`

返回：

- `provider`：由 `type` 解析得到
- `supported`：是否支持白名单
- `reason`：不支持时原因
- `models[]`：预置模型全集

### 3) 保存时转换

Create/Update Auth Files 时：

1. 解析 `content.type -> provider`
2. 校验 provider 是否支持白名单
3. 拉取预置模型全集 `universe`
4. 校验 `allowed_models` 是否都在 `universe`
5. 计算 `excluded_models = universe - allowed_models`
6. 持久化 `whitelist_enabled/allowed_models/excluded_models`

### 4) 运行时生效路径

watcher 将 `auth.excluded_models` 注入 runtime auth（`attributes["excluded_models"]`），由现有模型注册与候选过滤链路生效：

- `registerModelsForAuth` 应用排除模型
- 选路时 `ClientSupportsModel(candidate.ID, model)` 再过滤

---

## 前端（Auth Files 页面）设计

在新建/编辑弹窗增加“模型白名单”区域：

1. `Whitelist mode` 开关
2. `Allowed models` 多选（自动加载预置模型）
3. `Excluded models (auto)` 可选只读预览

交互规则：

- 当 `content.type` 变化：自动重新加载预置模型
- 若 `supported=false`：
  - 开关禁用
  - 显示 `reason`
- 白名单开启允许空列表（表示全禁）
- 白名单关闭：不再应用 `allowed/excluded` 规则

---

## 错误语义

- 无法识别 type/provider：`400 invalid auth type`
- provider 不支持白名单：`400 whitelist not supported for provider ...`
- `allowed_models` 包含未知模型：`400 unknown model: ...`
- 预置模型加载失败：前端禁用白名单并提示“模型预置暂不可用”

---

## 迁移与兼容

数据库迁移：

- `whitelist_enabled BOOLEAN NOT NULL DEFAULT false`
- `allowed_models JSON NULL`
- `excluded_models JSON NULL`

兼容策略：

- 历史 Auth 默认不启用白名单，不改变现有行为
- 仅管理员开启后生效

---

## 测试策略

### 后端

1. Create/Update + whitelist 开启 + allowed 非空 => excluded 正确
2. Create/Update + whitelist 开启 + allowed 空 => excluded=universe
3. unknown model => 400
4. unsupported provider => 400
5. List/Get 回传三字段

### 集成

1. watcher 注入 `excluded_models` 到 runtime auth
2. 选路阶段按模型过滤生效
3. 不可用场景返回与当前系统一致的错误语义

### 前端

1. type 切换自动加载预置模型
2. unsupported 时禁用并提示
3. payload 在开关开/关状态下字段正确

---

## 风险与后续

### 已知风险

采用“保存时快照”后，如果预置模型后续新增，已有 Auth 的 `excluded_models` 需要重算才会覆盖新增模型。

### 建议后续增强

1. 提供“按 provider 批量重算 Auth 白名单”管理操作。
2. 增加审计日志（记录 allowed/excluded 变化）。

---

## 结论

该设计在不破坏现有选路架构的前提下，为 Auth Files 提供端到端可配置白名单能力，满足：

- 后端预置模型自动加载；
- `content.type` 自动识别 provider；
- 空白名单全禁；
- 不支持 provider 时禁用并提示原因。
