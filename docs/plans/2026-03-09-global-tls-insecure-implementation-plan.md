# 全局 TLS 裸关（仅上游 API）Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新增 `config.yaml` 全局开关 `tls-insecure-skip-verify`（默认关闭），在仅上游 API 请求链路中可关闭 TLS 证书校验，并覆盖配额查询链路（含 Copilot 配额请求）。

**Architecture:** 在 `SDKConfig` 增加全局布尔开关，统一在上游 HTTP transport 构造点注入 `TLSClientConfig.InsecureSkipVerify`。主覆盖点为 `newProxyAwareHTTPClient`（executor 链路）与 `util.SetProxy`（非 executor 但使用 SDKConfig 的请求链路）。同时在 cpab 启动时增加安全告警日志，并更新配置示例。

**Tech Stack:** Go, net/http, tls.Config, YAML config, Go testing (`go test`).

---

### Task 1: 先写失败测试（transport 与 util）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go`
- Create: `third_party/CLIProxyAPIPlus/internal/util/proxy_test.go`
- Create: `third_party/CLIProxyAPIPlus/internal/config/sdk_config_tls_test.go`

**Step 1: 在 proxy_helpers_test.go 增加失败测试（默认 transport）**

新增测试（先失败）：

```go
func TestBuildDefaultTransportWithTimeouts_SetsInsecureSkipVerify(t *testing.T) {
    tr := buildDefaultTransportWithTimeouts(10, 30, true)
    if tr == nil || tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
        t.Fatalf("expected InsecureSkipVerify=true")
    }
}
```

**Step 2: 在 proxy_helpers_test.go 增加失败测试（代理 transport）**

```go
func TestBuildProxyTransport_HTTPProxy_SetsInsecureSkipVerify(t *testing.T) {
    tr := buildProxyTransport("http://proxy.example.com:8080", 10, 30, true)
    if tr == nil || tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
        t.Fatalf("expected proxy transport InsecureSkipVerify=true")
    }
}
```

**Step 3: 创建 util/proxy_test.go，验证 SetProxy 支持开关**

```go
func TestSetProxy_AppliesInsecureSkipVerify(t *testing.T) {
    cfg := &config.SDKConfig{ProxyURL: "http://127.0.0.1:7890", TLSInsecureSkipVerify: true}
    c := SetProxy(cfg, &http.Client{})
    tr, _ := c.Transport.(*http.Transport)
    if tr == nil || tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
        t.Fatalf("expected InsecureSkipVerify=true")
    }
}
```

**Step 4: 创建 sdk_config_tls_test.go，验证 YAML 解析字段**

```go
func TestSDKConfig_TLSInsecureSkipVerifyYAML(t *testing.T) {
    raw := []byte("tls-insecure-skip-verify: true\n")
    var cfg SDKConfig
    if err := yaml.Unmarshal(raw, &cfg); err != nil { t.Fatal(err) }
    if !cfg.TLSInsecureSkipVerify { t.Fatal("expected true") }
}
```

**Step 5: 运行测试并确认失败**

Run: `go test ./internal/runtime/executor -run "InsecureSkipVerify|BuildProxyTransport" -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: FAIL（函数签名/行为尚未支持）。

Run: `go test ./internal/util -run SetProxy_AppliesInsecureSkipVerify -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: FAIL。

Run: `go test ./internal/config -run SDKConfig_TLSInsecureSkipVerifyYAML -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: FAIL（字段未定义）。

**Step 6: Commit（失败测试先行）**

```bash
git add third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go third_party/CLIProxyAPIPlus/internal/util/proxy_test.go third_party/CLIProxyAPIPlus/internal/config/sdk_config_tls_test.go
git commit -m "test(tls): add failing tests for global insecure skip verify switch"
```

---

### Task 2: 实现 SDKConfig 与 executor 主链路支持

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/config/sdk_config.go`
- Modify: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers.go`
- Test: `third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go`
- Test: `third_party/CLIProxyAPIPlus/internal/config/sdk_config_tls_test.go`

**Step 1: 在 SDKConfig 增加全局开关字段**

```go
TLSInsecureSkipVerify bool `yaml:"tls-insecure-skip-verify" json:"tls-insecure-skip-verify"`
```

**Step 2: 给 transport 构造函数增加参数并注入 TLS 配置**

在 `proxy_helpers.go` 修改函数签名并实现：

```go
func buildDefaultTransportWithTimeouts(connectTimeoutSec, responseHeaderTimeoutSec int, insecureSkipVerify bool) *http.Transport
func buildProxyTransport(proxyURL string, connectTimeoutSec, responseHeaderTimeoutSec int, insecureSkipVerify bool) *http.Transport
```

在函数内做最小注入：

```go
if transport.TLSClientConfig == nil {
    transport.TLSClientConfig = &tls.Config{}
} else {
    transport.TLSClientConfig = transport.TLSClientConfig.Clone()
}
transport.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify
```

**Step 3: 在 newProxyAwareHTTPClient 透传配置值**

```go
insecureSkipVerify := cfg != nil && cfg.TLSInsecureSkipVerify
transport := buildProxyTransport(proxyURL, connectTimeout, responseHeaderTimeout, insecureSkipVerify)
// 或
transport := buildDefaultTransportWithTimeouts(connectTimeout, responseHeaderTimeout, insecureSkipVerify)
```

**Step 4: 运行对应测试确认转绿**

Run: `go test ./internal/config -run SDKConfig_TLSInsecureSkipVerifyYAML -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

