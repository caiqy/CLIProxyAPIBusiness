# 配额界面批量手动更新（查询配额）设计

## 背景与目标

当前配额页 `Quotas` 只能展示数据库中已落盘的额度数据，页面上的“刷新”仅重新拉取 `GET /v0/admin/quotas`，不会即时触发上游额度查询。系统虽已具备定时轮询（`internal/quota/poller.go`）能力，但缺少“按页面筛选范围立即查询”的人工触发入口。

本次目标：在配额界面增加按钮 **“查询配额”**，支持对“当前筛选结果”执行异步批量上游查询，并在前端展示进度，完成后自动刷新列表。

## 约束与确认结论

- 触发范围：**当前筛选结果**（`key`/`type`/`auth_group_id`），不按当前分页裁剪。
- 执行模式：**异步触发 + 进度提示**。
- 不支持类型处理：**跳过并计入失败**，不中断整批任务。
- 完成后行为：**自动刷新当前列表**。
- 权限模型：**新增独立权限点**。
- 并发策略：复用 `QUOTA_POLL_MAX_CONCURRENCY`。
- 任务存储：**内存任务表**（单实例，不做持久化）。

## 方案比较与选型

### 方案 A（采用）：后端异步任务 + 前端轮询任务状态

- 新建任务接口与状态接口。
- 后台 goroutine 执行批量刷新。
- 前端轮询显示进度与统计。

优点：体验完整、可追踪、适合大批量；缺点：需要额外任务管理器。

### 方案 B：单接口同步等待

优点：实现简单；缺点：大范围筛选易超时，不符合“异步+进度”诉求。

### 方案 C：仅触发全量即时轮询

优点：改动最小；缺点：无法保证“当前筛选结果”语义与统计精度。

## 架构设计

### 1) 后端组件

1. `QuotaHandler` 扩展：
   - `POST /v0/admin/quotas/manual-refresh`
   - `GET /v0/admin/quotas/manual-refresh/:task_id`
2. `ManualRefreshTaskManager`（新建）：
   - 内存 `map[taskID]*Task`
   - 互斥保护
   - TTL 清理（建议完成后保留 30 分钟）
   - 容量上限（建议 200）
3. `quota` 执行服务（新建或抽取）：
   - 复用现有 provider 查询与 `saveQuota` 逻辑
   - provider 分派：`antigravity` / `codex` / `gemini-cli`
   - 不支持类型按失败计数并继续

### 2) 前端组件（`web/src/pages/admin/Quotas.tsx`）

1. 在工具栏新增按钮：**“查询配额”**。
2. 点击后提交当前筛选条件创建任务。
3. 轮询任务状态（建议 1~2 秒）并展示：
   - `processed/total`
   - `success/failed/skipped`
4. 任务结束后停止轮询，toast 提示结果并调用 `fetchQuotas()` 自动刷新。

## 接口契约

### POST `/v0/admin/quotas/manual-refresh`

请求：

```json
{
  "key": "optional",
  "type": "optional",
  "auth_group_id": 123
}
```

响应：

```json
{
  "task_id": "mr_20260218_xxx",
  "status": "running",
  "total": 42
}
```

说明：若筛选结果为空，返回 `total=0`，任务可直接完成。

### GET `/v0/admin/quotas/manual-refresh/:task_id`

响应：

```json
{
  "task_id": "mr_20260218_xxx",
  "status": "running",
  "total": 42,
  "processed": 18,
  "success_count": 14,
  "failed_count": 4,
  "skipped_count": 0,
  "started_at": "2026-02-18T10:00:00Z",
  "finished_at": null,
  "last_error": "unsupported provider: xxx",
  "recent_errors": ["..."]
}
```

## 数据流

1. 前端基于当前筛选条件发起创建任务。
2. 后端按与列表一致的过滤语义查询目标 auth 集合。
3. 任务进入 `running` 并异步执行批量查询。
4. 每处理一条记录即更新计数与最近错误。
5. 前端轮询状态并渲染进度。
6. 任务完成后前端自动刷新列表展示最新配额。

## 错误处理策略

- 参数非法：400
- 无权限：403
- 任务不存在/过期（TTL 清理）：404
- 上游失败、超时、非 2xx：计失败并记录摘要
- 不支持类型：计失败并记录 `unsupported provider`
- `saveQuota` 失败：计失败并记录
- 执行 panic：recover 后置任务为失败并写 `last_error`

## 权限设计

新增独立权限点：

- `POST /v0/admin/quotas/manual-refresh`（触发任务）
- `GET /v0/admin/quotas/manual-refresh/:task_id`（查询任务）

前端依据权限控制按钮可见与可用状态。

## 测试与验收

### 后端

- 过滤条件解析与目标集合正确性
- 并发执行计数一致性（复用 `QUOTA_POLL_MAX_CONCURRENCY`）
- 不支持类型计失败且不终止整批
- 上游异常、超时、保存失败路径
- TTL 清理与容量上限清理

### 前端

- 无权限不展示按钮
- 创建任务成功后进入轮询
- 进度与统计正确渲染
- 任务完成后自动刷新列表
- 404 任务过期提示与兜底

### 验收标准

1. 点击“查询配额”可针对当前筛选结果触发批量更新。
2. 页面可看到任务执行进度与成功失败统计。
3. 完成后列表自动刷新，显示最新配额。
4. 不支持类型不会阻塞整体任务，并有明确失败统计。

## 非目标（YAGNI）

- 不做任务持久化（DB）
- 不做跨实例任务共享与调度
- 不做实时推送（WebSocket/SSE），先采用轮询
- 不新增手动任务独立并发配置
