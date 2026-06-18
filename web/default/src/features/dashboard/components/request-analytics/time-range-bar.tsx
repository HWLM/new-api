/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Checkbox } from '@/components/ui/checkbox'

// ============================
// TimeRange 类型与工具
// ============================

// 请求-响应统计分析"主时段 + 可选对比时段"模型，UI 状态形状。
// 时段语义统一为 [from, to)；结束时分若为 23:59 在转换为 unix 秒时会进位到次日 00:00。
export interface TimeRange {
  date: string        // 主日期 YYYY-MM-DD（限"今天 / 昨天"）
  startHM: string     // HH:MM
  endHM: string       // HH:MM
  compareEnabled: boolean
  compareDate: string // 对比日期 YYYY-MM-DD（限"近4天且不含今天"，即 T-4 ~ T-1）
}

// ----- 日期工具 -----

const pad2 = (n: number) => String(n).padStart(2, '0')

function ymd(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

function addDays(base: Date, days: number): Date {
  const d = new Date(base)
  d.setDate(d.getDate() + days)
  return d
}

// 主日期允许的最早 / 最晚边界：今天 / 昨天
export function mainDateBounds() {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const yesterday = addDays(today, -1)
  return { min: ymd(yesterday), max: ymd(today) }
}

// 对比日期允许的边界：T-4 ~ T-1（不含今天）
export function compareDateBounds() {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  return { min: ymd(addDays(today, -4)), max: ymd(addDays(today, -1)) }
}

// 默认时间范围：主日期 = 今天，主时段 = 00:00 ~ 当前 HH:MM；对比默认开启，对比日期默认昨天
export function defaultTimeRange(): TimeRange {
  const now = new Date()
  return {
    date: ymd(now),
    startHM: '00:00',
    endHM: `${pad2(now.getHours())}:${pad2(now.getMinutes())}`,
    compareEnabled: true,
    compareDate: ymd(addDays(now, -1)),
  }
}

// 把 "YYYY-MM-DD" + "HH:MM" → unix 秒。
// 特例：HH:MM = "23:59" 视作"覆盖到当天结束"，自动进位为次日 00:00。
function toUnix(date: string, hm: string): number {
  const [hStr, mStr] = hm.split(':')
  const [y, mo, d] = date.split('-').map((s) => Number(s))
  const h = Number(hStr)
  const m = Number(mStr)
  // 23:59 视作 24:00（次日 0:00）
  if (h === 23 && m === 59) {
    return Math.floor(new Date(y, (mo ?? 1) - 1, (d ?? 1) + 1, 0, 0, 0).getTime() / 1000)
  }
  return Math.floor(new Date(y, (mo ?? 1) - 1, d ?? 1, h, m, 0).getTime() / 1000)
}

// 把 TimeRange 转换为后端 API 查询参数。
export interface TimeWindowParams {
  from: number
  to: number
  compare_from?: number
  compare_to?: number
}

export function toWindowParams(r: TimeRange): TimeWindowParams {
  const from = toUnix(r.date, r.startHM)
  const to = toUnix(r.date, r.endHM)
  const out: TimeWindowParams = { from, to }
  if (r.compareEnabled) {
    out.compare_from = toUnix(r.compareDate, r.startHM)
    out.compare_to = toUnix(r.compareDate, r.endHM)
  }
  return out
}

// 校验 TimeRange，返回错误信息或 null。
export function validateTimeRange(r: TimeRange): string | null {
  if (!r.date) return 'invalid date'
  if (!r.startHM || !r.endHM) return 'invalid time'
  const f = toUnix(r.date, r.startHM)
  const t = toUnix(r.date, r.endHM)
  if (t <= f) return 'end must be after start'
  return null
}

// ============================
// TimeRangeBar UI 组件
// ============================

export function TimeRangeBar({
  value,
  onChange,
}: {
  value: TimeRange
  onChange: (next: TimeRange) => void
}) {
  const { t } = useTranslation()
  const mainBounds = useMemo(mainDateBounds, [])
  const cmpBounds = useMemo(compareDateBounds, [])

  const updateField = <K extends keyof TimeRange>(key: K, val: TimeRange[K]) => {
    onChange({ ...value, [key]: val })
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {/* 主日期 */}
      <input
        type='date'
        className='h-8 rounded border bg-background px-2 text-sm'
        min={mainBounds.min}
        max={mainBounds.max}
        value={value.date}
        onChange={(e) => updateField('date', e.target.value)}
      />
      {/* 主时段：开始 - 结束 */}
      <input
        type='time'
        className='h-8 rounded border bg-background px-2 text-sm'
        value={value.startHM}
        onChange={(e) => updateField('startHM', e.target.value)}
      />
      <span className='text-sm text-muted-foreground'>-</span>
      <input
        type='time'
        className='h-8 rounded border bg-background px-2 text-sm'
        value={value.endHM}
        onChange={(e) => updateField('endHM', e.target.value)}
      />

      {/* 对比开关 */}
      <label className='ms-2 flex items-center gap-1.5 text-sm'>
        <Checkbox
          checked={value.compareEnabled}
          onCheckedChange={(v) => updateField('compareEnabled', !!v)}
        />
        <span>{t('Enable time comparison')}</span>
      </label>

      {/* 对比日期 */}
      <input
        type='date'
        className='h-8 rounded border bg-background px-2 text-sm disabled:opacity-50'
        min={cmpBounds.min}
        max={cmpBounds.max}
        value={value.compareDate}
        disabled={!value.compareEnabled}
        onChange={(e) => updateField('compareDate', e.target.value)}
      />
    </div>
  )
}
