# 管理员面板认证文件内容编辑 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在管理员面板现有 `Edit Auth File` 弹窗中增加 `content` 原始 JSON 编辑能力，并在前端实时阻止非法 JSON、非对象 JSON 和顶层 `proxy_url` 冲突保存。

**Architecture:** 继续复用 `web/src/pages/admin/AuthFiles.tsx` 的编辑弹窗与 `PUT /v0/admin/auth-files/:id` 更新链路，不新增页面或接口。实现上先补纯函数帮助方法与单元测试，再补弹窗级交互测试，最后在同一文件中加入 JSON 编辑状态、实时校验、失败提示和本地列表同步更新。

**Tech Stack:** React 19, TypeScript, Vitest, React Testing Library, Vite

---

## File Structure

- Modify: `web/src/pages/admin/AuthFiles.tsx:322-424`
  - 在现有 helper 区新增 `content` 编辑/校验相关纯函数与 payload 扩展。
- Modify: `web/src/pages/admin/AuthFiles.tsx:679-699`
  - 为编辑弹窗增加 JSON 文本、校验错误、提交错误等 state。
- Modify: `web/src/pages/admin/AuthFiles.tsx:1136-1255`
  - 扩展 `handleEdit`、`handleEditSave`、`handleEditClose` 的初始化、校验、提交和失败回填行为。
- Modify: `web/src/pages/admin/AuthFiles.tsx:3077-3305`
  - 在现有 `Edit Auth File` 弹窗中加入 `Content (JSON)` 文本框、错误提示和禁用态逻辑。
- Create: `web/src/pages/admin/authFilesContentConfig.test.ts`
  - 覆盖纯 helper：格式化初始值、结构校验、顶层冲突校验、payload 带 `content`。
- Create: `web/src/pages/admin/authFilesContentEdit.test.tsx`
  - 覆盖编辑弹窗交互：回填内容、合法保存、非法 JSON、非对象 JSON、顶层 `proxy_url` 冲突、失败提示与成功后的本地状态更新。
- Reference: `web/src/pages/admin/authFilesWhitelistConfig.test.ts`
  - 参考现有 helper 测试风格。
- Reference: `web/src/pages/admin/authFilesWhitelistPayload.test.tsx`
  - 参考现有弹窗保存测试风格与 mock 方式。
- Reference: `web/src/pages/admin/AuthFilesProviderImportModal.tsx:355-389`
  - 参考现有 JSON textarea 样式。

## Chunk 1: 纯函数与数据边界

### Task 1: 先把 content 编辑纯函数的失败测试补齐

**Files:**
- Create: `web/src/pages/admin/authFilesContentConfig.test.ts`
- Modify: `web/src/pages/admin/AuthFiles.tsx:322-424`
- Reference: `web/src/pages/admin/authFilesWhitelistConfig.test.ts`

- [ ] **Step 1: 写 helper 失败测试文件**

创建 `web/src/pages/admin/authFilesContentConfig.test.ts`，先只覆盖纯逻辑，不碰 DOM：

```ts
import { describe, expect, it } from 'vitest';

import {
    buildAuthFileUpdatePayload,
    formatAuthFileContentForEdit,
    getAuthFileContentValidation,
} from './AuthFiles';

describe('AuthFiles content edit helpers', () => {
    it('formats object content as indented json', () => {
        expect(formatAuthFileContentForEdit({ type: 'claude' })).toEqual({
            text: '{\n  "type": "claude"\n}',
            error: '',
        });
    });

    it('keeps non-object json visible and marks it invalid', () => {
        expect(formatAuthFileContentForEdit(['a'])).toEqual({
            text: '[\n  "a"\n]',
            error: 'Content must be a JSON object.',
        });
    });

    it('rejects invalid json text', () => {
        const result = getAuthFileContentValidation('{');
        expect(result.parsedContent).toBeNull();
        expect(result.error).toBe('Content must be valid JSON.');
        expect(result.hasConflict).toBe(false);
    });

    it('rejects top-level proxy_url conflicts only', () => {
        const topLevel = getAuthFileContentValidation('{"proxy_url":"http://a"}');
        const nested = getAuthFileContentValidation('{"nested":{"proxy_url":"http://a"}}');
        const caseVariant = getAuthFileContentValidation('{"Proxy_URL":"http://a"}');

        expect(topLevel.hasConflict).toBe(true);
        expect(topLevel.error).toContain('proxy_url');
        expect(nested.hasConflict).toBe(false);
        expect(nested.error).toBe('');
        expect(caseVariant.hasConflict).toBe(false);
    });

    it('includes content in update payload', () => {
        const payload = buildAuthFileUpdatePayload({
            name: 'auth-a',
            key: 'auth-a',
            isAvailable: true,
            proxyUrl: '',
            rateLimit: 0,
            priority: 0,
            whitelistEnabled: false,
            allowedModels: [],
            content: { type: 'claude', access_token: 'token' },
        });

        expect(payload.content).toEqual({ type: 'claude', access_token: 'token' });
    });
});
```

