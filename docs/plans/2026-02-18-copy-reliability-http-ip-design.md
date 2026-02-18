# Web 复制能力在 HTTP/IP 场景下的可靠性修复设计

## 1. 背景与问题

当前 Web 管理界面存在大量“复制”功能点，复制实现分散在不同页面，主要直接调用 `navigator.clipboard.writeText`，少数位置带有 `execCommand('copy')` 回退。该模式在 `http://<ip>:<port>` 访问场景下容易出现不可用或行为不一致，原因是现代浏览器通常仅在安全上下文（HTTPS、localhost）完整开放 Clipboard API。结果是：

- 有些按钮在 HTTPS 正常、HTTP(IP) 失败；
- 有些按钮失败后仅报错，没有可完成任务的兜底路径；
- 复制体验和提示文案不统一，难以排查与维护。

本设计目标是：在 Chrome/Edge/Firefox 桌面端，实现“尽可能自动复制 + 必要时稳定手动兜底”，并对所有复制入口逐项验收，确保不出现“点击后无结果”的失效体验。

## 2. 目标与非目标

### 2.1 目标

1. 在 HTTPS 场景优先走现代 Clipboard API，保持高成功率。
2. 在 HTTP(IP) 或权限受限场景，自动回退 `execCommand`，继续提升自动复制成功率。
3. 自动复制失败时统一进入“手动复制”兜底 UI（温和引导），保证用户可完成复制。
4. 所有复制入口统一接入同一能力层，统一日志与错误语义。
5. 建立逐入口验证清单，覆盖 Chrome/Edge/Firefox + HTTPS/HTTP(IP)。

### 2.2 非目标（YAGNI）

1. 本轮不引入复杂权限引导页或浏览器特性向导。
2. 本轮不重构全站按钮组件体系，仅收敛复制能力与兜底交互。
3. 本轮不承诺移动端全面兼容（可在后续版本单独评估）。

## 3. 现状复制入口清单（首批）

以下入口已确认存在复制逻辑（后续以代码扫描脚本+人工复核持续补齐）：

- `web/src/pages/admin/Quotas.tsx`
- `web/src/pages/admin/AuthFiles.tsx`
- `web/src/pages/admin/Users.tsx`
- `web/src/pages/admin/PrepaidCards.tsx`
- `web/src/pages/Settings.tsx`
- `web/src/pages/ModelsPricing.tsx`
- `web/src/pages/ApiKeys.tsx`
- `web/src/components/admin/AdminDashboardLayout.tsx`

> 注：`ModelsPricing.tsx` 已有 `execCommand` 回退雏形，但实现和提示风格与其他页面不一致，需要统一。

## 4. 方案对比与选型

### 方案 A：统一能力层（推荐）

新增统一工具 `copyText()` + 全局 `ManualCopyDialog`，所有业务入口调用同一 API。

**优点：**
- 复制策略集中治理，后续维护成本低；
- 各页面交互一致，避免重复 bug；
- 更易埋点与逐入口统计。

**缺点：**
- 需要一次性改造所有复制调用点。

### 方案 B：逐页打补丁

每个页面各自 `try/catch` + 回退。

**优点：** 改动快。  
**缺点：** 逻辑继续分散，长期维护差，易遗漏。

### 方案 C：全面组件化重构

所有复制按钮改为统一组件。

**优点：** 规范性最强。  
**缺点：** 改动面最大，超出本轮“快速止血+可靠兜底”目标。

**结论：采用方案 A。**

## 5. 详细设计

### 5.1 统一复制能力 API

建议新增：`web/src/utils/copy.ts`

```ts
type CopyMethod = 'clipboard' | 'execCommand' | 'manual'
type CopyStatus = 'success' | 'fallback' | 'failed'

interface CopyOptions {
  source: string
  preferExecCommand?: boolean
}

interface CopyResult {
  status: CopyStatus
  method: CopyMethod
  reason?: 'insecure_context' | 'permission_denied' | 'api_unavailable' | 'unknown'
}

export async function copyText(text: string, options: CopyOptions): Promise<CopyResult>
```

