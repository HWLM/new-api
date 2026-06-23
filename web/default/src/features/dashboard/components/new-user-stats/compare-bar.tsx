/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { CompareWindow } from './types'

type CompareBarProps = {
  /** 推算出来的对比时间窗 —— null 表示当前筛选无法推算 */
  compareWindow: CompareWindow | null
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
  isTodayMode,
  startHour,
  endHour,
  onHourChange,
  enabled,
  onToggle,
}: CompareBarProps) {
  const { t } = useTranslation()

  const dateLabel =
    compareWindow == null
      ? '-'
      : compareWindow.start_date === compareWindow.end_date
        ? compareWindow.start_date
        : `${compareWindow.start_date} ~ ${compareWindow.end_date}`

  return (
    <div className='flex flex-wrap items-center gap-2 text-sm'>
      <span className='text-muted-foreground'>{t('Compare with')}：</span>
      {/* 日期只读展示 */}
      <span className='rounded-md border bg-muted/30 px-3 py-1 tabular-nums'>
        {dateLabel}
      </span>

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
            disabled={enabled}
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
            disabled={enabled}
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
