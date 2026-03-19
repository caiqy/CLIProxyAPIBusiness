# 管理员最近交易提供方回退逻辑恢复 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 恢复管理员“最近交易”里 provider 的显示优先级为 `auths.name -> auths.key -> provider_api_keys.name -> usage.provider`。

**Architecture:** 保持现有 RecentTransactions 查询结构不变，只调整 provider 显示名解析链路。先用回归测试锁定缺失行为，再最小化修改 `dashboard.go`，避免影响现有 `AuthIndex/AuthKey` 匹配与优先级逻辑。

**Tech Stack:** Go, Gin, GORM, Go test

---

## 文件结构

- 修改：`internal/http/api/admin/handlers/dashboard.go`
  - 调整 auth 标签加载逻辑，让 Auth 记录在 `name` 为空时回退到 `key`
  - 保留现有 provider api key 名称解析与最终兜底逻辑
- 新增：`internal/http/api/admin/handlers/dashboard_provider_display_test.go`
  - 覆盖管理员最近交易 provider 显示优先级的回归测试

## Chunk 1: 测试与最小修复

### Task 1: 先写回归测试锁定优先级

**Files:**
- Create: `internal/http/api/admin/handlers/dashboard_provider_display_test.go`
- Modify: `internal/http/api/admin/handlers/dashboard.go`

- [ ] **Step 1: 写失败测试**
  - 覆盖 `auth.name` 非空时优先显示 `auth.name`
  - 覆盖 `auth.name` 为空时回退显示 `auth.key`
  - 覆盖无可用 auth 显示名时回退 `provider_api_keys.name`
  - 覆盖前三者都没有时回退 `usage.provider`

- [ ] **Step 2: 运行单测并确认失败**
  - Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionsProviderDisplay -count=1`
  - Expected: FAIL，失败原因是当前逻辑不会在 `auth.name` 为空时回退 `auth.key`

- [ ] **Step 3: 写最小实现**
  - 在 `dashboard.go` 中把 Auth 标签查询从只取 `name` 改为同时取 `name`、`key`
  - 生成标签时使用 `strings.TrimSpace(name)`，为空则回退 `strings.TrimSpace(key)`
  - 保持 `providerCredentialName` 的整体优先级不变，让它继续按 `authLabel -> providerKeyLabel -> provider` 工作

- [ ] **Step 4: 重新运行单测并确认通过**
  - Run: `go test ./internal/http/api/admin/handlers -run TestAdminDashboardTransactionsProviderDisplay -count=1`
  - Expected: PASS

- [ ] **Step 5: 运行相关处理器测试确认无回归**
  - Run: `go test ./internal/http/api/admin/handlers -count=1`
  - Expected: PASS
