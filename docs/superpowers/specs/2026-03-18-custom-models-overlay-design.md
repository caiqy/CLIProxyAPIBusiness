# custom_models.json 模型目录叠加设计

## 1. 背景

当前 `CLIProxyAPIPlus` 的静态模型目录来源于 `internal/registry/models/models.json`，启动后还会从远端拉取最新 `models.json` 并整体替换内存中的 catalog。

这导致本地临时修改 `models.json` 不适合作为长期自定义方案：

- embedded `models.json` 只是在启动前提供 fallback
- 启动后的远端刷新会覆盖内存中的基础 catalog
- 定时刷新也会持续以远端结果替换 catalog

本次需要增加一个运行时 `custom_models.json` 机制，用来对基础模型目录做**增补与覆盖**，且在启动加载和定时刷新后都稳定生效。

## 2. 目标

1. 新增 `custom_models.json`，作为运行时本地 overlay。
2. `custom_models.json` 放在**运行配置文件同目录**。
3. 启动加载与远端定时刷新后，都对基础 `models.json` 执行同一套 overlay 合并。
4. 支持 `staticModelsJSON` 已定义的所有顶层 provider 段，而不是仅支持 `claude`。
5. 同 `id` 的模型项由 `custom_models.json` 覆盖；不存在的模型项追加增补。
6. `custom_models.json` 缺失或非法时，服务继续运行，仅记录告警。
7. 变更检测与 refresh callback 基于**最终合并结果**而不是基础 catalog。

## 3. 非目标

1. 本次不引入数据库或管理端 UI 来编辑 `custom_models.json`。
2. 本次不改变远端 `models.json` 的拉取来源和刷新周期。
3. 本次不做字段级 merge；覆盖以整条 `ModelInfo` 替换为准。
4. 本次不扩展到 auth 级模型映射机制（如 `claude-api-key.models[]`）。
5. 本次不承诺对 `custom_models.json` 做文件监听热更新；其生效点仅限启动加载与现有刷新链路。

## 4. 已确认决策

1. `custom_models.json` 支持 **`staticModelsJSON` 已定义的所有顶层 provider 段**。
2. 文件位置为：`<config.yaml 所在目录>/custom_models.json`。
3. 文件缺失、内容非法、段格式错误时：**容错忽略**，保留基础 models，并记录 warning。
4. 覆盖规则按 **model `id`** 判断，同 `id` 时 custom 覆盖基础。
5. 新增模型项追加到对应 provider 列表末尾。

## 5. 方案对比与选择

### 方案 A：加载/刷新后执行 overlay 合并（采用）

思路：

1. 保留现有基础链路：embedded 加载 + 远端拉取刷新。
2. 每次拿到基础 catalog 后，再尝试读取 `custom_models.json`。
3. 生成最终 catalog：`final = overlay(base, custom)`。

优点：

- 改动集中在 `internal/registry`
- 启动和定时刷新两条路径天然统一
- 不需要改变现有 registry 外部调用方式
- 风险最小，符合当前架构

缺点：

- 需要补一套 overlay 合并与容错日志逻辑

### 方案 B：base/custom 分库存储，读取时动态合并（不采用）

优点是数据来源更清晰；缺点是 `getModels()`、变更检测、回调逻辑都会变复杂，当前场景收益不足。

### 方案 C：将合并结果落盘成新文件（不采用）

优点是便于肉眼查看最终结果；缺点是引入额外磁盘状态与一致性问题，不适合当前以内存 catalog 为主的设计。

## 6. 总体架构

最终模型目录改为两层来源：

1. **基础层**：embedded `models.json` 或最近一次远端拉取得到的 `models.json`
2. **覆盖层**：`<config dir>/custom_models.json`

运行时需要维护两个概念：

1. **base catalog**：最近一次成功装载的基础目录（embedded 或远端成功拉取结果）
2. **final catalog**：对 base catalog 套用 `custom_models.json` 后得到的最终目录

