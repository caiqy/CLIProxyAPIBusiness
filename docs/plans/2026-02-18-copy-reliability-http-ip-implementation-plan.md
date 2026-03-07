# HTTP/IP 复制可靠性修复 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在 Chrome/Edge/Firefox 桌面端，把所有复制入口统一到一套“Clipboard API → execCommand → 手动复制弹层”链路，确保 HTTP(IP) 场景下也能最终完成复制。

**Architecture:** 新增 `copy` 能力层（纯函数 + 统一返回结果）和全局手动复制兜底弹层（事件总线驱动），在 `App` 根部挂载一次，所有页面只调用 `copyText` 并按结果显示提示。通过 Vitest + Testing Library 覆盖核心分支，再用逐入口人工矩阵验证交付质量。

**Tech Stack:** React 19 + TypeScript + Vite + i18next + Vitest + Testing Library。

---

> 说明：你已明确“继续但不使用 worktree”，本计划默认在当前工作区/当前分支执行。

### Task 1: 建立前端测试基座并写第一批失败用例（copy 核心）

**Files:**
- Modify: `web/package.json`
- Modify: `web/vite.config.ts`
- Create: `web/src/test/setup.ts`
- Create: `web/src/utils/copy.test.ts`

**Step 1: 写失败测试（先定义期望行为）**

在 `web/src/utils/copy.test.ts` 先写 3 个失败用例（此时 `copy.ts` 还不存在）：

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { copyText } from './copy'

describe('copyText', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('uses clipboard API when available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    const result = await copyText('hello', { source: 'test.clipboard' })
    expect(writeText).toHaveBeenCalledWith('hello')
    expect(result).toMatchObject({ status: 'success', method: 'clipboard' })
  })

  it('falls back to execCommand when clipboard path fails', async () => {
    // clipboard reject + execCommand success
  })

  it('returns manual fallback when both auto paths fail', async () => {
    // clipboard reject + execCommand false
  })
})
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: FAIL（`Cannot find module './copy'` 或等价报错）。

**Step 3: 最小实现测试基座**

1. 在 `web/package.json` 添加：

```json
{
  "scripts": {
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "devDependencies": {
    "vitest": "^3.2.0",
    "jsdom": "^26.0.0",
    "@testing-library/react": "^16.2.0",
    "@testing-library/jest-dom": "^6.6.3",
    "@testing-library/user-event": "^14.6.1"
  }
}
```

2. 在 `web/vite.config.ts` 增加 `test` 配置：

```ts
test: {
  environment: 'jsdom',
  setupFiles: './src/test/setup.ts',
  globals: false,
}
```

3. 创建 `web/src/test/setup.ts`：

```ts
import '@testing-library/jest-dom/vitest'
```

**Step 4: 再次运行测试（仍应失败在实现缺失）**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: FAIL（进入业务断言失败/模块缺失的下一阶段，为 Task 2 做准备）。

**Step 5: Commit**

```bash
git add web/package.json web/vite.config.ts web/src/test/setup.ts web/src/utils/copy.test.ts
git commit -m "test(web): bootstrap vitest for copy reliability work"
```

---

### Task 2: 实现统一 copy 能力层（Clipboard → execCommand → manual）

**Files:**
- Create: `web/src/utils/copy.ts`
- Modify: `web/src/utils/copy.test.ts`

**Step 1: 补齐失败测试（完整分支）**

在 `copy.test.ts` 补齐以下断言：

```ts
it('returns fallback+manual when clipboard and execCommand both fail', async () => {
  // expect(result).toMatchObject({
  //   status: 'fallback',
  //   method: 'manual',
  //   reason: 'insecure_context'
  // })
})
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: FAIL（返回值/分支与预期不符）。

**Step 3: 写最小实现**

在 `web/src/utils/copy.ts` 实现（保留该结构）：

```ts
export type CopyMethod = 'clipboard' | 'execCommand' | 'manual'
export type CopyStatus = 'success' | 'fallback' | 'failed'

export interface CopyOptions { source: string }
export interface CopyResult {
  status: CopyStatus
  method: CopyMethod
  reason?: 'insecure_context' | 'permission_denied' | 'api_unavailable' | 'unknown'
}

