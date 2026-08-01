/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getPromotionStats } from '@/features/dashboard/api'
import type {
  ChannelPromotionRow,
  SalesPromotionRow,
} from '@/features/dashboard/types'
import type { CompareWindow } from './types'

type TimeRange = { start_timestamp: number; end_timestamp: number }

type PromotionTablesCompareProps = {
  /** 当前周期时间范围（unix 秒，本地时区当日 00:00 ~ 23:59:59） */
  timeRange: TimeRange
  /** 对比周期；不传 = 不显示环比列 */
  compareWindow: CompareWindow | null
  topN?: number
}

// 计算环比百分比；prev=0 时返回 null，前端显示 "-"
function deltaPct(curr: number, prev: number): number | null {
  if (prev === 0) return null
  return ((curr - prev) / prev) * 100
}

function DeltaPctText({ value }: { value: number | null }) {
  const { t } = useTranslation()
  if (value === null) {
    return <span className='text-muted-foreground text-xs'>--</span>
  }
  const isUp = value > 0
  const isDown = value < 0
  const color = isUp
    ? 'text-emerald-600 dark:text-emerald-400'
    : isDown
      ? 'text-rose-600 dark:text-rose-400'
      : 'text-muted-foreground'
  const arrow = isUp ? '↑' : isDown ? '↓' : ''
  return (
    <span className={cn('ml-2 text-xs tabular-nums', color)}>
      {t('vs Previous Period')} {arrow}
      {Math.abs(value).toFixed(0)}%
    </span>
  )
}

// 把 compareWindow（YYYY-MM-DD 字符串）转 unix 秒（本地时区 00:00 ~ 23:59:59）
function compareToTimeRange(cw: CompareWindow): TimeRange {
  const startMs = new Date(`${cw.start_date}T00:00:00`).getTime()
  const endMs = new Date(`${cw.end_date}T23:59:59`).getTime()
  return {
    start_timestamp: Math.floor(startMs / 1000),
    end_timestamp: Math.floor(endMs / 1000),
  }
}

const quotaToUsd = (q: number) => quotaUnitsToDollars(q)

export function PromotionTablesCompare(props: PromotionTablesCompareProps) {
  const { t } = useTranslation()
  const compareEnabled = !!props.compareWindow

  // 当前周期数据
  const currentQuery = useQuery({
    queryKey: ['user-stats', 'promotion-current', props.timeRange],
    queryFn: () => getPromotionStats(props.timeRange),
    select: (res) => (res.success ? res.data : undefined),
    staleTime: 60_000,
  })

  // 对比周期数据（仅当开启对比时拉）
  const compareTimeRange = props.compareWindow
    ? compareToTimeRange(props.compareWindow)
    : null
  const compareQuery = useQuery({
    queryKey: ['user-stats', 'promotion-compare', compareTimeRange],
    queryFn: () =>
      compareTimeRange ? getPromotionStats(compareTimeRange) : null,
    select: (res) =>
      res && (res as any).success ? (res as any).data : undefined,
    enabled: compareEnabled && compareTimeRange != null,
    staleTime: 60_000,
  })

  const topN = props.topN ?? 10
  const isLoading = currentQuery.isLoading || (compareEnabled && compareQuery.isLoading)

  // 渠道：用 current 的列表作为 key，去 compare 里 lookup
  const channels = useMemo(() => {
    const rows = (currentQuery.data?.channels ?? []).filter(
      (c) => c.channel && c.channel.length > 0
    )
    return rows.slice(0, topN)
  }, [currentQuery.data, topN])

  const compareChannelMap = useMemo(() => {
    const m = new Map<string, ChannelPromotionRow>()
    if (compareQuery.data?.channels) {
      for (const r of compareQuery.data.channels) {
        m.set(r.channel, r)
      }
    }
    return m
  }, [compareQuery.data])

  // 销售：用 username 做 key
  const sales = useMemo(
    () => (currentQuery.data?.sales ?? []).slice(0, topN),
    [currentQuery.data, topN]
  )
  const compareSalesMap = useMemo(() => {
    const m = new Map<string, SalesPromotionRow>()
    if (compareQuery.data?.sales) {
      for (const r of compareQuery.data.sales) {
        m.set(r.username, r)
      }
    }
    return m
  }, [compareQuery.data])

  return (
    <div className='grid grid-cols-1 gap-3 lg:grid-cols-2'>
      {/* 渠道推广 */}
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>
            {t('Channel Promotion')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Channel Name')}</TableHead>
                <TableHead className='text-center'>
                  {t('Invited Users Count')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Total Consumed ($)')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {channels.map((row) => {
                const prev = compareChannelMap.get(row.channel)
                const invitedDelta = prev
                  ? deltaPct(row.invited_count, prev.invited_count)
                  : null
                const consumedDelta = prev
                  ? deltaPct(row.total_consumed, prev.total_consumed)
                  : null
                return (
                  <TableRow key={row.channel}>
                    <TableCell className='font-medium'>{row.channel}</TableCell>
                    <TableCell className='text-center'>
                      <span className='tabular-nums'>
                        {row.invited_count.toLocaleString()}
                      </span>
                      {compareEnabled && <DeltaPctText value={invitedDelta} />}
                    </TableCell>
                    <TableCell className='text-center'>
                      <span className='tabular-nums'>
                        {formatCurrencyFromUSD(quotaToUsd(row.total_consumed))}
                      </span>
                      {compareEnabled && <DeltaPctText value={consumedDelta} />}
                    </TableCell>
                  </TableRow>
                )
              })}
              {!isLoading && channels.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground text-center'
                  >
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
              {isLoading && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground text-center'
                  >
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* 销售推广 */}
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>{t('Sales Promotion')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Sales')}</TableHead>
                <TableHead className='text-center'>
                  {t('Owning Channel')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Invited Users Count')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Total Consumed ($)')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sales.map((row) => {
                const prev = compareSalesMap.get(row.username)
                const invitedDelta = prev
                  ? deltaPct(row.invited_count, prev.invited_count)
                  : null
                const consumedDelta = prev
                  ? deltaPct(row.total_consumed, prev.total_consumed)
                  : null
                return (
                  <TableRow key={row.username}>
                    <TableCell className='font-medium'>{row.username}</TableCell>
                    <TableCell className='text-center'>{row.channel}</TableCell>
                    <TableCell className='text-center'>
                      <span className='tabular-nums'>
                        {row.invited_count.toLocaleString()}
                      </span>
                      {compareEnabled && <DeltaPctText value={invitedDelta} />}
                    </TableCell>
                    <TableCell className='text-center'>
                      <span className='tabular-nums'>
                        {formatCurrencyFromUSD(quotaToUsd(row.total_consumed))}
                      </span>
                      {compareEnabled && <DeltaPctText value={consumedDelta} />}
                    </TableCell>
                  </TableRow>
                )
              })}
              {!isLoading && sales.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground text-center'
                  >
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
              {isLoading && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground text-center'
                  >
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
