# 全局 TLS 证书校验裸关设计（仅上游 API 请求）

## 1. 目标与边界

### 1.1 目标

新增一个**全局配置开关**，允许在代理证书不被系统信任时，对上游 API 请求关闭 TLS 证书校验：

- 配置入口：仅 `config.yaml`
- 默认值：关闭（`false`）
- 生效范围：仅上游 API 请求链路

### 1.2 已确认边界

- 不做管理面板开关
- 不做按 provider/按 auth 的细粒度开关
- 不改变现有 `proxy-url` / `proxy_url` 优先级（仍是 auth 覆盖全局）

## 2. 方案对比

### 方案 A（采纳）

在 `SDKConfig` 新增全局布尔开关，并在上游 HTTP 客户端构造入口统一注入 `InsecureSkipVerify`。

**优点**
- 改动集中，语义清晰
- 能覆盖配额轮询与 Copilot 配额查询
- 与现有配置体系一致（`config.yaml` 单入口）

**代价**
- 需同时覆盖 executor 客户端路径与 `util.SetProxy` 路径

### 方案 B（不采纳）

仅改 `newProxyAwareHTTPClient`。

**问题**
- 可能漏掉 `util.SetProxy` 直连路径，不满足“全局裸关”

### 方案 C（不采纳）

仅通过环境变量控制。

**问题**
- 与“仅 config.yaml 配置”要求不一致，运维可见性弱

## 3. 架构设计

### 3.1 配置模型

在 `third_party/CLIProxyAPIPlus/internal/config/sdk_config.go` 的 `SDKConfig` 增加：

- `TLSInsecureSkipVerify bool `yaml:"tls-insecure-skip-verify" json:"tls-insecure-skip-verify"``

并在示例配置中增加注释：仅用于受控网络/调试，不建议生产启用。

### 3.2 生效链路（核心）

在上游请求 transport 构造中统一注入：

- `transport.TLSClientConfig.InsecureSkipVerify = cfg.TLSInsecureSkipVerify`

覆盖以下主链路：

1. `newProxyAwareHTTPClient`（executor 主入口）
2. `buildDefaultTransportWithTimeouts`
3. `buildProxyTransport`（HTTP/HTTPS/SOCKS5）
4. `util.SetProxy`（非 executor 但依赖 SDKConfig 的请求路径）

### 3.3 覆盖配额查询的依据

当前配额刷新会经由：

- `internal/quota/poller.go -> doRequest -> executor.HttpRequest -> newProxyAwareHTTPClient`

其中 `github-copilot` executor 的 `HttpRequest` 直接调用 `newProxyAwareHTTPClient`，因此该方案可覆盖 Copilot 配额相关上游调用。

## 4. 安全与可观测性

### 4.1 安全默认

- 开关默认 `false`
- 仅显式开启时才裸关

### 4.2 启动告警日志

当开关为 `true` 时输出 WARN 日志，明确提示已关闭上游 TLS 证书校验。

## 5. 测试与验收

### 5.1 测试范围

1. 配置解析测试：字段可正确读取 true/false
2. transport 单元测试：
   - 默认 transport 路径
   - 代理 transport 路径（http/https/socks5）
3. 回归测试：不影响现有 quota/manual refresh 行为

### 5.2 验收标准

1. `config.yaml` 设置 `tls-insecure-skip-verify: true` 后，上游证书校验被关闭
2. 设置为 `false` 或不配置时，保持严格校验
3. 代理场景下（企业 MITM）可成功请求上游
4. 现有配额查询与管理任务流程无回归

## 6. 非目标

- 不引入 UI 配置项
- 不实现按 provider/auth 级别的证书校验策略
- 不改变现有代理选择优先级
