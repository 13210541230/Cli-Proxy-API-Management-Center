# 用量分析迁移运行说明

本项目的用量分析迁移采用增量吸收方式：`usage_events` 继续是唯一原始事实源，小时/日级表均为可删除、可重建的读取加速层。

## 升级与兼容

1. 备份 usage SQLite 数据库及同目录的 `-wal`、`-shm` 文件；备份现有配置 JSON/环境变量。
2. 停止旧 usage-service 后替换二进制并启动。启动只会创建派生表和 recovery spool，不删除或重排 `usage_events`。
3. `settings.setup`、`settings.manager_config_v1`、`model_prices`、`api_key_aliases`、企业 Key、配额和告警数据继续由现有代码负责读写。
4. Rollup worker 在后台按 event id 分批追赶；首次追赶期间 Analytics 会使用 raw fallback，旧 `GET /v0/management/usage` 不变。
5. 通过 collector status 查看 `rollup_status`、`coverage_event_id`、`last_error` 和 `pendingItems`。

## 回滚

- 若新版本需要回滚，停止新 usage-service，保留 SQLite 文件，恢复上一版本二进制并启动；旧 API 和原始事件不依赖 Rollup 表。
- 不要删除 `usage_events`，也不要用导入/导出重写原始事件作为回滚手段。
- 新增的 `usage_hourly_rollups`、`usage_daily_dimension_rollups`、`usage_rollup_state`、`collector_pending_items` 都是派生/恢复数据；必要时可在停服备份后删除，下一次启动/worker 会重建或回放。
- 删除派生表不会影响现有企业 Key、配额、告警、邮件、认证和配置数据。

## 恢复与清理边界

- RESP LPOP/RPOP 和 HTTP usage queue 是破坏性取出；collector 会先把批次写入 `collector_pending_items`，成功写入 `usage_events` 后再清理 recovery item。
- `InsertEvents` 或 dead-letter 写入失败时 recovery item 保留，后续轮询/重启会重试；不得手工清空该表，除非已确认对应事件已落库或已进入 dead letter。
- 本地 `usage_events` retention 与 CPA queue retention 独立。purge 会同时使覆盖水位和派生行失效，随后从 raw 事件重建。
- SQLite 运行时应保持单个 usage-service writer；保留 WAL/SHM 文件直至服务正常关闭或完成一致性备份。

## Analytics 读取

- 新接口：`POST /v0/management/monitoring/analytics`，时间范围为 `[from_ms,to_ms)`。
- `summary`、`timeline`、`model_stats`、账号/API Key 统计及 `api_key_timeline` 按 include 返回；`events` 独立使用 `(timestamp_ms,id)` keyset 分页并返回真实 `total_count`。
- Rollup 尚未覆盖、失败或不适用筛选时自动回退 raw SQL；旧 GET 用量接口作为兼容和回滚路径保留。
- 响应 `meta.source`、`meta.complete`、`meta.rollup_status` 和 `coverage_event_id` 用于运维观测，不应把分页明细当成完整统计。
