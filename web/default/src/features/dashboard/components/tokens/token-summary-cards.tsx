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
*/
import { useQuery } from '@tanstack/react-query'
import { Key, DollarSign, BarChart3, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Skeleton } from '@/components/ui/skeleton'
import { getTokenStatsSummary } from '@/features/dashboard/api'

function computeDelta(today: number, yesterday: number): {
  label: string
  positive: boolean | null
} {
  if (yesterday === 0) {
    if (today === 0) return { label: '0%', positive: null }
    return { label: '+100%', positive: true }
  }
  const pct = ((today - yesterday) / yesterday) * 100
  const sign = pct > 0 ? '+' : ''
  const positive = pct === 0 ? null : pct > 0
  return { label: `${sign}${pct.toFixed(1)}%`, positive }
}

export function TokenSummaryCards() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['token-stats-summary'],
    queryFn: getTokenStatsSummary,
    refetchOnWindowFocus: false,
  })

  const summary = data?.data
  const delta =
    summary != null
      ? computeDelta(summary.today_quota, summary.yesterday_quota)
      : null

  const items = [
    {
      title: t('Enabled Tokens'),
      icon: Key,
      value: summary != null ? String(summary.enabled_count) : '--',
      desc: null as string | null,
      deltaPositive: null as boolean | null,
    },
    {
      title: t("Today's Quota"),
      icon: DollarSign,
      value: summary != null ? formatQuota(summary.today_quota) : '--',
      desc: delta ? `${t('vs Yesterday')} ${delta.label}` : null,
      deltaPositive: delta?.positive ?? null,
    },
    {
      title: t('Cumulative Quota'),
      icon: BarChart3,
      value: summary != null ? formatQuota(summary.last30_quota) : '--',
      desc: t('Statistics Range: Last 30 Days'),
      deltaPositive: null,
    },
    {
      title: t('Remaining Quota'),
      icon: Wallet,
      value: summary != null ? formatQuota(summary.remain_total) : '--',
      desc: null,
      deltaPositive: null,
    },
  ]

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid grid-cols-2 divide-x lg:grid-cols-4'>
        {items.map((it) => {
          const Icon = it.icon
          return (
            <div key={it.title} className='px-3 py-2.5 sm:px-5 sm:py-4'>
              <div className='flex items-center gap-2'>
                <Icon className='text-muted-foreground/60 size-3.5 shrink-0' />
                <div className='text-muted-foreground truncate text-xs font-medium tracking-wider'>
                  {it.title}
                </div>
              </div>
              {isLoading ? (
                <div className='mt-2 space-y-1.5'>
                  <Skeleton className='h-7 w-20' />
                  <Skeleton className='h-3.5 w-28' />
                </div>
              ) : (
                <>
                  <div className='text-foreground mt-1.5 font-mono text-lg font-bold tracking-tight tabular-nums sm:mt-2 sm:text-2xl'>
                    {it.value}
                  </div>
                  {it.desc && (
                    <div
                      className={`mt-1 hidden text-xs md:block ${
                        it.deltaPositive === true
                          ? 'text-emerald-600 dark:text-emerald-400'
                          : it.deltaPositive === false
                            ? 'text-rose-600 dark:text-rose-400'
                            : 'text-muted-foreground/60'
                      }`}
                    >
                      {it.desc}
                    </div>
                  )}
                </>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
