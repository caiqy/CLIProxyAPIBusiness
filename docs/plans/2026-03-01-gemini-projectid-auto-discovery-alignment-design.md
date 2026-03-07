# Gemini 默认 project_id 获取逻辑对齐 AIClient 设计

## 背景

当前 `cpab` 在 Gemini CLI 鉴权流程中，未传 `project_id` 时会先调用 `fetchGCPProjects` 拉取项目列表并默认取第一个项目；随后在默认分支也会执行 `checkCloudAPIIsEnabled`（`serviceusage` 校验/启用）。

该行为与 `AIClient-2-API` 的“未显式提供项目时走 `loadCodeAssist/onboardUser` 自动发现项目”不一致，且容易出现“默认选中非目标项目”导致的 Cloud API 校验失败。

## 目标

1. 将 `cpab` 的“未传 `project_id`”默认分支改为 AIClient 式 auto-discovery。
2. 默认分支不再执行 `checkCloudAPIIsEnabled`。
3. 显式 `project_id` 分支保持当前行为与兼容性（继续校验）。
4. 保持现有接口结构与前端调用方式不变。

## 非目标

1. 不重构 `ALL`/`GOOGLE_ONE` 语义。
2. 不调整 AuthFiles 前端交互协议（仍允许不传 `project_id`）。
3. 不改 token 存储结构与 `metadata.project_id` 字段名。

## 方案对比

### 方案 A：最小侵入改造（采用）
- 做法：仅修改默认分支（`project_id` 为空）的项目解析与校验路径。
- 优点：改动面小、回归范围可控、满足目标。
- 缺点：分支策略仍分散在现有流程中。

### 方案 B：抽象统一策略层
- 做法：新增统一 `resolveProjectID + checkPolicy` 入口，所有分支收敛。
- 优点：长期维护性更好。
- 缺点：本次改动过大，不符合“最小变更”要求。

### 方案 C：全面 AIClient 化
- 做法：显式/默认/特殊分支统一 discovery 主路径，弱化或移除现有校验。
- 优点：行为统一。
- 缺点：兼容风险高，不符合“显式分支保留现状”。

## 总体设计

### 1) 默认分支项目解析改为 discovery

位置：`third_party/CLIProxyAPIPlus/internal/api/handlers/management/auth_files.go`

- 在 `ensureGeminiProjectAndOnboard(...)` 中：
  - 当 `requestedProject == ""` 时，不再调用 `fetchGCPProjects` 取 `projects[0]`。
  - 直接将空项目传递给 `performGeminiCLISetup(...)`，由其内部 `loadCodeAssist/onboardUser` 发现项目。

### 2) 默认分支跳过 Cloud API 状态校验

- 在 `RequestGeminiCLIToken` 的默认路径中，跳过 `checkCloudAPIIsEnabled`。
- 显式 `project_id` 继续保留当前校验逻辑。

### 3) 兼容矩阵

- `project_id` 为空：discovery + 不校验 Cloud API（新行为）
- `project_id=<具体ID>`：沿用现状（含 Cloud API 校验）
- `project_id=ALL`：沿用现状（遍历/校验）
- `project_id=GOOGLE_ONE`：沿用现状（保持当前路径与语义）

## 数据流（默认分支）

`RequestGeminiCLIToken (project_id="")`
`-> ensureGeminiProjectAndOnboard(..., "")`
`-> performGeminiCLISetup(..., "")`
`-> loadCodeAssist / onboardUser`
`-> resolve storage.ProjectID`
`-> skip checkCloudAPIIsEnabled`
`-> save token + metadata.project_id`

## 错误处理与可观测性

1. 默认分支失败应聚焦 discovery 链路（`loadCodeAssist/onboardUser`），避免误导为 Cloud API status 失败。
2. 建议增加结构化日志字段：
   - `project_resolution_mode`（`default-discovery/explicit/all/google_one`）
   - `requested_project_id`
   - `resolved_project_id`
   - `cloud_api_check_skipped`

## 测试设计

### A. 默认分支行为
1. 未传 `project_id` 时，不调用 `fetchGCPProjects`。
2. 未传 `project_id` 时，不调用 `checkCloudAPIIsEnabled`。
3. discovery 成功时，`metadata.project_id` 为发现值。

### B. 显式分支回归
1. 显式 `project_id` 仍执行 `checkCloudAPIIsEnabled`。
2. `ALL`/`GOOGLE_ONE` 行为保持不变（至少 smoke 级断言）。

## 风险与缓解

1. **失败位置后移**：默认分支不再提前做 `serviceusage` 检查，失败将体现在 onboarding 阶段。  
   - 缓解：增强失败文案与日志可读性。
2. **分支行为差异增加**：默认与显式路径校验策略不同。  
   - 缓解：在文档与注释中明确策略边界，并通过测试固定行为。

## 验收标准

1. 管理页默认（不传 `project_id`）可通过 discovery 获取项目并完成 token 生成。
2. 默认路径不再触发 Cloud API status 校验错误链路。
3. 显式 `project_id` 与特殊分支行为兼容现有逻辑。
4. 接口响应结构保持兼容，前端无需联动改造。
