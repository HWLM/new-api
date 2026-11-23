-- 2026-11-23 群组定价镜像键清理（配合 fix/group-ratio-mirror）
--
-- 背景
--   `group_ratio_setting.group_ratio` / `group_ratio_setting.group_group_ratio`
--   两个命名空间键是 `GroupRatio` / `GroupGroupRatio` 顶层键的重复注册：同一份
--   内存表被 `config.GlobalConfig.SaveToDB` 从内存快照序列化写库，而旧键由
--   分组定价页写入。加载时 `AllOption()` 没有 ORDER BY，镜像行只要排在旧键之后
--   加载就会把内存表整体换回旧快照（RWMap.UnmarshalJSON 是全量替换），表现为
--   新加分组在 60s 同步后被抹掉。
--
--   代码侧已删除这两个镜像字段（fix/group-ratio-mirror），残留行在加载时因
--   struct 里没有对应字段而被忽略，彻底失效。本脚本只做数据清理，避免
--   `GET /api/option/` 里留死键、以及被人工误读。
--
-- 执行顺序（在代码版本上线后执行）
--   ① 只读 diff —— 人工确认，不自动合并
--   ② 删除两行 —— 幂等
--   ③ 复核 —— 0 行
--
-- 目标库：生产 PostgreSQL（RDS）。只动 options 表，不碰其它表。

-- ① 只读 diff：镜像里有、旧键里没有的分组
--    预期：空集（镜像永远只是旧键某个时刻的子集）。
--    若非空，说明这些分组在镜像快照之后被删掉了，**不要合并**——
--    合并会把用户已删除的分组复活。请人工确认后再执行 ②。
WITH legacy AS (
    SELECT jsonb_object_keys((SELECT value::jsonb FROM options WHERE key = 'GroupRatio')) AS g
),
mirror AS (
    SELECT jsonb_object_keys((SELECT value::jsonb FROM options WHERE key = 'group_ratio_setting.group_ratio')) AS g
)
SELECT m.g AS only_in_mirror
FROM mirror m
LEFT JOIN legacy l ON l.g = m.g
WHERE l.g IS NULL
ORDER BY 1;

-- ② 幂等删除
DELETE FROM options
WHERE key IN ('group_ratio_setting.group_ratio', 'group_ratio_setting.group_group_ratio');

-- ③ 复核：应为 0
SELECT count(*) AS remaining
FROM options
WHERE key IN ('group_ratio_setting.group_ratio', 'group_ratio_setting.group_group_ratio');