- [ ] **Step 2: 跑 helper 测试确认失败**

Run: `npm test -- authFilesContentConfig.test.ts`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: FAIL，原因应为 `AuthFiles.tsx` 还没有导出 `formatAuthFileContentForEdit` / `getAuthFileContentValidation`，且 `buildAuthFileUpdatePayload` 还不支持 `content`。

- [ ] **Step 3: 在 AuthFiles.tsx 中实现最小 helper**

在 `buildAuthFileUpdatePayload` 附近新增一个明确、可测试的纯函数边界：

```ts
const AUTH_FILE_CONTENT_CONFLICT_KEYS = new Set(['proxy_url']);

export function formatAuthFileContentForEdit(content: unknown) {
    try {
        return {
            text: JSON.stringify(content ?? {}, null, 2),
            error:
                content !== null && typeof content === 'object' && !Array.isArray(content)
                    ? ''
                    : 'Content must be a JSON object.',
        };
    } catch {
        return {
            text: '{}',
            error: 'Current content could not be formatted. Initialized with an empty object.',
        };
    }
}

export function getAuthFileContentValidation(text: string) {
    try {
        const parsed = JSON.parse(text);
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
            return {
                parsedContent: null,
                error: 'Content must be a JSON object.',
                hasConflict: false,
            };
        }

        const topLevelKeys = Object.keys(parsed);
        const conflictKey = topLevelKeys.find((key) => AUTH_FILE_CONTENT_CONFLICT_KEYS.has(key));
        if (conflictKey) {
            return {
                parsedContent: null,
                error: `${conflictKey} must be edited with the dedicated form field.`,
                hasConflict: true,
            };
        }

        return {
            parsedContent: parsed as Record<string, unknown>,
            error: '',
            hasConflict: false,
        };
    } catch {
        return {
            parsedContent: null,
            error: 'Content must be valid JSON.',
            hasConflict: false,
        };
    }
}
```

同时扩展 `BuildAuthFileUpdatePayloadInput` 和 `buildAuthFileUpdatePayload(...)`：

```ts
interface BuildAuthFileUpdatePayloadInput {
    // ...existing fields...
    content?: Record<string, unknown>;
}

if (input.content) {
    payload.content = input.content;
}
```

要求：
- 冲突检测只扫**顶层**键。
- 只把 `proxy_url` 当作本次冲突键。
- 不在 helper 里做嵌套扫描或“自动修复”。

- [ ] **Step 4: 跑 helper 测试确认通过**

Run: `npm test -- authFilesContentConfig.test.ts`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