Run: `go test ./internal/runtime/executor -run "InsecureSkipVerify|BuildProxyTransport" -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 5: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/config/sdk_config.go third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers.go third_party/CLIProxyAPIPlus/internal/runtime/executor/proxy_helpers_test.go third_party/CLIProxyAPIPlus/internal/config/sdk_config_tls_test.go
git commit -m "feat(tls): add global tls-insecure-skip-verify to executor transports"
```

---

### Task 3: 实现 util.SetProxy 链路支持（补覆盖）

**Files:**
- Modify: `third_party/CLIProxyAPIPlus/internal/util/proxy.go`
- Test: `third_party/CLIProxyAPIPlus/internal/util/proxy_test.go`

**Step 1: 在 SetProxy transport 路径注入 TLS 配置**

在 HTTP/HTTPS 与 SOCKS5 分支创建 transport 后统一设置：

```go
if transport.TLSClientConfig == nil {
    transport.TLSClientConfig = &tls.Config{}
} else {
    transport.TLSClientConfig = transport.TLSClientConfig.Clone()
}
transport.TLSClientConfig.InsecureSkipVerify = cfg != nil && cfg.TLSInsecureSkipVerify
```

**Step 2: 跑 util 相关测试**

Run: `go test ./internal/util -run SetProxy -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 3: 跑 executor 关键回归测试**

Run: `go test ./internal/runtime/executor -run "BuildProxyTransport|BuildDefaultTransportWithTimeouts|IsTimeoutError" -v`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 4: Commit**

```bash
git add third_party/CLIProxyAPIPlus/internal/util/proxy.go third_party/CLIProxyAPIPlus/internal/util/proxy_test.go
git commit -m "feat(tls): apply insecure skip verify in util.SetProxy path"
```

---

### Task 4: 配置示例与安全告警日志

**Files:**
- Modify: `config.example.yaml`
- Modify: `third_party/CLIProxyAPIPlus/config.example.yaml`
- Modify: `internal/app/app.go`

**Step 1: 在两个示例配置加入字段与说明**

新增：

```yaml
tls-insecure-skip-verify: false
```

并补注释：仅受控环境用于排障，不建议生产开启。

**Step 2: 在服务启动时增加 WARN 提示**

在 `internal/app/app.go`（加载 config 完成后、服务启动前）增加：

```go
if cfg.TLSInsecureSkipVerify {
    log.Warn("tls-insecure-skip-verify is enabled: upstream TLS certificate verification is disabled")
}
```

**Step 3: 运行 cpab 相关测试**

Run: `go test ./internal/app -v`（workdir: 项目根）  
Expected: PASS

**Step 4: Commit**

```bash
git add config.example.yaml third_party/CLIProxyAPIPlus/config.example.yaml internal/app/app.go
git commit -m "chore(config): document tls insecure switch and add startup warning"
```

---

### Task 5: 覆盖配额查询链路回归与全量验证

**Files:**
- Test: `internal/quota/poller_manual_refresh_test.go`
- Test: `internal/http/api/admin/handlers/quotas_manual_refresh_test.go`

**Step 1: 回归运行配额相关测试（确认链路无回归）**

Run: `go test ./internal/quota ./internal/http/api/admin/handlers -run "RefreshAuth|ManualRefresh|Copilot" -v`（workdir: 项目根）  
Expected: PASS

**Step 2: 运行第三方子模块全量测试**

Run: `go test ./...`（workdir: `third_party/CLIProxyAPIPlus`）  
Expected: PASS

**Step 3: 运行主仓库全量测试**

Run: `go test ./...`（workdir: 项目根）  
Expected: PASS

**Step 4: 最终提交（若前序未分批提交）**

```bash
git add -A
git commit -m "feat: support global tls-insecure-skip-verify for upstream requests"
```

**Step 5: 评审与收尾**

- 使用 `@superpowers:requesting-code-review` 做实现终审。
- 使用 `@superpowers:verification-before-completion` 复核“证据先于结论”。
