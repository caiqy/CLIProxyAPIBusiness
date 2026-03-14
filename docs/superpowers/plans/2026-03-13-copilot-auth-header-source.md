# Copilot Auth Header Source Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `Editor-Device-Id`、`Vscode-Abexpcontext`、`Vscode-Machineid` 的来源从入站请求透传改为 auth 文件，并同时支持普通 auth 文件与 provider-driven import。

**Architecture:** 继续复用现有 auth 文件 -> `models.Auth.Content` -> watcher -> `auth.Metadata` 这条链路，不新增通用 header 注入机制。仅在 provider import 白名单中放行这三个字段，并在 GitHub Copilot executor 中显式读取 `auth.Metadata` 来设置上游 header，同时移除这两个现有透传头的白名单来源。

**Tech Stack:** Go, Gin, GORM, `gjson/sjson`, CLIProxyAPIPlus executor tests, admin handler tests

---

## Chunk 1: provider import 支持 auth 字段落库

### Task 1: 扩展 provider import 白名单并验证顶层输入

**Files:**
- Modify: `internal/http/api/admin/handlers/auth_files_provider_import.go`
- Test: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`

- [ ] **Step 1: 写失败测试，验证 github-copilot provider 顶层字段会被保留**

在 `auth_files_provider_import_test.go` 增加一个用例，输入：

```go
map[string]any{
    "access_token":         "gh-at",
    "editor_device_id":     "device-1",
    "vscode_abexpcontext":  "abexp-1",
    "vscode_machineid":     "machine-1",
}
```

断言 `normalizeProviderEntry("github-copilot", raw)` 的结果包含这 3 个字段。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./internal/http/api/admin/handlers -run TestNormalizeProviderEntry_GitHubCopilot_PreservesCopilotHeaderMetadataFromTopLevel -count=1`

Expected: FAIL，提示字段未进入 `normalized`。

- [ ] **Step 3: 最小实现：把 3 个字段加入 `commonImportAllowedFields`**

在 `auth_files_provider_import.go` 中把以下字段加入白名单：

```go
"editor_device_id",
"vscode_abexpcontext",
"vscode_machineid",
```

- [ ] **Step 4: 重新运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestNormalizeProviderEntry_GitHubCopilot_PreservesCopilotHeaderMetadataFromTopLevel -count=1`

Expected: PASS


### Task 2: 验证 provider import 支持 `entry.metadata` 回填

**Files:**
- Test: `internal/http/api/admin/handlers/auth_files_provider_import_test.go`

- [ ] **Step 1: 写回归测试，验证 `entry.metadata` 里的 3 个字段会被回填到顶层**

测试输入示例：

```go
map[string]any{
    "access_token": "gh-at",
    "metadata": map[string]any{
        "editor_device_id":    "device-meta-1",
        "vscode_abexpcontext": "abexp-meta-1",
        "vscode_machineid":    "machine-meta-1",
    },
}
```

断言 `normalized` 顶层包含这 3 个字段，值与 `metadata` 一致。

- [ ] **Step 2: 运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestNormalizeProviderEntry_GitHubCopilot_PullsCopilotHeaderMetadataFromNestedMetadata -count=1`

Expected: PASS；该用例用于固定现有 `pickImportFieldValue()` 的通用行为，确认白名单扩展后 nested metadata 会自动生效。

- [ ] **Step 3: 复用现有通用逻辑，不新增专门分支**

不新增专门分支，继续使用 `pickImportFieldValue()` 的现有逻辑。

- [ ] **Step 4: 重新运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestNormalizeProviderEntry_GitHubCopilot_PullsCopilotHeaderMetadataFromNestedMetadata -count=1`

Expected: PASS

- [ ] **Step 5: 运行 provider import 相关测试集**

Run: `go test ./internal/http/api/admin/handlers -run TestNormalizeProviderEntry_ -count=1`

Expected: PASS


## Chunk 2: executor 从 auth.Metadata 注入请求头

### Task 3: 为 executor 写失败测试，固定新的 header 来源

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`

- [ ] **Step 1: 写失败测试，验证 `auth.Metadata` 中的 3 个字段会写入上游请求头**

