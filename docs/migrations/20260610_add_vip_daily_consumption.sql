-- =============================================================================
-- Migration: add `vip_daily_consumption` table
-- Date:      2026-06-10
-- Feature:   重点客户每日消耗统计 (用于 8 点 TG 播报"近 7 天累计消耗"和消耗明细页)
-- =============================================================================
--
-- 背景:
--   - 每天本地时间凌晨 2 点，定时任务聚合"昨天" 的 vip 客户消耗 (按调用时的当前
--     vip 客户为口径) 并 UPSERT 进本表.
--   - 消耗明细页查询本表 + 实时聚合 logs 表得到今天数据.
--
-- 影响:
--   - 新增一张表 vip_daily_consumption.
--   - 每天产生 N 条记录 (N = 当前 vip 客户数), 数据量极小.
--   - UNIQUE(user_id, stat_date) 保证幂等 UPSERT.
--
-- 兼容性:
--   - GORM AutoMigrate 会在 SQLite / 本地启动时自动建表; 生产 PG 请手动执行
--     下面对应版本的脚本.
--
-- 回滚: 见文件末尾.
-- =============================================================================


-- -----------------------------------------------------------------------------
-- PostgreSQL (>= 9.6)
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS vip_daily_consumption (
    id         SERIAL PRIMARY KEY,
    user_id    INTEGER     NOT NULL,
    username   VARCHAR(64) NOT NULL DEFAULT '',
    stat_date  VARCHAR(10) NOT NULL,        -- 'YYYY-MM-DD'
    quota      BIGINT      NOT NULL DEFAULT 0,
    created_at BIGINT      NOT NULL
);

-- 唯一索引: 同一个客户同一天只有一条; 也用于 UPSERT 冲突检测
CREATE UNIQUE INDEX IF NOT EXISTS uk_vip_daily_user_date
    ON vip_daily_consumption(user_id, stat_date);

-- 单列索引: 按日期范围扫 (近 7 天明细) 时走这个
CREATE INDEX IF NOT EXISTS idx_vip_daily_stat_date
    ON vip_daily_consumption(stat_date);

-- 单列索引: 按 user_id 查某个客户的历史
CREATE INDEX IF NOT EXISTS idx_vip_daily_user_id
    ON vip_daily_consumption(user_id);


-- -----------------------------------------------------------------------------
-- MySQL (>= 5.7.8) — 仅在同时部署 MySQL 时执行
-- -----------------------------------------------------------------------------
--
-- CREATE TABLE IF NOT EXISTS vip_daily_consumption (
--     id         INT AUTO_INCREMENT PRIMARY KEY,
--     user_id    INT          NOT NULL,
--     username   VARCHAR(64)  NOT NULL DEFAULT '',
--     stat_date  VARCHAR(10)  NOT NULL,
--     quota      BIGINT       NOT NULL DEFAULT 0,
--     created_at BIGINT       NOT NULL,
--     UNIQUE KEY uk_vip_daily_user_date (user_id, stat_date),
--     KEY idx_vip_daily_stat_date (stat_date),
--     KEY idx_vip_daily_user_id (user_id)
-- ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


-- =============================================================================
-- 回滚脚本 (出问题时执行)
-- =============================================================================
--
-- PostgreSQL:
--   DROP TABLE IF EXISTS vip_daily_consumption;
--
-- MySQL:
--   DROP TABLE IF EXISTS vip_daily_consumption;
