/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { addDays, daysBetween } from './compare-util'
import type { CompareWindow } from './types'

type CompareBarProps = {
  /** 当前对比时间窗 —— 默认由顶部筛选推算，可被用户覆盖；null 表示当前筛选无法推算 */
  compareWindow: CompareWindow | null
  /** 用户改动对比日期时回调；长度已由本组件按当前周期守恒 */
  onCompareDateChange: (start_date: string, end_date: string) => void
  /** 顶部是否"今天"模式 —— 仅此时显示小时段输入 */
  isTodayMode: boolean
  /** 小时段值（绑定到外层 state，外层在传给后端时用） */
  startHour: number
  endHour: number
  onHourChange: (start: number, end: number) => void
  /** 对比开关 toggle */
  enabled: boolean
  onToggle: () => void
}

function clampHour(v: number): number {
  if (Number.isNaN(v)) return 0
  if (v < 0) return 0
  if (v > 24) return 24
  return v
}

export function CompareBar({
  compareWindow,
  onCompareDateChange,
  isTodayMode,
  startHour,
  endHour,
  onHourChange,
  enabled,
  onToggle,
}: CompareBarProps) {
  const { t } = useTranslation()

  // 从 compareWindow 反推「周期长度」——图表 X 轴按当前周期 buckets 索引，
  // 因此必须保持 对比周期长度 == 当前周期长度，否则展示会错位（长的一端会被静默丢弃）。
  const lenDays = compareWindow
    ? daysBetween(compareWindow.start_date, compareWindow.end_date)
    : 1
  const singleDay = lenDays === 1

  const [draftStart, setDraftStart] = useState(compareWindow?.start_date ?? '')
  const [draftEnd, setDraftEnd] = useState(compareWindow?.end_date ?? '')

  useEffect(() => {
    setDraftStart(compareWindow?.start_date ?? '')
    setDraftEnd(compareWindow?.end_date ?? '')
  }, [compareWindow?.start_date, compareWindow?.end_date])

  // 单日模式：只有一个输入，start == end
  const commitSingle = (v: string) => {
    setDraftStart(v)
    setDraftEnd(v)
    if (v) onCompareDateChange(v, v)
  }
  // 多日模式：改一端，自动平移另一端保持长度
  const commitStart = (v: string) => {
    setDraftStart(v)
    if (!v) return
    const newEnd = addDays(v, lenDays - 1)
    setDraftEnd(newEnd)
    onCompareDateChange(v, newEnd)
  }
  const commitEnd = (v: string) => {
    setDraftEnd(v)
    if (!v) return
    const newStart = addDays(v, -(lenDays - 1))
    setDraftStart(newStart)
    onCompareDateChange(newStart, v)
  }

  return (
    <div className='flex flex-wrap items-center gap-2 text-sm'>
      <span className='text-muted-foreground'>{t('Compare with')}：</span>

      {/* 对比日期：默认自动推算，可编辑；始终守恒长度与当前周期一致 */}
      {singleDay ? (
        <Input
          type='date'
          value={draftStart}
          onChange={(e) => commitSingle(e.target.value)}
          className='w-36'
        />
      ) : (
        <div className='flex items-center gap-1'>
          <Input
            type='date'
            value={draftStart}
            onChange={(e) => commitStart(e.target.value)}
            className='w-36'
          />
          <span className='text-muted-foreground text-xs'>~</span>
          <Input
            type='date'
            value={draftEnd}
            onChange={(e) => commitEnd(e.target.value)}
            className='w-36'
          />
          <span className='text-muted-foreground text-xs'>
            ({lenDays} {t('days')})
          </span>
        </div>
      )}

      {/* 仅"今天"模式可输入小时段 */}
      {isTodayMode && (
        <div className='flex items-center gap-1'>
          <Input
            type='number'
            min={0}
            max={24}
            value={startHour}
            onChange={(e) =>
              onHourChange(clampHour(Number(e.target.value)), endHour)
            }
            className='w-16'
          />
          <span className='text-muted-foreground text-xs'>-</span>
          <Input
            type='number'
            min={0}
            max={24}
            value={endHour}
            onChange={(e) =>
              onHourChange(startHour, clampHour(Number(e.target.value)))
            }
            className='w-16'
          />
        </div>
      )}

      <Button
        variant={enabled ? 'destructive' : 'default'}
        size='sm'
        onClick={onToggle}
        disabled={compareWindow == null}
      >
        {enabled ? t('Disable Compare') : t('Apply Compare')}
      </Button>
    </div>
  )
}
