# Admin Transactions Request Log Fullscreen Modal Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将管理员交易日志查看弹窗改为真全屏，并在日志请求失败时改为右上角 toast 提示且不打开弹窗。

**Architecture:** 仅修改 `web/src/components/admin/AdminTransactionsTable.tsx` 的前端交互与样式：把“先开弹窗再请求”改为“先请求成功再开弹窗”，并将 modal 容器改为 `w-screen + h-screen` 全屏布局。错误路径统一走 toast，不再使用弹窗内错误展示。测试在现有 request-log 测试文件中补齐全屏与失败不弹窗断言。

**Tech Stack:** React 19, TypeScript, Tailwind CSS, Vitest, Testing Library。

---

### Task 1: 调整 request-log 打开时机（成功才开弹窗）

**Files:**
- Modify: `web/src/components/admin/AdminTransactionsTable.tsx`
- Test: `web/src/components/admin/AdminTransactionsTable.request-log.test.tsx`

**Step 1: Write the failing test**

在 `AdminTransactionsTable.request-log.test.tsx` 新增用例：请求日志接口返回错误时，不应出现 `role="dialog"`。

```tsx
it('does not open dialog when request-log request fails', async () => {
  // list success + request-log failure
  // click icon
  // expect screen.queryByRole('dialog') toBeNull()
})
```

**Step 2: Run test to verify it fails**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: FAIL（当前实现会先打开弹窗）。

**Step 3: Write minimal implementation**

在 `openRequestLog` 中将 `setRequestLogOpen(true)` 从请求前移除，改为请求成功后再设置；失败分支不再打开弹窗。

```ts
apiFetchAdmin(...)
  .then((res) => {
    setRequestLogRequest(...)
    setRequestLogResponse(...)
    setRequestLogOpen(true)
  })
  .catch((error) => {
    // 仅设置 toast 文案
  })
```

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: PASS。

**Step 5: Commit**

```bash
git -C web add src/components/admin/AdminTransactionsTable.tsx src/components/admin/AdminTransactionsTable.request-log.test.tsx
git -C web commit -m "feat(dashboard): open request-log modal only on success"
```

### Task 2: 将弹窗改为真全屏布局

**Files:**
- Modify: `web/src/components/admin/AdminTransactionsTable.tsx`
- Test: `web/src/components/admin/AdminTransactionsTable.request-log.test.tsx`

**Step 1: Write the failing test**

新增断言：打开日志弹窗后，容器具有全屏类（如 `inset-0`、`w-screen`、`h-screen`）。

```tsx
const dialog = await screen.findByRole('dialog')
expect(dialog.parentElement).toHaveClass('inset-0')
expect(screen.getByTestId('request-log-modal-shell')).toHaveClass('w-screen', 'h-screen')
```

**Step 2: Run test to verify it fails**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: FAIL（当前是 `max-w-5xl` 居中弹窗）。

**Step 3: Write minimal implementation**

将弹窗结构改为真全屏：

- 外层遮罩：`fixed inset-0 z-50`
- 主体容器：`w-screen h-screen`
- 去掉 `max-w-5xl`、`rounded-xl`、`p-4`
- 内容区使用 `overflow-auto`

```tsx
<div className="fixed inset-0 z-50 bg-black/50">
  <div data-testid="request-log-modal-shell" className="w-screen h-screen ...">
```

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: PASS。

**Step 5: Commit**

```bash
git -C web add src/components/admin/AdminTransactionsTable.tsx src/components/admin/AdminTransactionsTable.request-log.test.tsx
git -C web commit -m "feat(dashboard): switch request-log dialog to fullscreen"
```

### Task 3: 失败提示改为右上角 toast（且不弹窗）

**Files:**
- Modify: `web/src/components/admin/AdminTransactionsTable.tsx`
- Test: `web/src/components/admin/AdminTransactionsTable.request-log.test.tsx`

**Step 1: Write the failing test**

新增用例：请求失败后出现 toast，且 dialog 不存在。

```tsx
expect(await screen.findByText('mock request log failed')).toBeInTheDocument()
expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
```

并给 toast 断言位置类（如 `fixed top-4 right-4`）。

**Step 2: Run test to verify it fails**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: FAIL（当前错误展示在弹窗内部）。

**Step 3: Write minimal implementation**

新增 toast 状态，例如：

```ts
const [requestLogToast, setRequestLogToast] = useState('')
```

失败分支设置 toast；渲染右上角 toast 容器，带自动关闭（可选 3-5 秒）与手动关闭按钮。

```tsx
{requestLogToast ? (
  <div className="fixed top-4 right-4 z-[60] ...">...</div>
) : null}
```

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: PASS。

**Step 5: Commit**

```bash
git -C web add src/components/admin/AdminTransactionsTable.tsx src/components/admin/AdminTransactionsTable.request-log.test.tsx
git -C web commit -m "feat(dashboard): show request-log errors as toast"
```

### Task 4: 回归可访问性与并发保护

**Files:**
- Modify: `web/src/components/admin/AdminTransactionsTable.request-log.test.tsx`

**Step 1: Write/adjust failing test**

确保以下旧能力仍覆盖：

1. Esc 关闭 dialog 并恢复焦点
2. 快速点击两行，仅最后一次响应生效

必要时调整断言，确保与“成功才开弹窗”新行为不冲突。

**Step 2: Run test to verify it fails (if broken)**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx`

Expected: 若有行为回退则 FAIL，否则可直接 PASS。

**Step 3: Minimal implementation/fix (if needed)**

仅在测试失败时做最小修复，避免引入额外功能。

**Step 4: Run test to verify it passes**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx AdminTransactionsTable.variant.test.tsx`

Expected: PASS。

**Step 5: Commit**

```bash
git -C web add src/components/admin/AdminTransactionsTable.request-log.test.tsx src/components/admin/AdminTransactionsTable.tsx
git -C web commit -m "test(dashboard): cover fullscreen and failure-without-dialog behavior"
```

### Task 5: 最终验证与主仓库同步

**Files:**
- Modify: `web` (submodule pointer in main repo)

**Step 1: Run full target verification**

Run: `npm --prefix web run test -- AdminTransactionsTable.request-log.test.tsx AdminTransactionsTable.variant.test.tsx`

Expected: PASS。

**Step 2: Run frontend build**

Run: `npm --prefix web run build`

Expected: BUILD SUCCESS。

**Step 3: Update main repo submodule pointer**

Run: `git add web`

Expected: `git status --short` shows `M web` staged。

**Step 4: Commit in main repo**

```bash
git add web
git commit -m "feat(admin): use fullscreen request-log modal with toast errors"
```

**Step 5: Final status check**

Run: `git status -sb && git -C web status -sb`

Expected: clean working trees。
