# Kiro 管理员最小接入 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在管理员平台 AuthFiles 中按既有 provider 惯例接入 Kiro（MVP），支持独立权限、统一 `POST /v0/admin/tokens/kiro` 启动、device code 展示与轮询闭环。

**Architecture:** 采用增量接入，不重构现有认证框架。后端在 admin 路由 + permissions + SDK requester 三层补 Kiro 暴露；前端在 `AuthFiles.tsx` 增加 Kiro provider 与 device code 分支，复用现有轮询与成功后刷新逻辑。通过最小单测保证权限定义与 Kiro requester 暴露正确，并用前端 Vitest + build 验证交互改动。

**Tech Stack:** Go (Gin/GORM)、React + TypeScript + Vitest、i18next。

---

> 说明：你已明确“本次不使用 worktree”，本计划默认在当前分支执行。

> 执行约束：每个任务按 **TDD 小步** 执行（先失败测试，再最小实现，再验证通过，再提交）。

### Task 1: 暴露 Kiro requester 方法（SDK 层）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/sdk/api/management.go`
- Create: `third_party/CLIProxyAPIPlus/sdk/api/management_kiro_test.go`

**Step 1: 写失败测试（接口应暴露 RequestKiroToken）**

```go
package api

import (
    "testing"

    "github.com/gin-gonic/gin"
)

func TestManagementTokenRequesterExposesKiroMethod(t *testing.T) {
    t.Parallel()

    var requester any = (*managementTokenRequester)(nil)
    if _, ok := requester.(interface{ RequestKiroToken(*gin.Context) }); !ok {
        t.Fatalf("managementTokenRequester must implement RequestKiroToken")
    }
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/api -run KiroMethod -count=1 -v`

Expected: FAIL（当前 `managementTokenRequester` 未实现 `RequestKiroToken`）。

**Step 3: 最小实现**

在 `management.go` 增加：

1. `ManagementTokenRequester` 接口方法：

```go
RequestKiroToken(*gin.Context)
```

2. `managementTokenRequester` 实现：

```go
func (m *managementTokenRequester) RequestKiroToken(c *gin.Context) {
    m.handler.RequestKiroToken(c)
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/api -run KiroMethod -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/sdk/api/management.go third_party/CLIProxyAPIPlus/sdk/api/management_kiro_test.go
git commit -m "feat(sdk): expose kiro token requester method"
```

---

### Task 2: 注册管理员 Kiro 路由并新增权限定义

**Files:**
- Modify: `internal/http/api/admin/admin.go`
- Modify: `internal/http/api/admin/permissions/permissions.go`
- Create: `internal/http/api/admin/permissions/token_kiro_permissions_test.go`

**Step 1: 写失败测试（权限定义包含 Kiro endpoint）**

```go
package permissions

import "testing"

func TestDefinitionMapIncludesKiroTokenPermission(t *testing.T) {
    t.Parallel()

    key := "POST /v0/admin/tokens/kiro"
    if _, ok := DefinitionMap()[key]; !ok {
        t.Fatalf("DefinitionMap() missing permission key %q", key)
    }
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./internal/http/api/admin/permissions -run KiroTokenPermission -count=1 -v`

Expected: FAIL（权限定义尚未包含 Kiro）。

**Step 3: 最小实现**

1. 在 `permissions.go` 新增：

```go
newDefinition("POST", "/v0/admin/tokens/kiro", "Request Kiro Token", "Auth Tokens"),
```

2. 在 `admin.go` token 路由区新增：

```go
authed.POST("/tokens/kiro", withOAuthCallbackDefaults(tokenRequester.RequestKiroToken))
```

放在现有 `/tokens/*` 路由段，保持风格一致。

**Step 4: 运行测试确认通过**

Run: `go test ./internal/http/api/admin/permissions -run KiroTokenPermission -count=1 -v`

Expected: PASS。

**Step 5: Commit**

```bash
git add internal/http/api/admin/admin.go internal/http/api/admin/permissions/permissions.go internal/http/api/admin/permissions/token_kiro_permissions_test.go
git commit -m "feat(admin): add kiro token route and permission"
```

---

### Task 3: 前端接入 Kiro provider 启动流程（最小侵入）

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Create: `web/src/pages/admin/authFilesKiroFlow.test.ts`

**Step 1: 写失败测试（Kiro 响应归一化）**

先在 `AuthFiles.tsx` 中抽出纯函数（建议同文件导出，避免过度拆分）：

```ts
export function normalizeTokenStartResponse(input: unknown): {
  state: string;
  url?: string;
  method?: string;
} {
  // impl later
}
```

测试示例：

