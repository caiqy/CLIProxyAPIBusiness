# 设计文档：合并上游 CLIProxyAPIPlus 冲突解决 (2026-03-02)

## 1. 背景与目标
为了同步上游 `CLIProxyAPIPlus` 的最新改进（包括 `PopulateAuthContext` 架构优化、安全性增强以及新模型定义），同时保留本地开发的“动态回调主机名 (callback_host)”特性，需要对合并过程中的冲突进行精细化处理。

## 2. 详细设计

### 2.1. OAuth 认证逻辑整合 (`internal/api/handlers/management/auth_files.go`)
*   **本地特性保留**：继续使用 `callbackHostFromRequest(c)` 从 Query 参数获取回调地址。
*   **上游架构集成**：调用 `PopulateAuthContext(ctx, c)`。此函数提取请求头和查询参数存入 Context，确保后续认证插件（如 `kiro` 或 `claude`）能获取到必要的环境信息。
*   **逻辑合并示例**：
    ```go
    ctx := context.Background()
    // 1. 本地动态回调逻辑
    callbackHost := callbackHostFromRequest(c)
    redirectURI := buildLoopbackRedirectURI(callbackHost, port, "/callback")
    // 2. 上游 Context 注入逻辑
    ctx = PopulateAuthContext(ctx, c)
    // 3. 调用认证器
    bundle, err := auth.ExchangeCodeForTokensWithRedirect(ctx, code, redirectURI, pkce)
    ```

### 2.2. Codex 认证签名统一 (`internal/auth/codex/openai_auth.go`)
*   **签名调整**：将 `ExchangeCodeForTokensWithRedirect` 签名修改为 `(ctx, code, redirectURI, pkceCodes)`，与上游保持一致。
*   **调用方更新**：同步修改 `auth_files.go` 中的调用代码，调整参数顺序。

### 2.3. 静态数据与模型定义 (`internal/registry/model_definitions_static_data.go`)
*   **合并策略**：采用上游最新的 `model_definitions_static_data.go`。如果本地存在特定的模型映射覆盖（Overrides），将在合并后手动补回。

### 2.4. 测试用例适配 (`server_test.go`, `aws_test.go`)
*   **Mock 对象更新**：根据上游对认证流程的重构，更新测试代码中的 Mock 返回值和断言逻辑，确保 CI 能够通过。

### 2.5. OpenAI 思考模型适配 (`internal/thinking/provider/openai/apply.go`)
*   **重构合并**：合并上游对 Thinking/Reasoning 逻辑的重构代码，确保与最新的 OpenAI API 规范兼容。

## 3. 验证计划
1.  **代码编译**：确保 `go build ./...` 无误。
2.  **单元测试**：运行相关测试用例：
    *   `go test ./internal/api/handlers/management/...`
    *   `go test ./internal/auth/codex/...`
3.  **功能验证**：通过管理后台尝试一次完整的 OAuth 登录流程，验证 `callback_host` 是否依然生效。

## 4. 结论
通过以上合并策略，我们可以在不破坏本地定制功能的前提下，平滑升级到上游的最新版本。
