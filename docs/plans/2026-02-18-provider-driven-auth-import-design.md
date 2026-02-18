# Provider 驱动的认证文件导入弹窗设计

## 背景

当前管理员面板虽有 `Import From CLIProxyAPI`，但不能显式指定 provider，也无法按 provider 展示真实 JSON 示例，导致导入前预期不清晰、导入后排错成本高。

本次目标是新增一套 **Provider 驱动** 的导入能力：

- 从 `New` 菜单进入新导入弹窗。
- 支持文件导入与文本导入。
- 按 provider 展示真实示例并进行对应校验。
- 不复用旧导入链路语义（保留旧入口仅做兼容，不作为本功能路径）。

## 目标与非目标

### 目标

1. 在 AuthFiles 页 `New` 菜单新增独立入口：`Import Auth Files (Provider)`。
2. 新增导入弹窗，支持：
   - Provider 选择
   - 文件导入（JSON）
   - 文本导入（JSON）
   - Provider 示例查看与一键填充
3. 新增后端专用接口：`POST /v0/admin/auth-files/import-by-provider`。
4. 按 provider 做强校验，返回逐条失败原因。
5. 校验与入库遵循“仅取用和验证需要字段”。
6. `key` 与 `type` 由系统自动生成，不要求用户提供。

### 非目标

1. 本期不下线旧 `Import From CLIProxyAPI` 接口。
2. 本期不做后端动态下发示例（示例先前端内置）。
3. 本期不做导入向导页或独立路由页。

## 入口与页面布局

### 入口

- 位置：`New` 下拉菜单。
- 新增菜单项：`Import Auth Files (Provider)`。
- 权限：沿用导入权限控制（无权限不展示入口）。

### 弹窗结构（沿用 Admin 面板风格）

1. 顶部：标题 + 关闭按钮。
2. 固定参数区：
   - `Provider` 下拉（必选）
   - `Auth Group` 选择（可选，默认组兜底）
3. 主体区 Tabs：
   - 文件导入
   - 文本导入
   - Provider 示例
4. 底部操作：`Cancel` / `Import`。
5. 结果区：成功计数 + 失败明细（可滚动）。

## 方案选型

### 方案 A（采用）：新增专用后端接口 + 新弹窗流程

- 优点：provider 语义完整、校验准确、前端示例可与解析器强绑定。
- 缺点：后端需新增处理逻辑与权限定义。

### 方案 B：仅前端改造复用旧接口

- 优点：改动小。
- 缺点：无法在后端做 provider 强校验，风险较高。

结论：采用方案 A。

## 数据契约与数据流

### 新接口

- `POST /v0/admin/auth-files/import-by-provider`

请求体（建议）：

```json
{
  "provider": "codex",
  "source": "text",
  "auth_group_id": [1, 2],
  "entries": [
    { "access_token": "xxx", "base_url": "https://..." }
  ]
}
```

响应体：

```json
{
  "imported": 1,
  "failed": [
    { "index": 2, "error": "missing key" }
  ]
}
```

### 导入流程

1. 用户在弹窗选择 provider。
2. 用户通过文件或文本输入 JSON。
3. 前端将内容标准化为 `entries[]`，附带 `provider` 提交新接口。
4. 后端按 provider 解析器逐条校验与入库。
5. 返回 `imported + failed[]`，前端展示结果并可继续导入或关闭刷新。

## 校验与字段策略（关键）

遵循你确认的原则：**仅取用和验证需要字段**。

### `key` / `type` 自动生成

- `type`：按 provider 自动写入 canonical 值（如 `anthropic -> claude`、`gemini-cli -> gemini`、`iflow-cookie -> iflow`）。
- `key`：优先使用 `provider + email`（email 规范化为 trim + lowercase）。
- 若 email 缺失：回退 `provider + 核心凭据哈希`（稳定幂等，不阻断导入）。
- 用户输入中的 `key`、`type` 不参与入库。

### 校验分层

