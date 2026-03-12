# 管理员面板模型映射批量删除 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为管理员面板“模型映射”页面增加“批量删除所选”能力，采用逐条删除、失败保留的前端实现。

**Architecture:** 仅修改 `web/src/pages/admin/Models.tsx`，复用现有 `selectedIds`、`confirmDialog` 和单条删除接口 `DELETE /v0/admin/model-mappings/:id`。测试通过 Vitest + React Testing Library 验证成功删除与部分失败两类状态更新，不新增后端接口或权限点。

**Tech Stack:** React 19, TypeScript, Vitest, React Testing Library, Vite

---

### Task 1: 为模型映射批量删除补失败测试

**Files:**
- Create: `web/src/pages/admin/modelMappingsBulkDelete.test.tsx`
- Reference: `web/src/pages/admin/AuthFiles.tsx`
- Reference: `web/src/pages/admin/authFilesWhitelistPayload.test.tsx`

**Step 1: 写失败测试（全部删除成功）**

新增测试文件，按现有管理页测试风格：

```tsx
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import i18n from '../../i18n';
import * as apiConfig from '../../api/config';
import { buildAdminPermissionKey } from '../../utils/adminPermissions';
import { AdminModels } from './Models';
```

准备两条映射数据，mock：
- `GET /v0/admin/model-mappings`
- 相关 providers/user-groups/provider-api-keys 请求
- `DELETE /v0/admin/model-mappings/:id`

测试步骤：
1. 勾选两条映射
2. 点击批量删除按钮
3. 确认弹窗中点击 `Delete`
4. 断言两条删除请求都已发送
5. 断言两条行都从页面消失
6. 断言已选数量归零

**Step 2: 写失败测试（部分删除失败）**

增加第二个用例：
- 第一条 `DELETE` 成功
- 第二条 `DELETE` reject

断言：
1. 成功条目从页面消失
2. 失败条目仍在页面中
3. 已选数量变为 `1`

**Step 3: 跑测试确认失败**

Run: `npm test -- modelMappingsBulkDelete.test.tsx`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: FAIL（因为页面还没有批量删除按钮和处理逻辑）。

---

### Task 2: 在 Models.tsx 中实现批量删除最小逻辑

**Files:**
- Modify: `web/src/pages/admin/Models.tsx:2020-2201`
- Test: `web/src/pages/admin/modelMappingsBulkDelete.test.tsx`

**Step 1: 增加 handleBulkDelete()**

在 `handleBulkSetEnabled` 附近新增：

```tsx
const handleBulkDelete = () => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0 || !canDeleteMapping) return;

    setConfirmDialog({
        title: t('Delete Model Mappings'),
        message: t('Are you sure you want to {{action}} {{count}} model mapping(s)?', {
            action: t('Delete'),
            count: ids.length,
        }),
        confirmText: t('Delete'),
        danger: true,
        onConfirm: async () => {
            const results = await Promise.allSettled(
                ids.map((id) => apiFetchAdmin(`/v0/admin/model-mappings/${id}`, { method: 'DELETE' }))
            );

            const successIds = ids.filter((_, index) => results[index]?.status === 'fulfilled');
            const failedIds = ids.filter((_, index) => results[index]?.status === 'rejected');
            const successSet = new Set(successIds);

            setMappings((prev) => prev.filter((item) => !successSet.has(item.id)));
            setSelectedIds(new Set(failedIds));
            setConfirmDialog(null);
        },
    });
};
```

要求：
- 不调用 `fetchData()` 全量刷新
- 仅按成功/失败结果更新本地状态
- 失败仅 `console.error`

**Step 2: 在批量操作区新增删除按钮**

在批量启用/停用按钮后增加红色按钮：

```tsx
<button
    type="button"
    onClick={handleBulkDelete}
    disabled={selectedCount === 0 || !canDeleteMapping}
    className="flex items-center gap-2 px-3 py-2 text-sm rounded-lg font-medium transition-colors bg-red-600 hover:bg-red-700 text-white disabled:opacity-50 disabled:cursor-not-allowed"
    title={t('Bulk delete selected')}
>
    <Icon name="delete" size={18} />
    <span>{t('Delete')}</span>
</button>
```

**Step 3: 跑新增测试确认通过**

Run: `npm test -- modelMappingsBulkDelete.test.tsx`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

---

### Task 3: 做页面级回归验证

**Files:**
- Modify (if needed): `web/src/pages/admin/modelMappingsBulkDelete.test.tsx`
- Reference: `web/src/pages/admin/Models.tsx`

**Step 1: 复核权限和禁用态**

补一个轻量断言（可放在现有测试里）：
- 当 `selectedCount === 0` 时，批量删除按钮是 disabled

**Step 2: 跑相关测试集合**

Run: `npm test -- modelMappingsBulkDelete.test.tsx`

Expected: PASS。

**Step 3: 跑前端构建**

Run: `npm run build`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

---

### Task 4: 最终核对与提交

**Files:**
- Modify: `web/src/pages/admin/Models.tsx`
- Create: `web/src/pages/admin/modelMappingsBulkDelete.test.tsx`

**Step 1: 最终核对需求清单**

确认以下都满足：
1. 有批量删除按钮  
2. 使用现有单条删除接口  
3. 逐条删、失败保留  
4. 不新增后端接口  
5. 不修改权限模型  

**Step 2: 提交**

```bash
git add web/src/pages/admin/Models.tsx web/src/pages/admin/modelMappingsBulkDelete.test.tsx
git commit -m "feat(admin): add bulk delete for model mappings"
```

---

## 完成定义（DoD）

1. 管理员面板模型映射页出现“批量删除所选”按钮。  
2. 多选后可删除多条映射。  
3. 部分删除失败时，成功项消失、失败项保留且仍选中。  
4. `npm test -- modelMappingsBulkDelete.test.tsx` 与 `npm run build` 通过。
