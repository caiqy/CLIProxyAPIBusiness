# 认证文件代理运行时对齐设计

## 1. 目标与边界

### 1.1 目标

修复“管理员面板中认证文件 `proxy_url` 已写入数据库，但运行时请求未实际使用该值”的不一致问题，使认证文件专属代理真正按预期生效：

- 管理面板编辑的 `proxy_url` 必须进入运行时 `auth.ProxyURL`
- 上游请求必须继续遵循既有优先级：`auth.ProxyURL > 全局 proxy-url > 直连`
- 当认证文件专属代理配置错误时，如果请求命中该认证，应体现为请求失败，而不是悄悄回退到全局代理或直连

### 1.2 已确认边界

- 运行时以 **`models.Auth.ProxyURL` 为权威来源**
- `content.proxy_url` 只保留为 **兼容旧数据的回退来源**
- `auth-files` 的创建、更新、导入入口统一改为 **严格校验**
- 允许的 scheme 仅：`http`、`https`、`socks5`
- 允许空字符串，表示清空认证文件专属代理
- 不做数据库迁移，不批量回填历史 JSON 中的 `proxy_url`

## 2. 问题根因

当前实现存在两条不一致的数据链路：

1. 管理面板保存认证文件时，`proxy_url` 写入 `models.Auth.ProxyURL`
2. 运行时 watcher 重建内存认证对象时，却没有读取数据库列 `proxy_url`，而是从 `content` JSON 的 `proxy_url` 读取

结果是：

- 面板展示值来自数据库列
- 运行时生效值来自 JSON 元数据
- 两者可能不一致

因此，管理员面板里给认证文件填了错误端口，但请求仍成功，最常见的原因不是“错误代理还能通”，而是“运行时根本没用到这个专属代理”，于是回退到了全局代理或直连。

## 3. 方案对比

### 方案 A（采纳）：DB 列权威 + 运行时兼容回退

规则：

- 运行时优先读取 `models.Auth.ProxyURL`
- 当数据库列为空时，再兼容读取 `content.proxy_url`
- 管理面板及导入接口统一严格校验

**优点**

- 与管理员面板语义一致
- 改动集中在 watcher 与 auth-files handler
- 不破坏旧数据，兼容成本低
- 以后“页面显示值”和“运行时实际值”一致

**代价**

- 需要补 watcher 查询字段和重建逻辑
- 需要为 auth-files 多个入口补齐统一校验

### 方案 B（不采纳）：双写 DB 列与 JSON

规则：

- 管理面板同时写 `models.Auth.ProxyURL` 与 `content.proxy_url`
- 运行时可继续读 JSON 或 DB

**问题**

- 两份同义数据容易漂移
- 后续修改、导入、修复时复杂度更高
- 本质上没有消除“单一权威来源不明确”的问题

### 方案 C（不采纳）：纯 DB 权威 + 立即迁移旧数据

规则：

- 完全废弃 `content.proxy_url`
- 先做历史数据迁移，再只读数据库列

**问题**

- 需要额外迁移/回填步骤
- 对当前问题来说实现成本偏高
- 不是最小必要改动

## 4. 架构设计

### 4.1 权威数据源

认证文件代理的权威来源定义为：

- `internal/models/auth.go` 中 `models.Auth.ProxyURL`

新写入的数据统一落在该列中。

### 4.2 运行时加载链路

修正 `internal/watcher/watcher.go` 的认证重载路径：

1. `pollAuth()` 查询 auth 记录时，把 `proxy_url` 列一并查出
2. `synthesizeAuthFromDBRow(...)` 增加数据库代理参数
3. 生成运行时 `coreauth.Auth` 时按以下顺序取值：

   1. `models.Auth.ProxyURL`
   2. `content.proxy_url`（仅兼容旧数据）
   3. 空字符串

最终进入运行时的 `auth.ProxyURL` 必须反映这一决策。

### 4.3 请求生效链路

上游请求客户端构造逻辑保持不变：

- `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers.go`
- 继续使用：`auth.ProxyURL > cfg.ProxyURL > 直连/上下文 transport`

本次修复不改变优先级，只修复 `auth.ProxyURL` 未被正确装载的问题。

## 5. 输入校验设计

### 5.1 严格校验入口

以下入口统一对认证文件 `proxy_url` 使用严格校验：

- `internal/http/api/admin/handlers/auth_files.go`
  - `Create`
  - `Update`
  - `Import`
- `internal/http/api/admin/handlers/auth_files_provider_import.go`
  - `normalizeProviderEntry` / `ImportByProvider` 对应路径

### 5.2 校验规则

- 允许：`http://`、`https://`、`socks5://`
- 允许：用户名密码形式
- 允许：空字符串（表示清空）
- 拒绝：其他 scheme、缺失 host、缺失端口、非法 URL

实现上应尽量复用当前 `handlers` 包内已有的 `normalizeProxyURL(...)`，避免“代理管理接口严格、认证文件接口宽松”的再次分叉。

## 6. 兼容策略

### 6.1 历史数据兼容

对于历史上仅把 `proxy_url` 写在认证内容 JSON 里的数据：

- 运行时继续允许从 `content.proxy_url` 回退读取
- 但只在数据库列 `models.Auth.ProxyURL` 为空时才生效

### 6.2 新数据语义

从本次修复后开始：

- 管理面板保存的认证文件代理，以数据库列为准
- 页面展示值和运行时使用值必须一致

## 7. 测试与验收

### 7.1 测试范围

1. watcher 单元测试
   - DB 列有值时优先于 JSON
   - DB 列为空时回退 JSON
   - 两者都空时运行时代理为空
2. auth-files handler 测试
   - `http/https/socks5` 合法
   - 非法 scheme 返回 `400`
   - 空字符串可清空代理
3. provider import 测试
   - 非法 `proxy_url` 被拒绝
   - 合法 `proxy_url` 正常入库
4. 热更新回归测试
   - 更新数据库列 `proxy_url` 后，watcher 重建的内存 auth 获得新值

### 7.2 验收标准

1. 管理员面板配置的认证文件 `proxy_url` 能进入运行时 `auth.ProxyURL`
2. 专属代理错误时，请求命中该 auth 必须失败，而不是静默回退到全局代理
3. 清空专属代理后，才允许重新回退到全局 `proxy-url`
4. 历史只写 JSON 的认证数据，在 DB 列为空时仍可继续工作

## 8. 非目标

- 不做代理池/批量绑定代理机制调整
- 不改 executor 的代理优先级定义
- 不做历史数据迁移脚本
- 不修改管理面板交互文案或新增 UI 说明
