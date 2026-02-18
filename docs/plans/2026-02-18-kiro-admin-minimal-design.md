# Kiro 在管理员平台最小可用接入设计

## 背景与问题

当前代码库中，`third_party/CLIProxyAPIPlus` 已具备 Kiro 认证能力（含 `RequestKiroToken` 与 `/kiro-auth-url` 相关实现），但业务层管理员接口与前端管理页尚未暴露该能力。结果是：底层可用、平台不可用，管理员无法像现有 Codex/Anthropic/Gemini 等 provider 一样在 AuthFiles 页面发起 Kiro 认证。

本设计目标是在 **不做大重构** 的前提下，沿用现有 provider 惯例，交付 Kiro 的最小可用接入（MVP）。

## 目标与非目标

### 目标

1. 管理端可通过 AuthFiles 入口发起 Kiro 认证。
2. 新增独立权限：`POST /v0/admin/tokens/kiro`。
3. 后端统一使用 `POST /v0/admin/tokens/kiro`，默认走 Kiro AWS Builder ID（device code）流程。
4. 前端在弹窗中展示设备码信息（`verification_url` + `user_code`），并复用现有轮询流程直至成功/失败。

### 非目标

1. 本期不做 Kiro Google/GitHub 完整社交流程 UI。
2. 本期不重构现有 AuthFiles 全部认证状态机。
3. 本期不新增独立 Kiro 页面。

## 方案选型与取舍

### 方案 A（推荐）：增量接入，沿用现有惯例

- 在现有 AuthFiles 流程上增量补 Kiro。
- 优点：改动面小、上线快、回归风险低、与现有管理员使用习惯一致。
- 缺点：认证流程抽象层次不提升，后续 provider 增长时仍有局部重复。

### 方案 B：中度重构后再接入

- 先抽象统一认证状态机，再迁移现有 provider 与 Kiro。
- 优点：结构更统一、扩展性更强。
- 缺点：超出 MVP 范围，周期长、联调成本高。

### 方案 C：Kiro 独立入口

- 单独页面或独立弹窗流程，不复用现有入口。
- 优点：Kiro 逻辑隔离明显。
- 缺点：体验不一致、维护两套入口、偏离“沿用惯例”。

最终选择：**方案 A**。

## 架构与组件设计

沿用现有 provider 接入链路：

1. **后端路由层**（`internal/http/api/admin/admin.go`）
   - 新增 `POST /v0/admin/tokens/kiro`。
   - 通过 `sdkapi.NewManagementTokenRequester(...)` 调用 `RequestKiroToken`。
   - 与现有 OAuth provider 一样保留 `is_webui/callback_host` 默认参数策略（即便 MVP 主走 device code，保持接口风格一致）。

2. **权限定义层**（`internal/http/api/admin/permissions/permissions.go`）
   - 新增权限定义：`POST /v0/admin/tokens/kiro`，归类到 `Auth Tokens`。

3. **SDK 暴露层**（`third_party/CLIProxyAPIPlus/sdk/api/management.go`）
   - 若当前导出接口未包含，补充 `RequestKiroToken(*gin.Context)`，与其他 provider 方法并列。

4. **前端入口层**（`web/src/pages/admin/AuthFiles.tsx`）
   - `AUTH_TYPES` 新增 `{ key: 'kiro', label: 'Kiro', endpoint: '/v0/admin/tokens/kiro' }`。
   - 按权限动态展示，行为与现有 provider 保持一致。

## 数据流与状态机

### 主流程

1. 点击 AuthFiles 的 Kiro 菜单项。
2. 前端 `POST /v0/admin/tokens/kiro`，获取 `state`。
3. 前端进入轮询：`POST /v0/admin/tokens/get-auth-status?state=...`。
4. 若状态为 `device_code`：展示设备码卡片并继续轮询。
5. 状态为 `ok`：关闭弹窗、提示成功、应用默认分组、刷新列表。
6. 状态为 `error`：显示错误信息并停止轮询。

### 前端类型兼容

扩展 `AuthStatusResponse`：

- `status`: `'ok' | 'wait' | 'error' | 'device_code' | 'auth_url'`（`auth_url` 预留向前兼容）
- `error?`
- `verification_url?`
- `user_code?`
- `url?`

说明：`device_code` 是 Kiro MVP 必需；`auth_url` 为未来 social 模式预留，不要求本期完整 UI。

## UI 与交互细节

在现有认证弹窗中增加 Kiro 设备码区块（仅 `authTypeKey==='kiro'` 且响应返回设备码信息时展示）：

1. `verification_url` 展示 + 打开链接按钮。
2. `user_code` 高亮展示 + 复制按钮。
3. 保留“轮询中”状态提示（与现有成功/失败 toast 机制一致）。

退化策略：

- 若仅拿到 `status=device_code` 但字段缺失，显示通用提示（请在外部完成授权）并继续轮询，不中断流程。

## 错误处理与边界

1. 启动阶段失败（`/tokens/kiro` 报错）：前端提示“无法启动认证流程”，不影响其他 provider。
2. 轮询返回 `error`：展示后端错误并停止轮询。
3. 单次轮询网络错误：记录日志，下一周期继续轮询（避免瞬时波动导致误失败）。
4. 关闭弹窗时：停止轮询并清理 Kiro 临时状态，避免内存泄露与状态串扰。

## 测试方案

### 后端

1. 路由可达：`POST /v0/admin/tokens/kiro` 在已登录且有权限时返回预期结构。
2. 权限校验：无权限 403，未登录 401。
3. requester 映射：管理员路由层能正确调到 management 的 Kiro 方法。

### 前端

1. 权限满足时 Kiro 出现在新建认证菜单。
2. 点击后成功发起 `/tokens/kiro` 并开始轮询。
3. 收到 `device_code` 时，设备码卡片正确渲染并可复制。
4. 轮询 `ok` 时：关闭弹窗、展示成功、刷新列表、执行分组应用。
5. 轮询 `error` 时：错误可见且轮询停止。
6. 对至少一个已有 provider 做冒烟，确保无回归。

## 验收标准

1. 管理员平台 AuthFiles 可见并可触发 Kiro 认证入口（受独立权限控制）。
2. Kiro device code 信息可在弹窗展示，轮询链路可达成功闭环。
3. 接入不破坏现有 provider 认证流程。
4. 代码改动保持最小化，符合现有风格与权限模型。

## 实施清单（建议顺序）

1. 补 SDK 暴露层 `RequestKiroToken`（如需）。
2. 注册 `POST /v0/admin/tokens/kiro` 路由。
3. 增加权限定义。
4. AuthFiles 增加 Kiro provider 入口。
5. 扩展轮询响应类型与 `device_code` 分支。
6. 新增设备码卡片 UI。
7. 执行后端/前端测试与手工联调。