1. 基础层：`provider/source/entries` 必填与类型。
2. 结构层：`entries` 必须是对象数组。
3. Provider 层：仅验证该 provider 所需字段（必填、类型、枚举/格式）。
4. 业务层：自动 key 生成、auth group 合法性、冲突更新策略。

### Provider 最小必需字段（按真实执行代码对齐）

- `codex`：`access_token` 必填（`refresh_token` 为刷新增强字段）。
- `anthropic/claude`：`access_token` 必填（`refresh_token` 为刷新增强字段）。
- `qwen`：`access_token` 必填（`refresh_token` 为刷新增强字段）。
- `antigravity`：`access_token` 必填（`refresh_token` 为刷新增强字段）。
- `kiro`：`access_token` 必填（`refresh_token` 为刷新增强字段；社交/IDC 附加字段可选）。
- `gemini`：`access_token` 或 `token.access_token` 二选一。
- `iflow`：三模式命中其一即通过：
  - A：`api_key`；
  - B：`cookie + email`；
  - C：`refresh_token`（`access_token` 可选）。

说明：`email` 全局可选，仅在展示归因与 iFlow-cookie 模式下具有功能性要求。

### 非必要字段处理

- 不报错。
- 不入库主结构（直接忽略）。
- 入库内容只保留规范化后的必要字段（canonical shape）。

该策略兼顾容错与稳定，避免外部冗余字段污染平台数据。

## Provider 示例策略

- 示例来源：前端内置模板。
- 每个 provider 一份“真实可用”最小示例。
- 示例不包含 `key`、`type`（由后端自动生成）。
- 示例支持：
  - 复制
  - 一键填充到“文本导入”Tab
- 切换 provider 时，示例与校验提示同步切换。

## 错误处理与交互细节

1. 前端提交前：
   - JSON 语法错误即时提示。
   - provider 未选、空内容、非对象/数组直接拦截。
2. 后端返回后：
   - 请求级错误：顶部错误框。
   - 条目级错误：结果区逐条展示（条目序号/文件名 + 原因）。
3. 切换 provider 时：若文本区已有内容，先确认再重校验。
4. 支持部分成功：失败条目不影响成功条目落库。

## 后端改造点

1. `internal/http/api/admin/admin.go`
   - 新增路由：`POST /v0/admin/auth-files/import-by-provider`
2. `internal/http/api/admin/permissions/permissions.go`
   - 新增权限定义。
3. `internal/http/api/admin/handlers/auth_files.go`
   - 新增 provider 导入 handler。
   - 新增 provider 解析与规范化逻辑（可按函数映射拆分）。

## 前端改造点

1. `web/src/pages/admin/AuthFiles.tsx`
   - `New` 菜单新增入口。
   - 新增导入弹窗状态与交互。
2. 新增/拆分组件（建议）：
   - Provider 选择区
   - 文件导入 Pane
   - 文本导入 Pane
   - 示例 Pane
3. `web/src/locales/*.ts`
   - 增加导入相关 i18n 文案。

## 测试方案

### 后端

1. 接口权限测试（有权限/无权限）。
2. provider 必填字段校验测试。
3. 冗余字段忽略测试（不报错且不入库）。
4. 批量部分成功测试。
5. 冲突更新与默认分组逻辑测试。

### 前端

1. `New` 菜单入口可见性测试。
2. Provider 切换触发示例切换测试。
3. 文本 JSON 语法校验测试。
4. 文件导入与文本导入提交流程测试。
5. 结果区成功/失败渲染测试。

## 验收标准

1. 可从 `New` 菜单打开新的 provider 导入弹窗。
2. 可在同一弹窗内完成文件导入与文本导入。
3. 每个 provider 可查看真实示例并可一键填充。
4. 导入按 provider 做强校验并返回逐条错误。
5. 非必要字段不阻断导入，且不会污染入库结构。
6. 用户无需提供 `key`、`type`，系统可自动生成并稳定幂等更新。