建议新增两类测试：

1. `applyHeaders`/构造 header 路径：

```go
auth := &cliproxyauth.Auth{Metadata: map[string]any{
    "access_token":         "gh-access",
    "editor_device_id":     "device-1",
    "vscode_abexpcontext":  "abexp-1",
    "vscode_machineid":     "machine-1",
}}
```

断言输出 header：

- `Editor-Device-Id=device-1`
- `Vscode-Abexpcontext=abexp-1`
- `Vscode-Machineid=machine-1`

2. 覆盖优先级路径：即使入站请求头带了不同值，也必须以 `auth.Metadata` 为准。

- [ ] **Step 2: 运行新增测试并确认失败**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run 'TestApplyHeaders_(UsesAuthMetadataForCopilotContextHeaders|PrefersAuthMetadataOverIncomingHeaders)' -count=1`

Expected: FAIL，原因是当前 `applyHeaders()` 无法访问 `auth.Metadata`。

- [ ] **Step 3: 最小实现：让 header 构造路径可以访问 `auth`**

修改 `github_copilot_executor.go`：

1. 调整 `applyHeaders()` 签名，增加 `auth *cliproxyauth.Auth`
2. 同步更新调用点：
   - `PrepareRequest`
   - `Execute`
   - `ExecuteStream`
3. 在 `applyHeaders()` 中读取以下 metadata 键并写 header：

```go
metaStringValue(auth.Metadata, "editor_device_id")
metaStringValue(auth.Metadata, "vscode_abexpcontext")
metaStringValue(auth.Metadata, "vscode_machineid")
```

仅当值为非空字符串时发送 header。

- [ ] **Step 4: 从入站白名单中移除旧透传来源**

从 `forwardHeaders` 删除：

```go
"Vscode-Abexpcontext"
"Vscode-Machineid"
```

注意：`Editor-Device-Id` 当前没有透传白名单，本次只新增 metadata 映射，不新增透传。

- [ ] **Step 5: 运行针对性测试并确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run 'TestApplyHeaders_(UsesAuthMetadataForCopilotContextHeaders|PrefersAuthMetadataOverIncomingHeaders)' -count=1`

Expected: PASS


### Task 4: 覆盖 `PrepareRequest` 调用链

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`

- [ ] **Step 1: 写失败测试，验证 `PrepareRequest` 非 internal API 路径也会从 `auth.Metadata` 写出这 3 个 header**

测试需要构造：

```go
auth := &cliproxyauth.Auth{Metadata: map[string]any{
    "access_token":         "gh-access",
    "editor_device_id":     "device-1",
    "vscode_abexpcontext":  "abexp-1",
    "vscode_machineid":     "machine-1",
}}
```

并通过可控的 API token 缓存/依赖，断言 `PrepareRequest()` 后请求头包含这 3 个 header。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run TestPrepareRequest_UsesAuthMetadataForCopilotContextHeaders -count=1`

Expected: FAIL，原因是当前 `PrepareRequest()` 调用的 `applyHeaders()` 不读取 `auth.Metadata`。

- [ ] **Step 3: 复用 Task 3 的最小实现，不新增第二套 header 逻辑**

确保 `PrepareRequest()` 走到的仍是同一套 `applyHeaders()` / metadata 读取逻辑。

- [ ] **Step 4: 重新运行测试并确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run TestPrepareRequest_UsesAuthMetadataForCopilotContextHeaders -count=1`

Expected: PASS


### Task 5: 补边界测试，固定缺失/空值行为

**Files:**
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor_test.go`

- [ ] **Step 1: 写测试，验证空字符串、非字符串、缺字段都不会发送对应 header**

示例：

```go
auth := &cliproxyauth.Auth{Metadata: map[string]any{
    "access_token":         "gh-access",
    "editor_device_id":     "",
    "vscode_abexpcontext":  123,
}}
```

断言：

- `Editor-Device-Id` 为空
- `Vscode-Abexpcontext` 为空
- `Vscode-Machineid` 为空

- [ ] **Step 2: 运行测试并确认通过**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run TestApplyHeaders_SkipsInvalidCopilotContextMetadata -count=1`

Expected: PASS

- [ ] **Step 3: 运行 executor 相关回归测试集**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -run TestApplyHeaders_ -count=1`