export async function copyText(text: string, options: CopyOptions): Promise<CopyResult> {
  const value = text.trim()
  if (!value) return { status: 'failed', method: 'manual', reason: 'api_unavailable' }

  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return { status: 'success', method: 'clipboard' }
    }
  } catch (err) {
    // continue to execCommand fallback
  }

  const ok = copyByExecCommand(value)
  if (ok) return { status: 'success', method: 'execCommand' }

  return {
    status: 'fallback',
    method: 'manual',
    reason: window.isSecureContext ? 'permission_denied' : 'insecure_context',
  }
}
```

**Step 4: 运行测试确认通过**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: PASS（`copyText` 三层策略全部通过）。

**Step 5: Commit**

```bash
git add web/src/utils/copy.ts web/src/utils/copy.test.ts
git commit -m "feat(web): add unified copy utility with fallback chain"
```

---

### Task 3: 实现全局手动复制兜底弹层（事件总线 + 组件）

**Files:**
- Create: `web/src/utils/copyFallbackBus.ts`
- Create: `web/src/components/common/ManualCopyDialogHost.tsx`
- Create: `web/src/components/common/ManualCopyDialogHost.test.tsx`

**Step 1: 写失败测试（弹层可见、可关闭、显示文本）**

在 `ManualCopyDialogHost.test.tsx` 写测试：

```tsx
it('opens dialog when fallback event is emitted', async () => {
  // render(<ManualCopyDialogHost />)
  // emitCopyFallback({ text: 'abc', source: 'test' })
  // expect(screen.getByText(/Ctrl|Cmd\+C/i)).toBeInTheDocument()
  // expect(screen.getByDisplayValue('abc')).toBeInTheDocument()
})
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/components/common/ManualCopyDialogHost.test.tsx`（workdir=`web`）  
Expected: FAIL（组件/事件尚未实现）。

**Step 3: 最小实现**

1. `copyFallbackBus.ts` 提供：

```ts
export interface ManualCopyPayload { text: string; source: string }
export function emitCopyFallback(payload: ManualCopyPayload): void
export function subscribeCopyFallback(listener: (payload: ManualCopyPayload) => void): () => void
```

2. `ManualCopyDialogHost.tsx` 实现：
   - 订阅事件并打开弹层；
   - `<textarea readOnly>` 展示并自动选中文本；
   - 文案：`Copy switched to manual mode` / `Press Ctrl/Cmd+C to copy`；
   - 按钮：`Retry auto copy`（调用 `copyText` 再试）与 `Close`。

**Step 4: 运行测试确认通过**

Run: `npm run test -- src/components/common/ManualCopyDialogHost.test.tsx`（workdir=`web`）  
Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/utils/copyFallbackBus.ts web/src/components/common/ManualCopyDialogHost.tsx web/src/components/common/ManualCopyDialogHost.test.tsx
git commit -m "feat(web): add global manual copy fallback dialog host"
```

---

### Task 4: 把 copy 能力层接上兜底总线并挂载到 App 根部

**Files:**
- Modify: `web/src/utils/copy.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`

**Step 1: 写失败测试（copy fallback 会触发事件）**

在 `copy.test.ts` 新增：

```ts
it('emits manual fallback event when auto copy fails', async () => {
  // spyOn emitCopyFallback
  // expect(spy).toHaveBeenCalledWith({ text: 'x', source: 'test' })
})
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: FAIL（事件未触发）。

**Step 3: 最小实现**

1. `copy.ts` 在 `status='fallback'` 前调用 `emitCopyFallback({ text, source })`；  
2. `App.tsx` 在 `<BrowserRouter>` 内追加 `<ManualCopyDialogHost />`（只挂载一次）；  
3. locale 新增：
   - `Copy switched to manual mode`
   - `Press Ctrl/Cmd+C to copy`
   - `Retry auto copy`

**Step 4: 运行测试确认通过**

Run: `npm run test -- src/utils/copy.test.ts`（workdir=`web`）  
Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/utils/copy.ts web/src/App.tsx web/src/locales/en.ts web/src/locales/zh-CN.ts web/src/utils/copy.test.ts
git commit -m "feat(web): wire copy fallback events and global dialog mount"
```

---

### Task 5: 改造前台页面复制入口（Settings / ModelsPricing / ApiKeys）

**Files:**
- Modify: `web/src/pages/Settings.tsx`
- Modify: `web/src/pages/ModelsPricing.tsx`
- Modify: `web/src/pages/ApiKeys.tsx`

**Step 1: 写失败测试（最少覆盖 1 个页面行为）**

