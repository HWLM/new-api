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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatCompactNumber, formatLogQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { useUsageLogAccess } from '../hooks/use-usage-log-access'
import { buildApiParams } from '../lib/utils'
import { useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

export function CommonLogsStats() {
  const { t } = useTranslation()
  const { scope: accessScope, userId, isAdmin } = useUsageLogAccess()
  const searchParams = route.useSearch()
  const {
    sensitiveVisible,
    statsRevealed,
    refreshStats,
    statsRefreshTick,
  } = useUsageLogsContext()

  // 只有用户点击「用量」按钮后才发接口；tick 变化触发刷新。
  // 该接口对 logs 表做 SUM/COUNT 聚合，日志表大时代价高，因此默认不主动调用。
  const { data: stats, isLoading, isFetching } = useQuery({
    queryKey: [
      'usage-logs-stats',
      accessScope,
      userId,
      searchParams,
      statsRefreshTick,
    ],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        accessScope,
      })

      const result = isAdmin
        ? await getLogStats(params)
        : await getUserLogStats(params)

      return result.success
        ? result.data || DEFAULT_LOG_STATS
        : DEFAULT_LOG_STATS
    },
    enabled: statsRevealed,
    placeholderData: (previousData) => previousData,
    // 缓存不再无声地重刷；每次刷新都是用户点击触发的
    staleTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const showLoading = statsRevealed && (isLoading || isFetching) && !stats
  const showValue = statsRevealed && !!stats
  const displayValue = (v: string) => (showValue && sensitiveVisible ? v : '••••')

  const totalQuota = stats?.quota || 0
  const subQuota = stats?.sub_quota || 0
  const otherQuota = Math.max(totalQuota - subQuota, 0)
  const subTokens = stats?.sub_tokens || 0

  const refreshButton = (
    <Button
      variant='outline'
      size='sm'
      className='h-7 gap-1 text-xs'
      onClick={() => refreshStats()}
      disabled={statsRevealed && isFetching}
    >
      <RefreshCw
        className={cn('size-3.5', statsRevealed && isFetching && 'animate-spin')}
      />
      {t('Usage')}
    </Button>
  )

  if (showLoading) {
    return (
      <div className='flex flex-wrap items-center gap-2'>
        {refreshButton}
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
      </div>
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {refreshButton}
      <StatBadge
        label={t('Total Usage')}
        value={displayValue(formatLogQuota(totalQuota))}
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('Sub Usage')}
        value={displayValue(formatLogQuota(subQuota))}
        accent='bg-emerald-500/70'
      />
      <StatBadge
        label={t('Other Usage')}
        value={displayValue(formatLogQuota(otherQuota))}
        accent='bg-amber-500/70'
      />
      <StatBadge
        label={t('Sub Total Tokens')}
        value={displayValue(formatCompactNumber(subTokens))}
        accent='bg-violet-500/70'
      />
      <StatBadge
        label={t('RPM')}
        value={showValue ? stats?.rpm || 0 : '••••'}
        accent='bg-rose-500/65'
      />
      <StatBadge
        label={t('TPM')}
        value={showValue ? stats?.tpm || 0 : '••••'}
        accent='bg-slate-400/70'
      />
    </div>
  )
}
