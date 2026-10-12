/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/

// 顶部统计区的时间范围：原型给的四个快捷档 + 自定义。
// 与 FilterBar 组件分开放，让组件文件只导出组件（Fast Refresh 要求）。

export type CreditQuickRange = 'today' | '3d' | '7d' | '1m' | 'custom'

export type CreditDateRange = {
  start_date: string
  end_date: string
}

function formatDateLocal(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// 快捷档换算成起止日期，区间都含今天。
export function rangeToDates(
  range: Exclude<CreditQuickRange, 'custom'>
): CreditDateRange {
  const now = new Date()
  const start = new Date(now)
  switch (range) {
    case 'today':
      break
    case '3d':
      start.setDate(now.getDate() - 2)
      break
    case '7d':
      start.setDate(now.getDate() - 6)
      break
    case '1m':
      // 近一个月：往前一个月再 +1 天，让区间长度正好是一个自然月
      start.setMonth(now.getMonth() - 1)
      start.setDate(start.getDate() + 1)
      break
  }
  return { start_date: formatDateLocal(start), end_date: formatDateLocal(now) }
}
