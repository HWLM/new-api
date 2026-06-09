-- =============================================================================
-- Migration: add `is_vip_customer` column to `users` table
-- Date:      2026-06-09
-- Feature:   重点客户标记 (VIP customer flag)
-- =============================================================================
--
-- 背景:
--   用户管理页 (/users) 新增"标记为重点客户 / 移除重点客户 / 重点客户筛选"
--   三个功能, 需要在 users 表持久化该标记.
--
-- 影响:
--   - 新增一列 is_vip_customer (BOOLEAN), 默认 FALSE, 已有用户不受影响.
--   - 新增一个 B-tree 索引, 用于按是否重点客户筛选.
--
-- 兼容性:
--   - 后端 GORM AutoMigrate 在 SQLite 上会自动添加该列, 无需手动执行.
--   - MySQL / PostgreSQL 生产环境请手动执行下面对应版本的脚本.
--
-- 回滚: 见文件末尾.
-- =============================================================================


-- -----------------------------------------------------------------------------
-- PostgreSQL (>= 9.6)
-- -----------------------------------------------------------------------------
-- PG 11+ 的 ADD COLUMN ... DEFAULT FALSE 是 metadata-only 操作, 不会重写表,
-- 秒级完成. CREATE INDEX 是阻塞建索引, 表行数较大时建议改用 CONCURRENTLY 版本.

ALTER TABLE users
    ADD COLUMN is_vip_customer BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_is_vip_customer
    ON users(is_vip_customer);

-- 大表 (百万行+) 推荐改成下面这条, 不阻塞写入 (但不能在事务内执行):
-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_is_vip_customer ON users(is_vip_customer);


-- -----------------------------------------------------------------------------
-- MySQL (>= 5.7.8)
-- -----------------------------------------------------------------------------
-- 如果同时部署 MySQL 环境再执行下面这段. 仅 PG 部署可忽略.
--
-- ALTER TABLE users
--     ADD COLUMN is_vip_customer TINYINT(1) NOT NULL DEFAULT 0;
--
-- CREATE INDEX idx_users_is_vip_customer ON users(is_vip_customer);


-- =============================================================================
-- 回滚脚本 (出问题时执行)
-- =============================================================================
--
-- PostgreSQL:
--   DROP INDEX IF EXISTS idx_users_is_vip_customer;
--   ALTER TABLE users DROP COLUMN IF EXISTS is_vip_customer;
--
-- MySQL:
--   DROP INDEX idx_users_is_vip_customer ON users;
--   ALTER TABLE users DROP COLUMN is_vip_customer;