对外提供给 `getModels()` 的始终是 **final catalog**。

最终内存中的可见 models catalog 为：

`final_catalog = overlay(base_catalog, custom_catalog)`

其中：

- 若 custom 文件不存在，则 `final_catalog = base_catalog`
- 若 custom 文件存在但非法，则记录 warning，`final_catalog = base_catalog`
- 若 custom 文件合法，则执行 provider 级 overlay merge

## 7. custom_models.json 文件格式

`custom_models.json` 顶层结构与现有 `models.json` 一致，支持的 provider 段与 `staticModelsJSON` 顶层字段保持一致，包括但不限于：

- `claude`
- `gemini`
- `vertex`
- `gemini-cli`
- `aistudio`
- `codex-free`
- `codex-team`
- `codex-plus`
- `codex-pro`
- `qwen`
- `iflow`
- `kimi`
- `antigravity`

不扩展到非 `models.json` 目录来源的其他静态通道，例如：

- `github-copilot`
- `kiro/kilo`
- `amazonq`

示例：

```json
{
  "claude": [
    {
      "id": "claude-sonnet-4.6",
      "display_name": "Claude Sonnet 4.6",
      "owned_by": "anthropic"
    }
  ],
  "gemini": [
    {
      "id": "gemini-2.5-pro-preview",
      "display_name": "Gemini 2.5 Pro Preview"
    }
  ]
}
```

## 8. 合并规则

### 8.1 合并粒度

按 provider/channel 分段合并，不跨段共享：

- `claude` 只与 `claude` 合并
- `gemini` 只与 `gemini` 合并
- `codex-free` 只与 `codex-free` 合并

### 8.2 唯一键

每个 provider 列表按 `ModelInfo.ID` 作为唯一键。

规则：

- custom 中某项 `id` 不存在于基础列表 -> **新增**
- custom 中某项 `id` 已存在于基础列表 -> **整条覆盖**

### 8.3 覆盖语义

覆盖时不做字段级 merge，而是以 custom 条目整体替换基础条目。

原因：

- 规则最简单，可预测性最高
- 避免旧字段残留导致结果不一致
- 便于后续调试“最终到底用了哪一条定义”

### 8.4 顺序规则

1. 基础列表中未被覆盖的项保持原顺序。
2. 被 custom 覆盖的项保留原位置，但内容替换为 custom 项。
3. custom 新增项追加到该 provider 列表末尾。

示例：

基础：

```json
{
  "claude": [
    {"id": "a"},
    {"id": "b"},
    {"id": "c"}
  ]
}
```

custom：

```json
{
  "claude": [
    {"id": "b", "owned_by": "custom"},
    {"id": "d"}
  ]
}
```

结果：

```json
{
  "claude": [
    {"id": "a"},
    {"id": "b", "owned_by": "custom"},
    {"id": "c"},
    {"id": "d"}
  ]
}
```

### 8.5 provider 存在性规则

- 基础存在、custom 不存在 -> 保留基础 provider 列表
- 基础不存在、custom 存在 -> 若是受支持 provider，则新增整个 provider 列表
- custom 中出现未知顶层 key -> 忽略该段并记录 warning

### 8.6 非法项处理

对 `custom_models.json` 执行“尽量吃掉合法部分”的容错策略：

- 文件不存在 -> 忽略 custom
- 文件 JSON 非法 -> 忽略 custom
- provider 值不是数组 -> 忽略该 provider 段
- 模型项缺少有效 `id` -> 跳过该项

所有上述情况都只记录 warning，不中断服务。

实现约束：

- **不能直接复用** `loadModelsFromBytes()` / `validateModelsCatalog()` 这套针对完整 `models.json` 的全量校验逻辑
- `custom_models.json` 应按 provider 逐段解析，例如基于 `map[string]json.RawMessage` 清洗合法段与合法模型项

