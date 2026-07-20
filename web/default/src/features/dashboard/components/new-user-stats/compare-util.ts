/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import type { CompareWindow, StatsFilter } from './types'

const DAY_MS = 86_400_000

function fmtDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function parseDate(s: string): Date | null {
  if (!s) return null
  const t = new Date(`${s}T00:00:00`)
  return Number.isNaN(t.getTime()) ? null : t
}

/**
 * 在 YYYY-MM-DD 上加/减 delta 天。空串或非法日期原样返回。
 */
export function addDays(dateStr: string, delta: number): string {
  const d = parseDate(dateStr)
  if (!d) return dateStr
  d.setDate(d.getDate() + delta)
  return fmtDate(d)
}

/**
 * [start, end] 闭区间天数（含两端）。任一端非法返回 1（保底给"单日"处理）。
 */
export function daysBetween(start: string, end: string): number {
  const s = parseDate(start)
  const e = parseDate(end)
  if (!s || !e) return 1
  const n = Math.round((e.getTime() - s.getTime()) / DAY_MS) + 1
  return n > 0 ? n : 1
}

/**
 * 根据当前筛选区时间，推算对比时间窗口。
 *
 * 规则：
 *  - 今天 (start == end == today) → 昨天
 *  - 其他范围 [start, end]，长度 = end - start + 1（天）
 *    对比窗 = [start - len, start - 1]
 *
 * 返回 null 表示无法推算（start/end 缺失或非法）。
 */
export function computeCompareWindow(filter: StatsFilter): CompareWindow | null {
  const s = parseDate(filter.start_date ?? '')
  const e = parseDate(filter.end_date ?? '')
  if (!s || !e) return null
  const lenDays = Math.round((e.getTime() - s.getTime()) / DAY_MS) + 1
  if (lenDays <= 0) return null
  const compareEnd = new Date(s.getTime() - DAY_MS)
  const compareStart = new Date(compareEnd.getTime() - (lenDays - 1) * DAY_MS)
  return {
    start_date: fmtDate(compareStart),
    end_date: fmtDate(compareEnd),
  }
}

/**
 * 顶部筛选是否对应"今天"（用来决定 compare-bar 是否显示小时段输入）。
 */
export function isTodayMode(filter: StatsFilter): boolean {
  const today = fmtDate(new Date())
  return filter.start_date === today && filter.end_date === today
}
