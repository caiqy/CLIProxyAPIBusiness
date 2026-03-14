# Copilot 请求头改造设计（auth 文件来源）

## 背景

当前 GitHub Copilot 上游请求中，`Vscode-Abexpcontext`、`Vscode-Machineid` 等请求头来自入站请求白名单透传；`Editor-Device-Id` 尚未由当前实现发送。根据抓包与现有逻辑对照，需要将以下 3 个请求头的来源统一调整为 auth 文件，而不是入站请求头：

- `Editor-Device-Id`
- `Vscode-Abexpcontext`
- `Vscode-Machineid`

同时，这次改造要求同时覆盖两条 auth 数据进入链路：

- 普通 auth 文件创建 / 更新
- provider-driven import

## 目标

1. 这 3 个 Copilot 请求头仅从 auth 文件内容派生。
2. 普通 auth 文件与 provider-driven import 都能写入并生效。
3. 运行时不再从入站请求头透传这 3 个字段。
4. 不引入通用 header 注入机制，保持最小改动范围。

## 非目标

1. 本次不改造其他 Copilot 请求头来源。
2. 本次不引入通用的 `headers` / `copilot_headers` 机制。
3. 本次不变更 watcher 的整体 metadata 合成模式。

## 设计决策

### 1. auth 文件字段命名

在 auth 文件顶层使用以下字段：

- `editor_device_id`
- `vscode_abexpcontext`
- `vscode_machineid`

选择顶层字段而不是嵌套结构，原因是现有运行时 `auth.Metadata` 直接来源于 auth `Content` 的反序列化结果，executor 读取顶层字段最直接，且与现有 `access_token` 等键的读取方式一致。

### 2. 普通 auth 文件创建 / 更新

普通 auth 文件创建、更新、导入时，这 3 个字段无需增加特殊存储逻辑；它们会作为 `Content` 的一部分落库，并在 watcher 合成运行时 `auth` 时进入 `auth.Metadata`。

### 3. provider-driven import

provider-driven import 当前依赖白名单归一化，且已经具备“顶层优先、`entry.metadata.<field>` 回填”的通用取值逻辑。本次只需要：

1. 将这 3 个字段加入可导入字段白名单。
2. 继续复用现有通用取值逻辑，不新增专门分支。

这样可以兼容以下两类输入：

- 顶层直接提供字段
- 在导入条目的 `metadata` 中提供字段

### 4. 运行时 header 映射

GitHub Copilot 当前的 header 拼装集中在 `applyHeaders()`，但该函数现有签名拿不到 `auth`。因此本次设计同时包含调用链调整：

1. 让 `applyHeaders()` 能访问 `auth.Metadata`（最直接的做法是为其增加 `auth *cliproxyauth.Auth` 入参）。
2. 同步修改所有调用入口，至少覆盖：
   - `PrepareRequest`
   - `Execute`
   - `ExecuteStream`

随后在 header 拼装阶段增加显式映射：

- `auth.Metadata["editor_device_id"]` -> `Editor-Device-Id`
- `auth.Metadata["vscode_abexpcontext"]` -> `Vscode-Abexpcontext`
- `auth.Metadata["vscode_machineid"]` -> `Vscode-Machineid`

这 3 个头的写出行为遵循：

- 有值时发送
- 无值时不发送
- 不做默认值生成
- 不回退到入站请求头
- 仅接受非空字符串值；空字符串、非字符串值一律视为缺失

### 5. 透传策略调整

从 GitHub Copilot executor 当前的入站 header 白名单中移除以下 3 项：

- `Vscode-Abexpcontext`
- `Vscode-Machineid`
- `Editor-Device-Id`（当前未在白名单中，但会新增显式映射，故不应再考虑透传）

其中 `Editor-Device-Id` 当前本来就未透传，本次新增后应仅受 auth 文件控制。

## 数据流

### 普通 auth 文件

1. 管理端创建 / 更新 auth 文件。
2. 字段写入 `models.Auth.Content`。
3. watcher 将 `Content` 反序列化到运行时 `auth.Metadata`。
4. Copilot executor 从 `auth.Metadata` 取值并设置上游请求头。

### provider-driven import

1. 导入逻辑按白名单读取条目字段。
2. 若顶层缺失，则尝试从 `entry.metadata.<field>` 回填。
3. 归一化后的字段写入 `Content`。
4. watcher 合成到 `auth.Metadata`。
5. Copilot executor 设置对应请求头。

## 兼容性与风险

### 兼容性

这次改造只影响 3 个明确指定的请求头，不改变其他 Copilot 请求头逻辑。

对旧 `github-copilot` auth 而言，这是一项有意的行为收敛：若 auth 文件中未补齐这 3 个字段，上线后将不再发送对应 header。

### 风险

1. 旧 auth 文件未配置这些字段时，请求将不再依赖入站透传，因此对应 header 会缺失。
2. provider-driven import 若未同步扩展白名单，会导致导入后运行时拿不到字段。

### 风险缓解

1. 为 provider-driven import 增加覆盖测试。
2. 为 executor 增加“metadata 优先生效、忽略入站同名头”的测试。
3. 对缺失字段场景增加测试，确保不会发送空 header。
4. 发布前检查现有 `github-copilot` auth 是否已完成字段补齐；若未补齐，则需先 backfill 或接受 header 缺失行为变化。

## 测试策略

至少覆盖以下用例：

1. `applyHeaders()` 在 `auth.Metadata` 含 3 个字段时，正确设置对应请求头。
2. 即使入站请求头带有同名值，也不会覆盖 auth 文件中的值。
3. 当 `auth.Metadata` 缺少其中部分字段时，仅发送存在值的 header。
4. `PrepareRequest` / 直接 HTTP 请求路径至少有一条用例，确保调用链改动后行为一致。
5. provider-driven import 能分别覆盖：
   - 顶层字段输入
   - `entry.metadata` 回填输入
6. 普通 auth 文件创建 / 更新链路保留这 3 个字段。
7. 普通 auth 文件导入路径保留这 3 个字段。
8. 空字符串、非字符串、缺字段时不发送对应 header。

## 实施范围

预期涉及以下位置：

- `internal/http/api/admin/handlers/auth_files_provider_import.go`
- `third_party/CLIProxyAPIPlus/internal/runtime/executor/github_copilot_executor.go`
- 相关测试文件（import / executor）

普通 auth 文件创建 / 更新逻辑预计无需新增业务分支，只需通过测试确认字段未被过滤。