策略顺序：
1. `navigator.clipboard.writeText(text)`（可用则优先）；
2. `execCommand('copy')` 回退；
3. 触发手动复制兜底（返回 `status='fallback'`）。

### 5.2 全局手动复制兜底组件

建议新增：
- `web/src/components/common/ManualCopyDialog.tsx`
- `web/src/contexts/CopyFallbackContext.tsx`（或轻量事件总线）

职责：
- 展示待复制文本；
- 自动聚焦并选中文本；
- 提示快捷键（Windows/Linux：Ctrl+C，macOS：Cmd+C）；
- 提供“重试自动复制”按钮；
- 关闭后可回到原页面继续操作。

提示风格按已确认策略：**温和引导**（非强报错）。

### 5.3 业务层接入规范

所有复制点位改为：

```ts
const result = await copyText(value, { source: 'ApiKeys.copyExistingKey' })
```

页面仅负责：
- 成功 toast；
- 回退提示文案（如“已切换手动复制”）；
- 不再直接处理浏览器 API 细节。

### 5.4 文案规范

新增/统一国际化键值：
- `Copy switched to manual mode`
- `Press Ctrl/Cmd+C to copy`
- `Retry auto copy`

沿用已有：
- `Copied to clipboard`
- `Failed to copy`（仅极端失败使用）

## 6. 数据流与错误处理

### 6.1 数据流

`点击复制 -> copyText -> 能力探测 -> 执行策略 -> 返回结果 -> UI反馈`

### 6.2 错误分类

- `insecure_context`：HTTP(IP) 常见；
- `permission_denied`：用户/浏览器策略拒绝；
- `api_unavailable`：接口缺失；
- `unknown`：其他异常。

### 6.3 UI反馈规则

1. 自动复制成功：显示成功提示。  
2. 自动失败但进入手动兜底：显示温和引导提示，不强调“错误”。  
3. 兜底流程异常：显示明确失败提示并建议重试。

## 7. 测试与验收

### 7.1 测试矩阵

- 浏览器：Chrome / Edge / Firefox（桌面）
- 协议：HTTPS / HTTP(IP)
- 文本类型：短文本、长文本、含特殊字符
- 操作模式：单次、连续点击

### 7.2 自动化建议

1. **单测（utils）**：覆盖三层策略分支与返回值语义。  
2. **组件测试（dialog）**：选中、提示、重试行为。  
3. **关键链路 E2E（可选）**：至少 API Key 与 AuthFiles 两条主链路。

### 7.3 人工验收标准

1. 清单中每个复制入口均完成跨浏览器、跨协议验证。  
2. 不允许出现“点击复制后无提示、无结果”。  
3. HTTP(IP) 场景下允许自动复制失败，但必须可通过兜底完成复制（完成率 100%）。

## 8. 实施顺序建议

1. 落地 `copyText` 与 `ManualCopyDialog`。  
2. 批量替换全部复制调用点。  
3. 统一 i18n 文案。  
4. 完成测试矩阵与逐项勾检。  
5. 灰度上线并观察“自动成功率/回退率/手动兜底率”。

## 9. 风险与缓解

1. **浏览器策略继续收紧**：通过能力层集中更新降低改造成本。  
2. **漏改复制入口**：以“代码扫描 + 清单验收”双轨兜底。  
3. **用户对兜底弹层不习惯**：使用温和文案并保持操作极简。

---

本设计已确认采用：
- 兼容目标：HTTPS + HTTP(IP)
- 策略：最大成功率（Clipboard API + `execCommand` + 手动兜底）
- 浏览器范围：Chrome + Edge + Firefox
- 反馈风格：温和引导
- 验收口径：所有复制入口逐一验证
