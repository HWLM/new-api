/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * 用户分组速率（docs/rate-limit-fallback-split.md §21.3）。
 *
 * 后端 user.group_rate_limits 是 JSON 字符串：
 *   {"vip":{"limit":20,"fallback":true},"default":{"limit":5,"fallback":false}}
 * limit：>0 自定义并发上限；-1 该分组不限；0/缺省 = 不单独限制（只受总并发约束）。
 * fallback：默认 true（超限走速率兜底渠道）；false = 到顶直接 429 rate_limit_exceeded。
 */
export type GroupRateLimitRow = {
  group: string
  limit: number
  fallback: boolean
}

export function parseGroupRateLimits(
  raw: string | null | undefined
): GroupRateLimitRow[] {
  if (!raw || raw.trim() === '') return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return []
    }
    return Object.entries(parsed as Record<string, unknown>).map(
      ([group, value]) => {
        const v = (value ?? {}) as { limit?: unknown; fallback?: unknown }
        const limit =
          typeof v.limit === 'number' && Number.isInteger(v.limit) ? v.limit : 0
        return { group, limit, fallback: v.fallback !== false }
      }
    )
  } catch {
    return []
  }
}

/** 空表 → 空串（后端视为未配置，行为与改动前一致）。 */
export function serializeGroupRateLimits(rows: GroupRateLimitRow[]): string {
  const out: Record<string, { limit: number; fallback: boolean }> = {}
  for (const row of rows) {
    const group = row.group.trim()
    if (!group) continue
    out[group] = { limit: row.limit, fallback: row.fallback }
  }
  return Object.keys(out).length === 0 ? '' : JSON.stringify(out)
}