```ts
import { describe, expect, it } from 'vitest';
import { normalizeTokenStartResponse } from './AuthFiles';

describe('normalizeTokenStartResponse', () => {
  it('supports kiro device_code response without url', () => {
    const out = normalizeTokenStartResponse({ state: 's1', method: 'device_code' });
    expect(out.state).toBe('s1');
    expect(out.url).toBeUndefined();
    expect(out.method).toBe('device_code');
  });
});
```

**Step 2: 运行测试确认失败**

Run: `npm run test -- authFilesKiroFlow.test.ts`（workdir=`web`）

Expected: FAIL（函数未实现或行为不符合预期）。

**Step 3: 最小实现**

在 `AuthFiles.tsx` 中完成以下改动：

1. `AUTH_TYPES` 增加：

```ts
{ key: 'kiro', label: 'Kiro', endpoint: '/v0/admin/tokens/kiro' }
```

2. 扩展启动响应类型，允许 `url` 可选、带 `method`。
3. `handleNewAuthType` 中：
   - 支持 Kiro 启动返回仅 `state` 的情况；
   - 当 `typeKey === 'kiro'` 且拿到 `state` 时，**自动调用 `startPolling(state)`**（不依赖点击 Copy/Open URL）。

**Step 4: 运行测试确认通过**

Run: `npm run test -- authFilesKiroFlow.test.ts`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/pages/admin/authFilesKiroFlow.test.ts
git commit -m "feat(web): add kiro provider start flow in auth files"
```

---

### Task 4: 轮询兼容 device_code 并渲染设备码卡片

**Files:**
- Modify: `web/src/pages/admin/AuthFiles.tsx`
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh-CN.ts`
- Create: `web/src/pages/admin/authFilesKiroStatus.test.ts`

**Step 1: 写失败测试（device_code 状态处理）**

建议新增纯函数：

```ts
export function isDeviceCodeStatus(s: { status?: string }): boolean {
  return s.status === 'device_code';
}
```

或为状态分支函数写测试，至少覆盖：

- `status=device_code` 时识别成功；
- `status=wait/ok/error` 时不误判。

**Step 2: 运行测试确认失败**

Run: `npm run test -- authFilesKiroStatus.test.ts`（workdir=`web`）

Expected: FAIL。

**Step 3: 最小实现**

1. 扩展 `AuthStatusResponse`：

```ts
status: 'ok' | 'wait' | 'error' | 'device_code' | 'auth_url';
verification_url?: string;
user_code?: string;
url?: string;
```

2. `startPolling` 新增 `device_code` 分支：
   - 保持 `authStatus='polling'`；
   - 将 `verification_url`、`user_code` 写入组件状态；
   - 不停止轮询。

3. 在弹窗中新增 Kiro 设备码展示区（条件渲染）：
   - `verification_url`（可打开）；
   - `user_code`（可复制，复用 `copyText`）；
   - 字段缺失时显示通用引导文案，不中断流程。

4. 增加 i18n 词条（en/zh）：
   - `Verification URL`
   - `User Code`
   - `Copy Code`
   - `Use this code to complete Kiro sign-in`

**Step 4: 运行测试与构建确认通过**

Run: `npm run test -- authFilesKiroStatus.test.ts`（workdir=`web`）

Expected: PASS。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 5: Commit**

```bash
git add web/src/pages/admin/AuthFiles.tsx web/src/locales/en.ts web/src/locales/zh-CN.ts web/src/pages/admin/authFilesKiroStatus.test.ts
git commit -m "feat(web): support kiro device-code polling and ui"
```

---

### Task 5: 回归验证与需求对齐收口

**Files:**
- Modify (if needed): `docs/plans/2026-02-18-kiro-admin-minimal-design.md`

**Step 1: 后端测试回归**

Run: `go test ./third_party/CLIProxyAPIPlus/sdk/api ./internal/http/api/admin/permissions -v`

Expected: PASS。

**Step 2: 前端回归**

Run: `npm run test`（workdir=`web`）

Expected: PASS（至少本次新增用例通过）。

Run: `npm run lint`（workdir=`web`）

Expected: 无本次新增 lint 报错。

Run: `npm run build`（workdir=`web`）

Expected: PASS。

**Step 3: 全仓基础回归**

Run: `go test ./...`

Expected: PASS（若存在历史基线失败，需在结果中明确区分与本改动无关项）。

**Step 4: 需求核对清单**

- [ ] AuthFiles 菜单出现 Kiro（受 `POST /v0/admin/tokens/kiro` 权限控制）
- [ ] 点击 Kiro 可成功启动并进入轮询
- [ ] device code（`verification_url`/`user_code`）可展示与复制
- [ ] 认证成功后关闭弹窗并刷新认证文件列表
- [ ] 现有 provider（至少 1 个）流程不受影响

**Step 5: 最终提交（如前面未分批提交）**

```bash
git add -A
git commit -m "feat(admin): add minimal kiro auth flow in auth files"
```