新增或扩展组件测试，验证：
- 点击复制按钮会调用 `copyText`；
- `status='success'` 维持原成功提示；
- `status='fallback'` 显示温和引导提示（不是硬错误）。

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/pages`（workdir=`web`）  
Expected: FAIL（仍在用原生 `navigator.clipboard`）。

**Step 3: 最小实现**

统一替换：

```ts
const result = await copyText(value, { source: 'Settings.copyTotpSecret' })
if (result.status === 'success') showToast(t('Copied'))
else if (result.status === 'fallback') showToast(t('Copy switched to manual mode'))
else setMfaError(t('Failed to copy'))
```

`ModelsPricing.tsx` 删除本地 `execCommand` 复制逻辑，改为只调用统一 `copyText`。

**Step 4: 运行测试确认通过**

Run: `npm run test -- src/pages`（workdir=`web`）  
Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/Settings.tsx web/src/pages/ModelsPricing.tsx web/src/pages/ApiKeys.tsx
git commit -m "refactor(web): route front-page copy actions through unified copy utility"
```

---

### Task 6: 改造后台页面复制入口（AdminDashboardLayout / AuthFiles / Users / PrepaidCards / Quotas）

**Files:**
- Modify: `web/src/components/admin/AdminDashboardLayout.tsx`
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Modify: `web/src/pages/admin/Users.tsx`
- Modify: `web/src/pages/admin/PrepaidCards.tsx`
- Modify: `web/src/pages/admin/Quotas.tsx`

**Step 1: 写失败测试（至少覆盖 2 个关键入口）**

建议优先：
- `AuthFiles` 的 `Copy URL`
- `Quotas` 的 `Copy Error`

断言 fallback 分支下出现温和提示并可触发全局手动复制弹层。

**Step 2: 运行测试确认失败**

Run: `npm run test -- src/pages/admin`（workdir=`web`）  
Expected: FAIL（旧调用仍直接依赖 clipboard API）。

**Step 3: 最小实现**

统一替换所有 `navigator.clipboard.writeText(...)`：

```ts
const result = await copyText(payload, { source: 'AdminAuthFiles.copyUrl' })
if (result.status === 'success') setCopied(true)
if (result.status === 'fallback') showToast(t('Copy switched to manual mode'))
```

并移除重复 `try/catch` 剪贴板细节代码。

**Step 4: 运行测试确认通过**

Run: `npm run test -- src/pages/admin`（workdir=`web`）  
Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/components/admin/AdminDashboardLayout.tsx web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/Users.tsx web/src/pages/admin/PrepaidCards.tsx web/src/pages/admin/Quotas.tsx
git commit -m "refactor(admin): unify admin copy flows with HTTP/IP fallback behavior"
```

---

### Task 7: 全量验证与逐入口验收清单

**Files:**
- Create: `docs/plans/2026-02-18-copy-reliability-http-ip-checklist.md`（可选但推荐）
- Modify: `docs/plans/2026-02-18-copy-reliability-http-ip-design.md`（如需补充实际落地差异）

**Step 1: 运行自动化验证**

Run: `npm run test`（workdir=`web`）  
Expected: PASS（0 failing）。

Run: `npm run build`（workdir=`web`）  
Expected: PASS（TypeScript + Vite 均通过）。

Run: `npm run lint`（workdir=`web`）  
Expected: PASS；若存在仓库历史问题，需在结果中区分“既有问题 vs 本次引入”。

**Step 2: 执行人工矩阵验收（逐入口）**

浏览器：Chrome / Edge / Firefox  
协议：`https://domain` 与 `http://ip:port`  
每个入口确认：
1. HTTPS：优先自动成功；
2. HTTP/IP：自动失败时可回退；
3. 回退失败时弹出手动复制面板；
4. 用户按 Ctrl/Cmd+C 能完成复制。

**Step 3: 记录结果**

在 checklist 中记录每个入口状态：`PASS(auto)` / `PASS(manual)` / `FAIL`，并附失败原因。

**Step 4: 代码提交**

```bash
git add web docs/plans/2026-02-18-copy-reliability-http-ip-checklist.md docs/plans/2026-02-18-copy-reliability-http-ip-design.md
git commit -m "feat(web): harden copy reliability for HTTP/IP contexts"
```

**Step 5: 最终回归摘要**

输出：
- 总入口数
- 自动成功率
- 手动兜底率
- 剩余失败入口（如有）

---

## 附：复制入口 source 命名建议（统一埋点维度）

- `Settings.copyTotpSecret`
- `Settings.copyTotpUrl`
- `ModelsPricing.copyModelName`
- `ApiKeys.copyExistingKey`
- `ApiKeys.copyNewToken`
- `AdminDashboardLayout.copyTotpSecret`
- `AdminDashboardLayout.copyTotpUrl`
- `AdminAuthFiles.copyUrl`
- `AdminUsers.copyNewKeyToken`
- `AdminUsers.copyKeyPrefix`
- `AdminPrepaidCards.copyBatch`
- `AdminQuotas.copyError`
