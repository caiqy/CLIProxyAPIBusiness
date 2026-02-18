# 管理员平台 Models/BillingRules Provider Catalog 设计

## 背景

当前管理员平台中：

- `Models` 页面 provider 下拉使用前端硬编码列表；
- `BillingRules` 页面 provider 下拉同样使用独立硬编码列表；
- 两处列表存在漂移风险（例如 `kiro` 在后端能力已具备，但前端未展示）。

需求是让两个页面都支持 Kiro，并扩展为“按运行时可注册 provider 全量对齐”。

## 目标

1. Models 与 BillingRules 两页 provider 来源统一。
2. provider 列表由后端下发，不再由前端硬编码维护。
3. 列表口径为“全量可注册 provider”（固定集合、稳定顺序），包含 `kiro`。
4. 页面仍沿用现有模型加载接口，不重写 `available-models` 逻辑。

## 非目标

1. 本期不改模型映射和计费规则核心业务流程。
2. 本期不引入前端回退常量。
3. 本期不做 provider 可视化分组 UI 重设计。

## 方案选型

### 方案 A：前端统一常量（未采用）

- 优点：改动小。
- 缺点：依然需要前端维护 provider 枚举，长期会再次漂移。

### 方案 B：两页各自补齐（未采用）

- 优点：最快。
- 缺点：重复逻辑，后续继续分叉。

### 方案 C：后端下发 Provider Catalog（采用）

- 优点：单一事实来源（Single Source of Truth），两页自动一致。
- 成本：新增一个后端接口与前端加载逻辑。

## 架构设计

新增后端接口：

- `GET /v0/admin/model-mappings/providers`

用途：返回全量可注册 provider 列表，供 Models 与 BillingRules 公用。

建议返回结构：

```json
{
  "providers": [
    {
      "id": "kiro",
      "label": "Kiro",
      "category": "oauth",
      "supports_models": true
    }
  ]
}
```

provider 来源口径：按 `Service` 实际可注册 provider 集合输出（固定顺序），例如：

- `gemini`
- `vertex`
- `gemini-cli`
- `aistudio`
- `antigravity`
- `claude`
- `codex`
- `qwen`
- `iflow`
- `kimi`
- `github-copilot`
- `kiro`
- `kilo`
- `openai-compatibility`

## 页面与组件改造

### Models 页面

1. 移除本地 `PROVIDER_OPTIONS` 硬编码。
2. 页面加载时请求 `/v0/admin/model-mappings/providers`。
3. Provider 下拉使用接口返回列表。
4. 选中 provider 后仍调用：
   - `/v0/admin/model-mappings/available-models?provider=...`

### BillingRules 页面

1. 同步移除本地硬编码 provider 列表。
2. 与 Models 使用同一 provider 接口结果。
3. 保留当前模型请求分支逻辑（mapped/非 mapped）。

## 数据流

1. 页面初始化 → 请求 Provider Catalog。
2. 成功后渲染 provider 下拉（固定顺序）。
3. 用户选择 provider → 请求可用模型。
4. 返回模型后填充 model 下拉。

## 错误处理

1. Provider Catalog 请求失败：
   - provider 下拉不可用；
   - 显示错误提示与重试入口。
2. 模型列表请求失败：
   - 清空模型列表；
   - 仅影响模型选择，不影响 provider 切换。
3. 模型为空：
   - 显示“当前 provider 无可用模型”空态，不视为错误。

## 权限与安全

新增权限建议：

- `GET /v0/admin/model-mappings/providers`

前端在无权限时应隐藏/禁用相关加载，并给出明确提示。

## 测试方案

### 后端测试

1. Provider Catalog 接口返回完整集合与稳定顺序。
2. 权限测试：有权限 200、无权限 403。
3. 回归 `available-models`：`provider=kiro` 路径正常（有 auth 有模型，无 auth 返回空）。

### 前端测试

1. Models 页 provider 下拉来源于新接口。
2. BillingRules 页 provider 下拉来源于新接口。
3. 两页 provider 列表一致（内容与顺序）。
4. 选中 `kiro` 触发模型加载请求。
5. Provider 接口失败时错误可见且可重试。

### 联调验证

1. 管理员有权限时，两页可看到 `kiro`。
2. 模型列表可按 provider 变化。
3. 创建模型映射与计费规则流程不回归。

## 验收标准

1. Models 与 BillingRules 都展示全量可注册 provider（含 `kiro`）。
2. 两页 provider 列表来自同一后端接口，且顺序一致。
3. 不依赖前端硬编码回退常量。
4. 选择 `kiro` 后可正常加载可用模型并用于创建配置。
