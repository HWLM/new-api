/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { cn } from '@/lib/utils'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fetchConsumptionTrend } from './api'
import type {
  CompareWindow,
  StatsFilter,
  TrendGranularity,
} from './types'

// day "2026-06-12" → "6-12"；hour "2026-06-12#14" → "6-12 14:00"
function toShortLabel(bucket: string): string {
  if (bucket.includes('#')) {
    const [date, hour] = bucket.split('#')
    const parts = date.split('-')
    if (parts.length !== 3) return bucket
    return `${parseInt(parts[1], 10)}-${parts[2]} ${hour}:00`
  }
  const parts = bucket.split('-')
  if (parts.length !== 3) return bucket
  return `${parseInt(parts[1], 10)}-${parts[2]}`
}

type ChartConsumptionTrendProps = {
  filter: StatsFilter
  /** 开启对比时传入 —— 不传 = 单线模式 */
  compareWindow?: CompareWindow | null
  /** 仅 hour granularity 生效（今天模式下用户填的小时段） */
  startHour?: number
  endHour?: number
}

export function ChartConsumptionTrend({
  filter,
  compareWindow,
  startHour,
  endHour,
}: ChartConsumptionTrendProps) {
  const { t } = useTranslation()
  const [granularity, setGranularity] = useState<TrendGranularity>('day')

  const compareEnabled = !!compareWindow
  const filterKey = useMemo(
    () =>
      JSON.stringify({
        filter,
        granularity,
        compareWindow,
        startHour,
        endHour,
      }),
    [filter, granularity, compareWindow, startHour, endHour]
  )
  const { data, isLoading } = useQuery({
    queryKey: ['user-stats', 'consumption-trend', filterKey],
    queryFn: () =>
      fetchConsumptionTrend(filter, granularity, {
        compare: compareWindow
          ? {
              start_date: compareWindow.start_date,
              end_date: compareWindow.end_date,
            }
          : undefined,
        start_hour: startHour,
        end_hour: endHour,
      }),
  })

  // 合并 current + compare 成同一张表，X 轴用 current 的 buckets。
  // compare 长度若不一致按短截断，多的 compare 数据忽略（不画）。
  type Point = { label: string; bucket: string; current: number; compare?: number }
  const series: Point[] = useMemo(() => {
    if (!data) return []
    const compareValues = data.compare_values ?? []
    return data.buckets.map((b, i) => ({
      label: toShortLabel(b),
      bucket: b,
      current: data.values[i] ?? 0,
      compare: compareEnabled ? compareValues[i] : undefined,
    }))
  }, [data, compareEnabled])

  const isEmpty =
    series.length === 0 ||
    (series.every((p) => p.current === 0) &&
      (!compareEnabled || series.every((p) => (p.compare ?? 0) === 0)))

  // KPI 卡片：仅对比开启时显示
  const showKpi = compareEnabled && data && data.has_compare
  const currentTotal = data?.current_total ?? 0
  const compareTotal = data?.compare_total ?? 0
  const diff = data?.diff ?? 0
  const changeRate = data?.change_rate ?? 0
  const diffSign = diff > 0 ? '+' : ''
  const rateSign = changeRate > 0 ? '+' : ''
  const rateColor =
    changeRate > 0
      ? 'text-emerald-600 dark:text-emerald-400'
      : changeRate < 0
        ? 'text-rose-600 dark:text-rose-400'
        : 'text-muted-foreground'

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-2'>
        <div>
          <CardTitle className='text-base'>
            {t('User Consumption Trend')}
          </CardTitle>
          <div className='text-muted-foreground text-xs'>{t('(Amount $)')}</div>
        </div>
        <Tabs
          value={granularity}
          onValueChange={(v) => setGranularity(v as TrendGranularity)}
        >
          <TabsList>
            <TabsTrigger value='day' className='px-3 text-xs'>
              {t('By Day')}
            </TabsTrigger>
            <TabsTrigger value='hour' className='px-3 text-xs'>
              {t('By Hour')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent className='space-y-3'>
        <div className='h-72'>
          {isLoading ? (
            <Skeleton className='h-full w-full' />
          ) : isEmpty ? (
            <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
              {t('No data')}
            </div>
          ) : (
            <ResponsiveContainer width='100%' height='100%'>
              <LineChart
                data={series}
                margin={{ top: 8, right: 16, left: 0, bottom: 8 }}
              >
                <CartesianGrid strokeDasharray='3 3' />
                <XAxis dataKey='label' tick={{ fontSize: 11 }} />
                <YAxis
                  tick={{ fontSize: 11 }}
                  tickFormatter={(v: number) => `$${v.toFixed(0)}`}
                />
                <Tooltip
                  labelFormatter={(_, entries) =>
                    entries?.[0]?.payload?.bucket ?? ''
                  }
                  formatter={(value, name) => [
                    `$${Number(value ?? 0).toFixed(2)}`,
                    name,
                  ]}
                />
                {compareEnabled && <Legend />}
                <Line
                  type='monotone'
                  dataKey='current'
                  name={t('Current')}
                  stroke='#3b82f6'
                  strokeWidth={2}
                  dot={{ r: 2 }}
                  activeDot={{ r: 4 }}
                />
                {compareEnabled && (
                  <Line
                    type='monotone'
                    dataKey='compare'
                    name={t('Compare')}
                    stroke='#ef4444'
                    strokeWidth={2}
                    dot={{ r: 2 }}
                    activeDot={{ r: 4 }}
                  />
                )}
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* 对比模式下的 4 张 KPI 卡片 */}
        {showKpi && (
          <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            <div className='rounded-lg border px-3 py-2'>
              <div className='text-muted-foreground text-xs'>
                {t('Current Period')}
              </div>
              <div className='text-lg font-semibold tabular-nums'>
                ${currentTotal.toFixed(2)}
              </div>
            </div>
            <div className='rounded-lg border px-3 py-2'>
              <div className='text-muted-foreground text-xs'>
                {t('Compare Period')}
              </div>
              <div className='text-lg font-semibold tabular-nums'>
                ${compareTotal.toFixed(2)}
              </div>
            </div>
            <div className='rounded-lg border px-3 py-2'>
              <div className='text-muted-foreground text-xs'>{t('Diff')}</div>
              <div
                className={cn(
                  'text-lg font-semibold tabular-nums',
                  diff > 0
                    ? 'text-emerald-600 dark:text-emerald-400'
                    : diff < 0
                      ? 'text-rose-600 dark:text-rose-400'
                      : ''
                )}
              >
                {diffSign}
                {diff.toFixed(2)}
              </div>
            </div>
            <div className='rounded-lg border px-3 py-2'>
              <div className='text-muted-foreground text-xs'>
                {t('Change Rate')}
              </div>
              <div
                className={cn('text-lg font-semibold tabular-nums', rateColor)}
              >
                {rateSign}
                {changeRate.toFixed(1)}%
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