Expected: PASS


## Chunk 3: 普通 auth 文件链路回归

### Task 6: 验证普通 auth 文件 Create 会保留字段

**Files:**
- Modify or Create Test: `internal/http/api/admin/handlers/auth_files_proxy_test.go`

- [ ] **Step 1: 写回归测试，验证普通 auth 文件 Create 会保留这 3 个字段到 `Content`**

构造 `POST /v0/admin/auth-files` 请求，`content` 至少包含：

```json
{
  "type": "github-copilot",
  "access_token": "gh-at",
  "editor_device_id": "device-1",
  "vscode_abexpcontext": "abexp-1",
  "vscode_machineid": "machine-1"
}
```

创建成功后查询 DB，断言 `Content` 中这 3 个字段仍然存在。

说明：该链路按设计预计无需生产代码改动，本任务用于防止后续过滤逻辑回归，因此新增测试后预期直接 PASS。

- [ ] **Step 2: 运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_Create_PreservesCopilotHeaderMetadata -count=1`

Expected: PASS


### Task 7: 验证普通 auth 文件 Update 会保留字段

**Files:**
- Modify or Create Test: `internal/http/api/admin/handlers/auth_files_proxy_test.go`

- [ ] **Step 1: 写回归测试，验证普通 auth 文件 Update 会保留这 3 个字段到 `Content`**

先插入一条 auth 记录，再通过 `PUT /v0/admin/auth-files/:id` 更新 `content`，断言更新后 `Content` 仍保留这 3 个字段。

说明：该链路按设计预计无需生产代码改动，本任务用于固定现有行为，新增测试后预期直接 PASS。

- [ ] **Step 2: 运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_Update_PreservesCopilotHeaderMetadata -count=1`

Expected: PASS


### Task 8: 验证普通 auth 文件 Import 会保留字段

**Files:**
- Modify or Create Test: `internal/http/api/admin/handlers/auth_files_proxy_test.go`

- [ ] **Step 1: 写回归测试，验证普通 auth 文件 Import 会保留这 3 个字段到 `Content`**

构造一个上传 JSON 文件，内容至少包含：

```json
{
  "key": "gh-copilot-demo",
  "type": "github-copilot",
  "access_token": "gh-at",
  "editor_device_id": "device-1",
  "vscode_abexpcontext": "abexp-1",
  "vscode_machineid": "machine-1"
}
```

导入后查询 DB，断言 `Content` 中这 3 个字段仍然存在。

说明：普通 import 当前直接保留 JSON payload，本任务用于固定这一行为，新增测试后预期直接 PASS。

- [ ] **Step 2: 运行测试并确认通过**

Run: `go test ./internal/http/api/admin/handlers -run TestAuthFiles_Import_PreservesCopilotHeaderMetadata -count=1`

Expected: PASS

- [ ] **Step 3: 运行 admin handler 相关测试集**

Run: `go test ./internal/http/api/admin/handlers -count=1`

Expected: PASS


## Chunk 4: 全量验证与收尾

### Task 9: 运行目标测试集并检查回归

**Files:**
- No code changes

- [ ] **Step 1: 运行 executor 测试**

Run: `go test ./third_party/CLIProxyAPIPlus/internal/runtime/executor -count=1`

Expected: PASS

- [ ] **Step 2: 运行 admin handler 测试**

Run: `go test ./internal/http/api/admin/handlers -count=1`

Expected: PASS

- [ ] **Step 3: 记录上线检查项**

确认以下事实：

1. 旧 `github-copilot` auth 若未补齐 3 个字段，将不再发送对应 header
2. provider-driven import 已支持导入这 3 个字段
3. 普通 auth 文件导入/创建/更新已由测试覆盖
4. 发布前检查现有 `github-copilot` auth 是否已补齐：
   - `editor_device_id`
   - `vscode_abexpcontext`
   - `vscode_machineid`
   若未补齐，则需先 backfill 或接受行为变化

- [ ] **Step 4: 请求代码评审 / 准备提交**

在实现完成后进行代码审查，并在需要时准备提交。