原因是 custom 的目标是“部分覆盖文件”，而不是必须满足完整 catalog 的全量约束。

## 9. 加载与刷新流程

### 9.1 启动流程

1. 进程 init 阶段加载 embedded `models.json`，形成初始 base catalog。
2. 主程序在解析出 `config.yaml` 路径后，计算 `custom_models.json` 的完整路径。
3. 调用 registry 的 custom 路径设置入口。
4. **设置路径后立即执行一次重算**：基于当前 base catalog 读取 custom 并生成 final catalog，写回当前 store。
5. 此后 service 对外暴露的 `getModels()` 始终读取 final catalog。

强约束：

- 不能只“记录 custom 路径”而不立即重算，否则第一次远端刷新成功前，调用方可能读到未 overlay 的基础目录。

### 9.2 启动后首次远端刷新流程（startup refresh）

1. `StartModelsUpdater()` 启动后，先立即执行一次 startup refresh。
2. startup refresh 尝试拉取远端最新 `models.json`。
3. 若远端拉取成功，则更新 base catalog 为最新远端结果。
4. 若远端拉取失败，则保留“最近一次成功装载的 base catalog”不变。
5. 读取当前 `custom_models.json`。
6. 基于当前 base catalog + 当前 custom 重新生成 final catalog。
7. 用新的 final catalog 替换当前可见 store。
8. 基于最终合并结果执行 changed providers 检测与回调。

### 9.3 周期刷新流程（periodic refresh）

1. 周期 tick 到达时，读取当前 `custom_models.json`。
2. 尝试拉取远端最新 `models.json`。
3. 若远端拉取成功，则更新 base catalog 为最新远端结果。
4. 若远端拉取失败，则保留“最近一次成功装载的 base catalog”不变。
5. 无论远端成功还是失败，都基于当前 base catalog + 当前 custom 重新生成 final catalog。
6. 用新的 final catalog 替换当前可见 store。
7. 基于最终合并结果执行 changed providers 检测与回调。

这样可以保证：

- custom 文件修改无需热监听，也能在下一次周期 tick 生效
- 远端暂时不可用时，custom 仍可覆盖最近一次成功装载的基础目录

## 10. 路径与初始化

为避免 `registry` 与 `config`/`watcher` 强耦合，建议给 `internal/registry` 增加一个小型初始化入口，用于注入 custom 文件路径或配置目录，例如：

- `SetCustomModelsPath(path string)`
或
- `SetCustomModelsDir(dir string)`

推荐注入完整路径，便于测试与后续扩展。

调用时机：

- 在主程序已解析出 `config.yaml` 路径后立即设置
- 设置时要同步触发一次“基于当前 base catalog 的 final catalog 重算”
- 之后 `registry` 内部统一使用该路径查找 `custom_models.json`

## 11. 变更检测与回调语义

当前 `detectChangedProviders` 基于基础 catalog 比较。本次需要调整为基于**最终合并结果**比较：

- 旧值：`final(old_base + custom)`
- 新值：`final(new_base + custom)`

这样可以保证：

1. 若远端基础 catalog 变化被 custom 完全覆盖，且最终结果未变 -> 不误报 changed
2. 若 custom 让最终 provider 列表变化 -> 正确上报 changed providers

该规则对 registry 后续刷新回调尤为关键，否则会导致运行时注册更新与实际可见模型目录不一致。

兼容性约束：

- **比较对象**改为最终 overlay 结果
- **回调输出语义**保持现有约定不变
- `codex-free` / `codex-team` / `codex-plus` / `codex-pro` 的变化仍统一聚合为单个 `codex` provider 事件

## 12. 可观测性与调试

### 12.1 日志

建议增加以下日志：

1. custom 文件不存在：
   - `custom models file not found, skipping overlay`
2. custom 文件加载成功：
   - `custom models loaded from <path>`
