/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { ArrowDown, ArrowUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { CardSection, UserStatsCards } from './types'

function formatNumber(n: number, fractionDigits: number): string {
  return n.toLocaleString(undefined, {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  })
}

/**
 * DeltaText: 「较昨日 ↑/↓ X%」
 *   delta == null  → 显示 --（凌晨 daily_summary 还没写入 / 昨日基线为 0）
 *   delta > 0      → 绿色 ↑
 *   delta < 0      → 红色 ↓
 *   delta === 0    → 灰色
 */
function DeltaText({
  delta,
  compareLabel,
}: {
  delta: number | null
  compareLabel: string
}) {
  if (delta == null) {
    return (
      <div className='text-muted-foreground mt-1 text-xs'>
        {compareLabel} --
      </div>
    )
  }
  const positive = delta > 0 ? true : delta < 0 ? false : null
  const colorClass =
    positive === true
      ? 'text-emerald-600 dark:text-emerald-400'
      : positive === false
        ? 'text-rose-600 dark:text-rose-400'
        : 'text-muted-foreground'
  return (
    <div className={cn('mt-1 flex items-center gap-1 text-xs', colorClass)}>
      <span className='text-muted-foreground'>{compareLabel}</span>
      {positive === true && <ArrowUp className='h-3 w-3' />}
      {positive === false && <ArrowDown className='h-3 w-3' />}
      <span className='tabular-nums'>{Math.abs(delta).toFixed(1)}%</span>
    </div>
  )
}

type CardItem = {
  label: string
  value: string
  subtitle?: string
  delta?: { compareLabel: string; value: number | null }
}

function CardCell({ item }: { item: CardItem }) {
  return (
    <Card className='gap-1 py-3 sm:py-4'>
      <CardContent className='px-4 sm:px-5'>
        <div className='text-muted-foreground text-sm'>{item.label}</div>
        <div className='mt-1 text-2xl font-semibold tabular-nums'>
          {item.value}
        </div>
        {item.subtitle && (
          <div className='text-muted-foreground mt-1 text-xs'>
            {item.subtitle}
          </div>
        )}
        {item.delta && (
          <DeltaText
            delta={item.delta.value}
            compareLabel={item.delta.compareLabel}
          />
        )}
      </CardContent>
    </Card>
  )
}

function CardSkeletonRow() {
  return (
    <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
      {Array.from({ length: 6 }).map((_, i) => (
        <Card key={i} className='gap-1 py-3 sm:py-4'>
          <CardContent className='px-4 sm:px-5'>
            <Skeleton className='h-4 w-20' />
            <Skeleton className='mt-2 h-7 w-24' />
            <Skeleton className='mt-2 h-3 w-16' />
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function buildRow(
  s: CardSection,
  labels: {
    users: string
    totalRecharge: string
    totalConsumed: string
    todayRecharge: string
    todayConsumed: string
    totalRemaining: string
    todayActiveSuffix: string
    vsYesterday: string
    subLabel: string
    otherLabel: string
  }
): CardItem[] {
  const todaySub = s.today_sub_consumed_usd || 0
  const todayOther = Math.max(s.today_consumed_usd - todaySub, 0)
  return [
    {
      label: labels.users,
      value: formatNumber(s.total_users, 0),
      subtitle: `${labels.todayActiveSuffix}: ${formatNumber(s.today_active_users, 0)}`,
    },
    { label: labels.totalRecharge, value: formatNumber(s.total_recharge_cny, 2) },
    { label: labels.totalConsumed, value: formatNumber(s.total_consumed_usd, 2) },
    {
      label: labels.todayRecharge,
      value: formatNumber(s.today_recharge_cny, 2),
      delta: {
        compareLabel: labels.vsYesterday,
        value: s.today_recharge_cny_delta,
      },
    },
    {
      label: labels.todayConsumed,
      value: formatNumber(s.today_consumed_usd, 2),
      subtitle: `(${labels.subLabel}: ${formatNumber(todaySub, 2)}  ${labels.otherLabel}: ${formatNumber(todayOther, 2)})`,
      delta: {
        compareLabel: labels.vsYesterday,
        value: s.today_consumed_usd_delta,
      },
    },
    {
      label: labels.totalRemaining,
      value: formatNumber(s.total_remaining_usd, 2),
    },
  ]
}

export function SummaryCards({
  data,
  isLoading,
}: {
  data: UserStatsCards | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()

  if (isLoading || !data) {
    return (
      <div className='space-y-3'>
        <CardSkeletonRow />
        <CardSkeletonRow />
      </div>
    )
  }

  const todayActive = t('Today Active Users')
  const vsYesterday = t('vs Yesterday')
  const subLabel = 'sub'
  const otherLabel = t('Other')

  const allRow = buildRow(data.all, {
    users: t('Total Users'),
    totalRecharge: t('Total Recharge (¥)'),
    totalConsumed: t('Total Consumed ($)'),
    todayRecharge: t('Today Recharge (¥)'),
    todayConsumed: t('Today Consumed ($)'),
    totalRemaining: t('Total Remaining ($)'),
    todayActiveSuffix: todayActive,
    vsYesterday,
    subLabel,
    otherLabel,
  })
  const officialRow = buildRow(data.official, {
    users: t('Official Users'),
    totalRecharge: t('Recharge (¥)'),
    totalConsumed: t('Total Consumed ($)'),
    todayRecharge: t('Today Recharge (¥)'),
    todayConsumed: t('Today Consumed ($)'),
    totalRemaining: t('Total Remaining ($)'),
    todayActiveSuffix: todayActive,
    vsYesterday,
    subLabel,
    otherLabel,
  })

  return (
    <div className='space-y-3'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
        {allRow.map((it, i) => (
          <CardCell key={`all-${i}`} item={it} />
        ))}
      </div>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
        {officialRow.map((it, i) => (
          <CardCell key={`off-${i}`} item={it} />
        ))}
      </div>
    </div>
  )
}