- [ ] **Step 5: 提交 helper 相关改动**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesContentConfig.test.ts
git commit -m "test(admin): cover auth file content edit helpers"
```

## Chunk 2: 弹窗交互与页面回归

### Task 2: 先写编辑弹窗交互失败测试

**Files:**
- Create: `web/src/pages/admin/authFilesContentEdit.test.tsx`
- Modify: `web/src/pages/admin/AuthFiles.tsx:679-699`
- Modify: `web/src/pages/admin/AuthFiles.tsx:1136-1255`
- Modify: `web/src/pages/admin/AuthFiles.tsx:3077-3305`
- Reference: `web/src/pages/admin/authFilesWhitelistPayload.test.tsx`

- [ ] **Step 1: 写成功与校验类失败测试**

创建 `web/src/pages/admin/authFilesContentEdit.test.tsx`，准备一个基础认证文件：

```ts
const BASE_AUTH_FILE = {
    id: 1,
    key: 'auth-a',
    name: 'auth-a',
    auth_group_id: [],
    auth_group: [],
    proxy_url: '',
    content: { type: 'claude', access_token: 'old-token' },
    whitelist_enabled: false,
    allowed_models: [],
    excluded_models: [],
    is_available: true,
    rate_limit: 0,
    priority: 0,
    created_at: '2026-03-01T00:00:00Z',
    updated_at: '2026-03-01T00:00:00Z',
};
```

至少写以下用例：

1. 打开编辑弹窗后，JSON 文本框展示格式化后的 `content`。
   - 同时断言 `Content (JSON)` 标题可见。
2. 把 JSON 改成合法对象后点击 Save，断言 `PUT /v0/admin/auth-files/1` 请求体既包含 `content`，也继续包含现有受管字段（至少断言 `name`、`key`、`proxy_url`、`rate_limit`、`priority` 仍在 payload 中）。
3. 输入非法 JSON（如 `{`）时，显示 `Content must be valid JSON.`，且 Save disabled。
4. 输入 `[]` 时，显示 `Content must be a JSON object.`，且 Save disabled。
5. 输入 `{"proxy_url":"http://conflict"}` 时，显示冲突提示，且 Save disabled。
6. 当初始 `content` 为 `{"proxy_url":"http://legacy"}` 时，弹窗一打开就显示冲突提示，且 Save disabled。
7. 当初始 `content` 为 `[]` 时，弹窗仍可打开，textarea 中保留 `[]`，并立即显示 `Content must be a JSON object.`。

对第 3/4/5/6/7 类校验失败场景，都额外断言：

- `expect(putBodies).toHaveLength(0)`
- 若尝试点击 Save，仍不应发送 `PUT /v0/admin/auth-files/1`

可直接按现有测试风格 mock：

```ts
vi.spyOn(apiConfig, 'apiFetchAdmin').mockImplementation(async (endpoint, options) => {
    if (endpoint === '/v0/admin/auth-files') {
        return { auth_files: [BASE_AUTH_FILE] } as never;
    }
    if (endpoint === '/v0/admin/auth-files/model-presets?type=claude') {
        return { supported: false, reason: '', reason_code: 'unsupported_auth_type', models: [] } as never;
    }
    if (endpoint === '/v0/admin/auth-files/1' && options?.method === 'PUT') {
        putBodies.push(JSON.parse(String(options.body || '{}')));
        return { ok: true } as never;
    }
    throw new Error(`Unexpected endpoint: ${String(endpoint)}`);
});
```

- [ ] **Step 2: 跑交互测试确认失败**

Run: `npm test -- authFilesContentEdit.test.tsx`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: FAIL，因为页面还没有 `Content (JSON)` 文本框、实时校验和冲突提示。

- [ ] **Step 3: 在弹窗中实现最小可用 JSON 编辑能力**

在 `AuthFiles.tsx` 中加入以下状态：

```ts
const [editContentText, setEditContentText] = useState('{}');
const [editContentError, setEditContentError] = useState('');
const [editContentNotice, setEditContentNotice] = useState('');
const [editSubmitError, setEditSubmitError] = useState('');
```

在 `handleEdit(file)` 中使用 helper 初始化：

```ts
const formatted = formatAuthFileContentForEdit(file.content);
const initialValidation = getAuthFileContentValidation(formatted.text);
setEditContentText(formatted.text);
setEditContentError(
    formatted.error === 'Current content could not be formatted. Initialized with an empty object.'
        ? initialValidation.error
        : initialValidation.error || formatted.error
);
setEditContentNotice(formatted.error === 'Current content could not be formatted. Initialized with an empty object.' ? formatted.error : '');
setEditSubmitError('');
```

要求：
- 打开弹窗时就要立刻根据 `formatted.text` 执行一次 `getAuthFileContentValidation(...)`。
- 这样历史数据里若顶层已存在 `proxy_url`，或 `content` 本身是非对象 JSON，弹窗一打开就能立即显示阻塞错误并禁用 Save。
- `editContentError` 只承载**阻塞保存**的错误（非法 JSON、非对象 JSON、顶层 `proxy_url` 冲突）。
- `editContentNotice` 只承载**非阻塞提示**（例如无法格式化时回退为 `{}`）。
- Save 禁用条件只能依赖 `editContentError`，不能因为 notice 而禁用。

在 `handleEditSave()` 里，在 `buildAuthFileUpdatePayload(...)` 之前增加：

```ts
const contentValidation = getAuthFileContentValidation(editContentText);
if (contentValidation.error || !contentValidation.parsedContent) {
    setEditContentError(contentValidation.error);
    return;
}

const payload = buildAuthFileUpdatePayload({
    name: trimmedName,
    key: trimmedKey,
    isAvailable: editIsAvailable,
    proxyUrl,
    rateLimit,
    priority,
    authGroupIds: canListGroups ? normalizedEditGroupIds : undefined,
    includeWhitelistFields: editWhitelistSupported && editWhitelistDirty,
    whitelistEnabled: whitelistSave.whitelistEnabled,
    allowedModels: whitelistSave.allowedModels,
    content: contentValidation.parsedContent,
});
```

在 textarea 的 `onChange` 中做实时校验：

```tsx
onChange={(e) => {
    const next = e.target.value;
    setEditContentText(next);
    setEditSubmitError('');
    setEditContentNotice('');
    const validation = getAuthFileContentValidation(next);
    setEditContentError(validation.error);
}}
```

并在 Save 按钮禁用条件里加入：

```tsx
disabled={editSaving || !editName.trim() || !editKey.trim() || !!editContentError}
```

文本框 UI 可直接复用项目已有 textarea 风格：

```tsx
<textarea
    value={editContentText}
    onChange={...}
    className="block w-full h-72 p-3 text-sm font-mono text-slate-900 dark:text-white bg-gray-50 dark:bg-background-dark border border-gray-300 dark:border-border-dark rounded-lg focus:ring-primary focus:border-primary"
/>
```

在文本框下方分别渲染：

- `editContentNotice`：非阻塞提示，使用普通提示样式
- `editContentError`：阻塞错误，使用红色错误样式

文案优先使用 helper 返回值，避免把规则分散到 JSX。

- [ ] **Step 4: 重新跑交互测试确认通过**

Run: `npm test -- authFilesContentEdit.test.tsx`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

- [ ] **Step 5: 提交弹窗 JSON 编辑功能**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesContentEdit.test.tsx
git commit -m "feat(admin): add auth file content editor"
```

### Task 3: 补失败提示与本地状态回归

**Files:**
- Modify: `web/src/pages/admin/authFilesContentEdit.test.tsx`
- Modify: `web/src/pages/admin/AuthFiles.tsx:1159-1235`

- [ ] **Step 1: 补请求失败与本地状态更新测试**

在 `authFilesContentEdit.test.tsx` 里再增加两个用例：

1. `PUT /v0/admin/auth-files/1` reject 时：
   - 弹窗仍然打开
   - 当前 textarea 输入保留
   - 页面显示用户可见的失败提示（例如 `Failed to update auth file.`）
2. `PUT` 成功后：
   - 行列表中的该条数据在重新打开编辑弹窗时显示更新后的 `content`

- [ ] **Step 2: 在保存失败路径增加用户可见反馈**

最小实现建议：

```ts
catch (err) {
    console.error('Failed to update auth file:', err);
    setEditSubmitError(t('Failed to update auth file.'));
}
```

并在弹窗底部按钮区上方增加：

```tsx
{editSubmitError ? (
    <div className="text-sm text-red-600 dark:text-red-400">{editSubmitError}</div>
) : null}
```

同时在成功分支更新本地列表项时把 `content` 写回：

```ts
content: contentValidation.parsedContent,
```

`handleEditClose()` 中也要清理 `editContentText`、`editContentError`、`editContentNotice`、`editSubmitError`。

- [ ] **Step 3: 跑两类测试集合**

Run: `npm test -- authFilesContentConfig.test.ts authFilesContentEdit.test.tsx`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

- [ ] **Step 4: 跑前端构建回归**

Run: `npm run build`

Workdir: `d:/Caiqy/Projects/Github/cpab/web`

Expected: PASS。

- [ ] **Step 5: 提交回归与错误处理收尾**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesContentConfig.test.ts web/src/pages/admin/authFilesContentEdit.test.tsx
git commit -m "test(admin): cover auth file content edit flows"
```

---

## 完成定义（DoD）

1. `Edit Auth File` 弹窗中出现 `Content (JSON)` 文本框。
2. 所有认证文件类型都能看到并编辑 `content`。
3. 非法 JSON 会立即显示错误并禁用 Save。
4. 非对象 JSON 会立即显示错误并禁用 Save。
5. 仅当 `content` 顶层出现 `proxy_url` 时触发冲突提示并禁用 Save。
6. 更新失败时，弹窗不关闭、输入不丢失、且有用户可见错误提示。
7. 更新成功后，本地列表状态与再次打开弹窗时看到的 `content` 一致。
8. `npm test -- authFilesContentConfig.test.ts authFilesContentEdit.test.tsx` 与 `npm run build` 通过。