3. custom 文件解析/校验失败：
   - `custom models parse failed, skipping overlay`
4. 每次合并摘要：
   - `custom models overlay applied: provider=claude overridden=2 added=1`

### 12.2 调试接口

建议扩展现有 `/debug/model-route-memory` 的诊断信息，但不强制改变主体结构。可补充：

- 当前是否检测到 `custom_models.json`
- custom 文件路径
- 某个 model 是否来自 custom overlay
- provider 级 overlay 摘要

这样排查时可以直接判断某模型来自基础 catalog 还是 custom 覆盖层。

## 13. 代码落点

### 主要修改

1. `third_party/CLIProxyAPIPlus/internal/registry/model_updater.go`
   - 统一基础加载与 refresh 后的 overlay 逻辑
   - 调整 changed providers 检测时机与比较对象
   - 注入 custom 路径读取逻辑
   - 保存最近一次成功装载的 base catalog，并在每次重算时生成新的 final catalog

2. `third_party/CLIProxyAPIPlus/internal/registry/model_definitions.go`
   - 原则上不改行为，仅继续读取 `getModels()` 的最终结果
   - 如有需要，补注释说明其返回的是 overlay 后 catalog

### 建议新增

3. `third_party/CLIProxyAPIPlus/internal/registry/custom_models.go`
   - 负责 custom 文件读取
   - provider 段校验/清洗（逐段解析，不能依赖完整 catalog 校验器）
   - overlay merge 实现
   - 合并摘要统计

## 14. 测试策略

### 14.1 单元测试

至少覆盖：

1. custom 文件不存在 -> 返回基础 catalog
2. custom 文件非法 -> 忽略 custom，服务不失败
3. provider 新增 -> 基础无、custom 有，最终出现
4. 同 `id` 覆盖 -> custom 替换基础项
5. 新模型追加 -> 追加到列表末尾
6. 顺序稳定 -> 未覆盖项顺序不变，覆盖项位置不变
7. 未知顶层 key -> 忽略并记录 warning
8. provider 值不是数组 -> 忽略该段
9. 模型项缺少 `id` -> 跳过该项
10. changed providers 比较基于最终结果
11. 设置 custom 路径后、首次远端刷新前，`getModels()` 已返回 overlay 后结果
12. `codex-*` 任一分段变化时，callback 仍只收到 `codex`
13. custom 仅提供部分 provider 时仍合法，不触发“完整 catalog 缺段”类失败

### 14.2 集成测试

至少覆盖：

1. 启动加载时 overlay 生效
2. startup refresh 时重新套用 overlay
3. 周期刷新时重新套用 overlay
4. refresh callback 看到的是最终 catalog 的变化结果
5. 远端拉取失败时，仍基于最近一次成功 base + 当前 custom 重算 final catalog

## 15. 风险与缓解

### 风险

1. 若 custom 覆盖了远端 catalog 的大量字段，可能掩盖远端升级带来的新元数据变化。
2. 若 custom 写入未知 provider 或无效模型项，用户容易误以为“系统已接受”。
3. 若 changed providers 仍基于基础 catalog 计算，会导致运行时模型注册与最终目录不一致。

### 缓解

1. 使用整条覆盖规则，避免半合并状态不透明。
2. 增加明确日志与 debug 诊断信息。
3. 为 changed providers 比较补单测与集成测试。

## 16. 验收标准

1. 运行时支持从 `<config dir>/custom_models.json` 读取 overlay。
2. custom 支持 `staticModelsJSON` 已定义的所有顶层 provider 段。
3. 启动加载与远端定时刷新后，custom 都能重新叠加到基础 catalog。
4. 同 `id` 模型项按 custom 覆盖，新增项追加到末尾。
5. custom 文件缺失或非法时，服务继续运行，仅记录 warning。
6. changed providers 检测基于最终合并结果。
7. debug / 日志能帮助确认某模型是否由 custom overlay 提供。
