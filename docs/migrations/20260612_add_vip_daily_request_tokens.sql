-- =============================================================================
-- Migration: add `request_count` and `tokens` columns to `vip_daily_consumption`
-- Date:      2026-06-12
-- Feature:   重点客户统计扩展 — 请求次数 + token 数
-- =============================================================================
--
-- 背景:
--   重点客户统计页面新增 4 张卡片(今日/7天 请求次数 + 今日/7天 token 数)，
--   以及第二张"请求次数/token 数"明细表格. 凌晨 2 点统计任务在原有 quota 之外
--   同时聚合昨日的 COUNT(*) 和 SUM(prompt_tokens + completion_tokens) 写入两列.
--
-- 影响:
--   - 给 vip_daily_consumption 加两个 BIGINT 列, 默认 0.
--   - 历史行的两列默认是 0; 不回填(从下次凌晨 2 点统计开始有真实数据).
--
-- 兼容性:
--   - GORM AutoMigrate 在 SQLite / MySQL 上启动时自动 ADD COLUMN;
--   - PG 请手动执行下面 ALTER TABLE.
--
-- 回滚: 见文件末尾.
-- =============================================================================


-- -----------------------------------------------------------------------------
-- PostgreSQL (>= 9.6)
-- -----------------------------------------------------------------------------

ALTER TABLE vip_daily_consumption
    ADD COLUMN IF NOT EXISTS request_count BIGINT NOT NULL DEFAULT 0;

ALTER TABLE vip_daily_consumption
    ADD COLUMN IF NOT EXISTS tokens BIGINT NOT NULL DEFAULT 0;


-- -----------------------------------------------------------------------------
-- MySQL (>= 5.7.8) — 仅在同时部署 MySQL 时执行
-- -----------------------------------------------------------------------------
-- ALTER TABLE vip_daily_consumption
--     ADD COLUMN request_count BIGINT NOT NULL DEFAULT 0,
--     ADD COLUMN tokens        BIGINT NOT NULL DEFAULT 0;


-- =============================================================================
-- 回滚脚本 (出问题时执行)
-- =============================================================================
--
-- PostgreSQL:
--   ALTER TABLE vip_daily_consumption DROP COLUMN IF EXISTS request_count;
--   ALTER TABLE vip_daily_consumption DROP COLUMN IF EXISTS tokens;
--
-- MySQL:
--   ALTER TABLE vip_daily_consumption DROP COLUMN request_count, DROP COLUMN tokens;
