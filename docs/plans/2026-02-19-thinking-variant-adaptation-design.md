# Thinking Variant 完善适配设计

**日期：** 2026-02-19  
**范围：** 推理强度（thinking / reasoning_effort）从“静默降级”升级为“能力优先、可审计、可展示”的完整适配。  
**目标：** 保留模型能力优先（支持则透传），不支持时按“向下最近级别”降级，并在首页交易/请求列表可见 `origin => real`。

---

## 1. 背景与问题

当前上游在 `openai` 适配路径中对 `reasoning_effort` 做了统一收敛（如 `xhigh -> high`、`minimal -> low`、`auto -> medium`）。该行为解决了部分 OpenAI-format 兼容问题，但同时引入两个核心问题：

1. **能力信息丢失**：即使目标模型声明支持 `xhigh`，也会被统一降级。
2. **可观测性不足**：用户和管理员无法从首页快速看出“请求值”和“实际下发值”是否发生偏差。

本设计将“是否降级、为何降级、降级到什么”从隐式行为改为显式行为，确保策略可解释、可追踪、可展示。

---

## 2. 设计目标与非目标

### 2.1 设计目标

- **能力优先**：目标模型支持的推理强度必须原样透传（例如支持 `xhigh` 就不降级）。
- **统一降级规则**：不支持时按“向下最近级别”降级。
- **双字段可审计**：落库同时保存 `variant_origin`（请求值）和 `variant`（实际值）。
- **首页可见**：在管理员首页 Recent Transactions 与用户首页 Recent Requests 的模型右侧展示 variant。
- **降级可见**：发生降级时展示 `origin variant => real variant`。

### 2.2 非目标

- 不改二级页面日志列表（`/logs`、`/admin/logs`）展示。
- 不做历史 usage 回填。
- 不引入复杂多维统计（本次仅完成展示与可追踪）。

---

## 3. 关键决策

1. **字段命名统一使用 `variant`（非 `varient`）**。
2. **无显式推理参数时首页显示 `-`**，不将“未指定”与“显式 none”混淆。
3. **双字段设计**：
   - `variant_origin`: 用户请求推理强度。
   - `variant`: 最终下发推理强度。
4. **降级展示规则**：
   - `variant_origin == '' && variant == ''` → `-`
   - `variant_origin == variant` → 显示 `variant`
   - `variant_origin != variant` → 显示 `variant_origin => variant`

---

## 4. 架构与数据流

请求链路拆分为四层：

1. **输入解析层**：从 suffix/body 解析用户意图，得到 `origin_variant`。
2. **能力判定层**：结合 model registry 判断支持集。
3. **策略决策层**：产出 `real_variant` 与决策原因（pass / downgrade / none）。
4. **输出映射层**：将 `real_variant` 写入上游请求字段（`reasoning_effort` 或 `reasoning.effort`）。

同时，usage 链路接收并持久化 `origin_variant` 与 `real_variant`，形成统一审计口径。

---

## 5. 适配规则细化

### 5.1 基本规则

- 若用户未显式指定推理强度：
  - `variant_origin = ''`
  - `variant = ''`
- 若用户指定且模型支持：
  - `variant_origin = requested`
  - `variant = requested`
- 若用户指定但模型不支持：
  - `variant_origin = requested`
  - `variant = nearest_lower(requested, supported_set)`

### 5.2 向下最近级别

定义统一顺序（示例）：`none < minimal < low < medium < high < xhigh`。  
降级时仅允许向下选择，优先最近可用级别。若不存在更低可用值，回退 `none`（并记录 reason）。

### 5.3 严格模式（预留）

保留 `strict_thinking` 入口：开启时，不支持即报错，不降级。默认关闭。

---

## 6. 数据库与后端改造

### 6.1 数据模型

在 `internal/models/usage.go` 的 `Usage` 新增：

- `VariantOrigin string` (`variant_origin`)
- `Variant string` (`variant`)

### 6.2 迁移

在 `internal/db/migrate.go` 新增增量迁移：

- 若 `usages.variant_origin` 不存在则添加。
- 若 `usages.variant` 不存在则添加。

默认空字符串，旧数据无需回填。

### 6.3 usage 写入链路

涉及改造点：

- `third_party/CLIProxyAPIPlus/sdk/cliproxy/usage/manager.go`：`Record` 增加 `VariantOrigin`、`Variant`。
- `third_party/CLIProxyAPIPlus/internal/runtime/executor/usage_helpers.go`：`usageReporter` 透传双字段。
- `internal/usage/usage.go`：落库写入双字段。

---

## 7. 首页接口与前端展示

### 7.1 接口返回

仅调整首页交易接口：

- 用户：`GET /v0/front/dashboard/transactions`
- 管理员：`GET /v0/admin/dashboard/transactions`

两者的 transaction item 增加：

- `variant_origin`
- `variant`

### 7.2 前端组件

- `web/src/components/TransactionsTable.tsx`
- `web/src/components/admin/AdminTransactionsTable.tsx`

在“Model”右侧新增轻量标签（badge）展示：

- 无值：`-`
- 一致：`xhigh`
- 降级：`xhigh => high`

说明：仅首页展示，日志二级页不改。

---

## 8. 错误处理与可观测性

- 能力未知：默认降级路径记录 `reason=unknown_model_fallback`。
- 参数冲突：suffix/body 同时出现且冲突时按既定优先级处理，并记录 warning。
- 落库异常：不阻断主请求返回，但必须记录 error 日志，避免计费链路中断。

建议增加结构化日志字段：`provider`, `model`, `variant_origin`, `variant`, `decision`, `reason`。

---

## 9. 测试计划

### 9.1 单元测试

- 支持直通：`xhigh -> xhigh`
- 不支持降级：`xhigh -> high`
- 无显式参数：`'' -> ''`
- unknown model fallback 行为
- strict 模式下不支持直接报错

### 9.2 集成测试

- executor → usage reporter → usage plugin → DB 双字段全链路。

### 9.3 API/前端测试

- dashboard transactions 返回新增字段。
- 两个首页表格三种展示态：`-` / `xhigh` / `xhigh => high`。

---

## 10. 发布策略

1. 先发后端（字段+写入+接口），保证前端未升级时不受影响。
2. 再发前端展示。
3. 观察指标（24h）：
   - `origin != real` 比例
   - `unknown_model_fallback` 比例
   - strict 错误率（如启用）

---

## 11. 验收标准

- 对支持 `xhigh` 的模型，`variant_origin = xhigh` 且 `variant = xhigh`。
- 对不支持 `xhigh` 的模型，首页显示 `xhigh => high`（或其他最近下级）。
- 无显式推理参数的请求首页显示 `-`。
- 管理员首页与用户首页均可见 variant；日志二级页不变。
- 不引入现有计费与请求处理回归。
