/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import type {
  CreditTrendDimension,
  CreditTrendGranularity,
  CreditTrendResp,
} from './types'

// 按天 "2026-06-12" → "6-12"；按月 "2026-06" 原样展示
function toShortLabel(bucket: string): string {
  const parts = bucket.split('-')
  if (parts.length !== 3) return bucket
  return `${Number.parseInt(parts[1], 10)}-${parts[2]}`
}

export function TrendChart({
  data,
  isLoading,
  dimension,
  granularity,
  onDimensionChange,
  onGranularityChange,
}: {
  data: CreditTrendResp | undefined
  isLoading: boolean
  dimension: CreditTrendDimension
  granularity: CreditTrendGranularity
  onDimensionChange: (next: CreditTrendDimension) => void
  onGranularityChange: (next: CreditTrendGranularity) => void
}) {
  const { t } = useTranslation()

  const dimensionLabels: Record<CreditTrendDimension, string> = {
    recharge: t('Recharge'),
    gift: t('Gift'),
    credit: t('Credit'),
    repayment: t('Repayment'),
  }

  // items prop 让 Base UI Select 的 trigger 显示 label 而不是 raw value
  // （没有它时触发器会直接渲染 "recharge" / "day"）。
  const dimensionItems = [
    { value: 'recharge', label: dimensionLabels.recharge },
    { value: 'gift', label: dimensionLabels.gift },
    { value: 'credit', label: dimensionLabels.credit },
    { value: 'repayment', label: dimensionLabels.repayment },
  ]
  const granularityItems = [
    { value: 'day', label: t('By Day') },
    { value: 'month', label: t('By Month') },
  ]

  const chartData = (data?.buckets ?? []).map((bucket, i) => ({
    bucket,
    label: toShortLabel(bucket),
    value: data?.values?.[i] ?? 0,
  }))

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-end gap-2 space-y-0'>
        <Select
          items={dimensionItems}
          value={dimension}
          onValueChange={(v) => onDimensionChange(v as CreditTrendDimension)}
        >
          <SelectTrigger className='w-28'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {dimensionItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          items={granularityItems}
          value={granularity}
          onValueChange={(v) => onGranularityChange(v as CreditTrendGranularity)}
        >
          <SelectTrigger className='w-20'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {granularityItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent className='h-80'>
        {isLoading && <Skeleton className='h-full w-full' />}
        {!isLoading && chartData.length === 0 && (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        )}
        {!isLoading && chartData.length > 0 && (
          <ResponsiveContainer width='100%' height='100%'>
            <BarChart
              data={chartData}
              margin={{ top: 8, right: 16, left: 0, bottom: 8 }}
            >
              <CartesianGrid strokeDasharray='3 3' />
              <XAxis dataKey='label' tick={{ fontSize: 12 }} />
              <YAxis tick={{ fontSize: 12 }} />
              <Tooltip
                labelFormatter={(_, entries) =>
                  entries?.[0]?.payload?.bucket ?? ''
                }
                formatter={(value) => [
                  `¥${Number(value ?? 0).toFixed(2)}`,
                  dimensionLabels[dimension],
                ]}
              />
              <Bar dataKey='value' fill='#3b82f6' radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
